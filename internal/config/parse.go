package config

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ParseError reports a malformed config line with enough context to fix it
// without guessing; the parser must never fail silently.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// LoadFile reads the config file at path exactly once and parses that
// content, returning the parsed config alongside the bytes it was parsed
// from.
//
// Handing the content back is why this exists next to ParseFile. A caller
// that also makes a trust decision must hash *these* bytes: two reads open a
// window in which the file can change, so the content executed is not the
// content validated.
func LoadFile(path string) (*Config, []byte, error) {
	return load(path, path)
}

// loadFragment is LoadFile for a file in the envokerc.d directory, resolving
// symlinks before deciding what "./" means.
//
// A fragment is often a symlink to a config committed inside a project. A
// relative pattern in that file describes the project, not the config
// directory the link sits in, so the base has to be the *target's* directory.
// The resolution also decides whether the config gets confined (Config.Local).
//
// Unexported so no caller outside this package resolves the path a second
// time per fragment per directory change, or derives a base from a different
// resolution than the identity it deduplicated on.
func loadFragment(path string) (*Config, []byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = ""
	}
	return LoadFragmentResolved(path, resolved)
}

// LoadFragmentResolved is loadFragment for a caller that has already followed
// path's symlinks and needs that resolution for something else as well.
//
// resolved must be filepath.EvalSymlinks' answer for path, or "" when it
// refused to follow the link. "" is not the same as "path is not a link": the
// base then has to be the link's own directory, and Config.DirUnresolved says
// so, which is what lets the confinement decision fail closed.
func LoadFragmentResolved(path, resolved string) (*Config, []byte, error) {
	if resolved == "" {
		// Not fatal: a broken link fails on the read below with a message
		// naming the link, which is more useful than one naming the target.
		// The kernel and EvalSymlinks do not fail on identical conditions,
		// least of all on Windows reparse points, so a link that reads fine
		// anyway leaves Dir describing the link's own directory.
		cfg, content, loadErr := load(path, path)
		if cfg != nil {
			cfg.DirUnresolved = true
		}
		return cfg, content, loadErr
	}
	return load(path, resolved)
}

// load reads path and parses it with base taken from basedOn — the same file
// except for a symlinked fragment, where it is the link's target.
func load(path, basedOn string) (*Config, []byte, error) {
	content, err := readSource(path)
	if err != nil {
		return nil, nil, err
	}

	// Absolute, because patterns are matched against absolute directories:
	// resolving "./src" against a relative "." would compile a pattern that
	// can never match.
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	baseAbs, err := filepath.Abs(basedOn)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}

	cfg, err := Parse(bytes.NewReader(content), filepath.Dir(baseAbs))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = abs
	return cfg, content, nil
}

// readSource reads path's contents once and refuses anything past
// maxConfigBytes.
//
// The bound is on the read rather than on a stat's reported size: a character
// device reports zero and would still read until memory ran out, and a
// regular file can grow between being measured and being read. Reading one
// byte past the bound tells "at the bound" from "over it" without a second
// look, so the one read stays the one read.
func readSource(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	if len(content) > maxConfigBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes; a config that size is not one, and every config in the set is read whole on every directory change", path, maxConfigBytes)
	}
	return content, nil
}

// ParseFile reads and parses the config file at path. Use LoadFile instead
// when the same file's content also feeds a trust decision.
func ParseFile(path string) (*Config, error) {
	cfg, _, err := LoadFile(path)
	return cfg, err
}

// Parse reads an envoke config from r, resolving "./"-relative patterns
// against base — the directory the config file lives in. An empty base means
// there is no such directory, and a relative pattern is then a parse error
// rather than a pattern that silently matches nothing.
//
// The format is line-oriented:
//
//	enter <pattern>
//	    <script line>
//	    <script line>
//
//	leave <pattern>
//	    <script line>
//
// A block header ("enter "/"leave " at the start of a line, no leading
// whitespace) is followed by an indented script body; the body ends at the
// next unindented, non-blank line or EOF. Blank lines inside a body don't end
// it. Lines outside any block that are blank or start with "#" are ignored;
// anything else outside a block is a syntax error.
func Parse(r io.Reader, base string) (*Config, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}

	cfg := Config{Dir: base}
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		lineNo := i + 1

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		if isIndented(line) {
			return nil, &ParseError{Line: lineNo, Msg: fmt.Sprintf("unexpected indented line outside of a block: %q", trimmed)}
		}

		blockType, pattern, ok := parseHeader(trimmed)
		if !ok {
			return nil, &ParseError{Line: lineNo, Msg: fmt.Sprintf("expected \"enter <pattern>\" or \"leave <pattern>\", got %q", trimmed)}
		}
		if pattern == "" {
			return nil, &ParseError{Line: lineNo, Msg: fmt.Sprintf("%s block is missing a pattern", blockType)}
		}
		headerLine := lineNo
		i++

		var bodyLines []string
		for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || isIndented(lines[i])) {
			bodyLines = append(bodyLines, lines[i])
			i++
		}
		bodyLines = trimTrailingBlank(bodyLines)
		if len(bodyLines) == 0 {
			return nil, &ParseError{Line: headerLine, Msg: fmt.Sprintf("%s %s has no script body", blockType, pattern)}
		}

		re, err := compilePattern(pattern, os.UserHomeDir, base)
		if err != nil {
			return nil, &ParseError{Line: headerLine, Msg: err.Error()}
		}

		cfg.Blocks = append(cfg.Blocks, Block{
			Type:       blockType,
			Pattern:    re,
			RawPattern: pattern,
			Script:     dedent(bodyLines),
			Line:       headerLine,
		})
	}
	return &cfg, nil
}

func parseHeader(trimmed string) (BlockType, string, bool) {
	switch {
	case trimmed == "enter" || strings.HasPrefix(trimmed, "enter "):
		return Enter, strings.TrimSpace(strings.TrimPrefix(trimmed, "enter")), true
	case trimmed == "leave" || strings.HasPrefix(trimmed, "leave "):
		return Leave, strings.TrimSpace(strings.TrimPrefix(trimmed, "leave")), true
	default:
		return Enter, "", false
	}
}

func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

func readLines(r io.Reader) ([]string, error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	// A block body line longer than bufio's own 64 KiB limit has to parse
	// rather than fail. The starting size is Scanner's to pick -- a fixed
	// floor would be allocated and zeroed for every config in the set.
	scanner.Buffer(nil, 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return lines, nil
}

// dedent strips the common leading whitespace from a block's script lines, so
// the script's own indentation is preserved relative to the block rather than
// to the config file's column 0.
func dedent(lines []string) string {
	minIndent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}

	if minIndent <= 0 {
		return strings.Join(lines, "\n")
	}

	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			out[i] = ""
			continue
		}
		if len(l) >= minIndent {
			out[i] = l[minIndent:]
		} else {
			out[i] = strings.TrimLeft(l, " \t")
		}
	}
	return strings.Join(out, "\n")
}

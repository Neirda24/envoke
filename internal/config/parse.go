package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseError reports a malformed config line with enough context to fix it
// without guessing — CLAUDE.md requires the parser to never fail silently.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// ParseFile reads and parses the config file at path.
func ParseFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()

	cfg, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse reads an envoke config from r.
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
// next unindented, non-blank line or EOF. Blank lines inside a body don't
// end it, so multi-line scripts with blank lines in the middle work as
// expected. Lines outside any block that are blank or start with "#" are
// ignored; anything else outside a block is a syntax error.
func Parse(r io.Reader) (*Config, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}

	var cfg Config
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

		re, err := compilePattern(pattern, os.UserHomeDir)
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
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return lines, nil
}

// dedent strips the common leading whitespace from a block's script lines,
// so the script's own indentation (e.g. a shell for-loop) is preserved
// relative to the block, not to the config file's column 0.
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

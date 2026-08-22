package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// compilePattern turns a raw config pattern into an anchored regex, resolving
// "./"-relative patterns against base — the directory the config file lives
// in.
//
// A leading "./" or "../" resolves against base, a leading "~" expands to the
// home directory, and "$VAR"/"${VAR}" expands to the environment value. Each
// substitution is inserted as a regexp.QuoteMeta'd literal, so a component
// like "john.doe" cannot change the pattern's meaning, and slash-normalized,
// so a Windows home of `C:\Users\you` can match a pattern written with "/".
//
// The base is prepended *after* expandEnv, unlike the tilde: a directory can
// legitimately be named `$HOME`, and QuoteMeta only makes it `\$HOME` — still
// a `$` followed by an identifier that expandEnv would then substitute.
//
// The result is wrapped as "^(?:...)$", which is what makes matching
// segment-based rather than prefix-based: "/home/foo" can no longer match
// "/home/foobar".
func compilePattern(raw string, homeDir func() (string, error), base string) (*regexp.Regexp, error) {
	expanded, prefix, relative, err := splitRelative(raw, base)
	if err != nil {
		return nil, err
	}
	if !relative {
		if expanded, err = expandHome(raw, homeDir); err != nil {
			return nil, err
		}
	}

	expanded, missing := expandEnv(expanded)
	if len(missing) > 0 {
		// Substituting "" instead would turn a typo like $HOEM/Projects into a
		// valid pattern that can simply never match.
		return nil, fmt.Errorf("pattern %q references undefined environment variable(s): %s",
			raw, strings.Join(missing, ", "))
	}
	if relative {
		expanded = regexp.QuoteMeta(filepath.ToSlash(prefix)) + expanded
	}

	// Compiled standalone first, and the result thrown away. The anchoring
	// below only anchors a pattern whose groups balance: `)|(` wraps into
	// `^(?:)|()$`, a top-level alternation that escaped the anchors and
	// matches the empty string at the start of every path. A pattern that
	// compiles standalone cannot close the group early.
	if _, err := regexp.Compile(expanded); err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", raw, err)
	}

	re, err := regexp.Compile("^(?:" + expanded + ")$")
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", raw, err)
	}
	return re, nil
}

// splitRelative resolves a "./"-relative pattern into the directory it
// resolves against and the regex that follows it. A pattern that isn't
// relative is returned unchanged with relative=false.
//
// The remainder keeps its leading "/" ("./src" -> "/src"), so joining is plain
// concatenation and a bare "." resolves to base itself. For that, the
// directory must not end in a separator of its own — a filesystem root does,
// and a long enough "../" chain reaches one.
func splitRelative(raw, base string) (rest, prefix string, relative bool, err error) {
	up, rest, relative := splitDotPrefix(raw)
	if !relative {
		return raw, "", false, nil
	}
	if base == "" {
		// Parse was handed a reader, so there is no directory to resolve
		// against. Compiling as-is would leave a literal "." in the regex.
		return "", "", false, fmt.Errorf("relative pattern %q needs a config file on disk to resolve against", raw)
	}

	prefix = base
	for range up {
		prefix = filepath.Dir(prefix)
	}
	if rest != "" {
		// os.IsPathSeparator rather than a "/\\" cutset: on Unix a directory
		// may legitimately be named with a trailing backslash.
		for len(prefix) > 0 && os.IsPathSeparator(prefix[len(prefix)-1]) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return rest, prefix, true, nil
}

// splitDotPrefix counts a pattern's leading "../" segments and returns what
// follows them, or relative=false if it has no "./"-relative prefix at all.
//
// Only a leading "./" or "../" counts, exactly as only a leading "~" does, so
// `(/opt|/srv)/x` doesn't silently become relative to somebody's config
// directory and "..." stays the three-any-characters regex it reads as.
func splitDotPrefix(raw string) (up int, rest string, relative bool) {
	for strings.HasPrefix(raw, "../") {
		up++
		raw = raw[3:]
	}

	switch {
	case raw == "":
		// A trailing "../", meaning the parent directory itself, or an empty
		// pattern — which Parse rejects before it gets here.
		return up, "", up > 0
	case raw == "..":
		return up + 1, "", true
	case raw == "." || raw == "./":
		return up, "", true
	case strings.HasPrefix(raw, "./"):
		return up, raw[1:], true
	case up > 0:
		return up, "/" + raw, true
	default:
		return 0, "", false
	}
}

// expandHome replaces a leading "~" with the user's home directory. "~" is
// only special at the very start of the pattern; anywhere else it is an
// ordinary character.
func expandHome(pattern string, homeDir func() (string, error)) (string, error) {
	if pattern != "~" && !strings.HasPrefix(pattern, "~/") {
		return pattern, nil
	}
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("expand ~ in pattern %q: %w", pattern, err)
	}
	return regexp.QuoteMeta(filepath.ToSlash(home)) + pattern[1:], nil
}

// expandEnv replaces $VAR / ${VAR} references with their environment value,
// quoted so the value is matched literally, and reports the names of any that
// aren't set (first-appearance order, deduplicated) so the caller can refuse
// the pattern rather than compile one that can't match.
//
// Hand-rolled rather than os.Expand because patterns are regexes and `$` is a
// metacharacter: os.Expand would eat `$?`, `$*`, `$#` and `$0`-`$9` as shell
// special variables. Only a `$` followed by a real identifier counts.
func expandEnv(pattern string) (expanded string, missing []string) {
	var b strings.Builder
	seen := make(map[string]bool)

	for i := 0; i < len(pattern); {
		if pattern[i] != '$' {
			b.WriteByte(pattern[i])
			i++
			continue
		}

		name, width, ok := envRef(pattern[i:])
		if !ok {
			b.WriteByte('$')
			i++
			continue
		}
		i += width

		// LookupEnv, so an explicitly-empty variable stays a legitimate value.
		if value, defined := os.LookupEnv(name); defined {
			b.WriteString(regexp.QuoteMeta(filepath.ToSlash(value)))
			continue
		}
		if !seen[name] {
			seen[name] = true
			missing = append(missing, name)
		}
	}

	return b.String(), missing
}

// envRef parses a $VAR or ${VAR} reference at the start of s, returning the
// variable name and how many bytes it spans. ok is false for an unterminated
// "${", or a "$" followed by anything that isn't an identifier.
func envRef(s string) (name string, width int, ok bool) {
	if len(s) < 2 {
		return "", 0, false
	}

	if s[1] == '{' {
		end := strings.IndexByte(s, '}')
		if end < 0 {
			return "", 0, false
		}
		name = s[2:end]
		if !isEnvName(name) {
			return "", 0, false
		}
		return name, end + 1, true
	}

	end := 1
	for end < len(s) && isNameByte(s[end], end == 1) {
		end++
	}
	if end == 1 {
		return "", 0, false
	}
	return s[1:end], end, true
}

func isEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameByte(s[i], i == 0) {
			return false
		}
	}
	return true
}

// isNameByte reports whether c may appear in an environment variable name.
// first excludes digits, so "$1" in a pattern stays a literal.
func isNameByte(c byte, first bool) bool {
	switch {
	case c == '_':
		return true
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return !first
	default:
		return false
	}
}

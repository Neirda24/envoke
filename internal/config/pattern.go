package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// compilePattern turns a raw config pattern into an anchored regex.
//
// Two expansions happen before compilation, both inserting their result as a
// *literal* (regexp.QuoteMeta'd) string so that path components like
// "john.doe" or "$HOME" containing regex metacharacters don't accidentally
// change the pattern's meaning:
//
//   - A leading "~" (as in "~/Projects/foo" or a bare "~") expands to the
//     current user's home directory.
//   - "$VAR" / "${VAR}" expands to the environment variable's value.
//
// Both substituted values are also slash-normalized (filepath.ToSlash), for
// the same reason matcher.MatchPath normalizes the paths being tested: the
// pattern text around them is written with "/", so a Windows home directory
// of `C:\Users\you` has to become `C:/Users/you` or `~/Projects` could never
// match anything. On Unix this is a no-op.
//
// The result is then wrapped as "^(?:...)$" so matching is always a full
// match against a whole directory path — this is what makes matching
// segment-based rather than prefix-based: pattern "/home/foo" can no longer
// match "/home/foobar", since a partial/prefix match no longer satisfies the
// anchors.
func compilePattern(raw string, homeDir func() (string, error)) (*regexp.Regexp, error) {
	expanded, err := expandHome(raw, homeDir)
	if err != nil {
		return nil, err
	}

	expanded, missing := expandEnv(expanded)
	if len(missing) > 0 {
		// Silently substituting "" here is what this used to do, and it is
		// the worst option available: a typo like $HOEM/Projects quietly
		// compiles to a perfectly valid pattern (^(?:/Projects)$) that can
		// simply never match, so the block never fires and nothing ever says
		// why. Failing with a positioned error matches how the rest of the
		// parser treats a malformed config, and points straight at the
		// variable to fix.
		return nil, fmt.Errorf("pattern %q references undefined environment variable(s): %s",
			raw, strings.Join(missing, ", "))
	}

	re, err := regexp.Compile("^(?:" + expanded + ")$")
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", raw, err)
	}
	return re, nil
}

// expandHome replaces a leading "~" with the user's home directory, mirroring
// shell tilde expansion. "~" is only special at the very start of the
// pattern (either exactly "~" or "~/..."); a "~" anywhere else is left as an
// ordinary (literal, in regex terms) character.
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
// quoted so the value is matched literally regardless of its contents, and
// reports the names of any references that aren't set (in first-appearance
// order, deduplicated) so the caller can refuse the pattern instead of
// silently compiling one that can't match.
//
// This is hand-rolled rather than using os.Expand for one reason: patterns
// are regexes, and `$` is a regex metacharacter. os.Expand treats `$?`,
// `$*`, `$#` and `$0`-`$9` as shell special variables, so it would consume
// them. Only a `$` followed by an actual identifier is treated as a
// reference here; every other `$` is left alone as the regex anchor it
// almost certainly is.
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

		// An explicitly-empty variable is a legitimate value, so this
		// distinguishes "set to empty" from "not set at all".
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
// variable name and how many bytes it spans. ok is false when s doesn't
// start with a well-formed reference — an unterminated "${", or a "$"
// followed by anything that isn't an identifier.
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
// first excludes digits, so "$1" in a pattern stays a literal rather than
// becoming a reference to a variable nobody would name that.
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

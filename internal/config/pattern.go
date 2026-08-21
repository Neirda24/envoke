package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// compilePattern turns a raw config pattern into an anchored regex, resolving
// "./"-relative patterns against base — the directory the config file itself
// lives in.
//
// Three expansions happen before compilation, all inserting their result as a
// *literal* (regexp.QuoteMeta'd) string so that path components like
// "john.doe" or "$HOME" containing regex metacharacters don't accidentally
// change the pattern's meaning:
//
//   - A leading "./" or "../" (or a bare "." / "..") resolves against base.
//   - A leading "~" (as in "~/Projects/foo" or a bare "~") expands to the
//     current user's home directory.
//   - "$VAR" / "${VAR}" expands to the environment variable's value.
//
// Every substituted value is also slash-normalized (filepath.ToSlash), for
// the same reason matcher.MatchPath normalizes the paths being tested: the
// pattern text around them is written with "/", so a Windows home directory
// of `C:\Users\you` has to become `C:/Users/you` or `~/Projects` could never
// match anything. On Unix this is a no-op.
//
// The base is prepended *after* expandEnv rather than before, unlike the
// tilde. A directory can legitimately be named `$HOME`, and QuoteMeta only
// makes it `\$HOME` — still a `$` followed by an identifier, which expandEnv
// would then substitute, silently pointing the pattern at a directory nobody
// named.
//
// The result is then wrapped as "^(?:...)$" so matching is always a full
// match against a whole directory path — this is what makes matching
// segment-based rather than prefix-based: pattern "/home/foo" can no longer
// match "/home/foobar", since a partial/prefix match no longer satisfies the
// anchors.
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
		// Substituting "" instead would turn a typo like $HOEM/Projects into
		// a perfectly valid pattern that can simply never match, so the
		// block never fires and nothing says why.
		return nil, fmt.Errorf("pattern %q references undefined environment variable(s): %s",
			raw, strings.Join(missing, ", "))
	}
	if relative {
		expanded = regexp.QuoteMeta(filepath.ToSlash(prefix)) + expanded
	}

	// Compiled on its own before being wrapped, and the result thrown away.
	// The anchoring below only anchors a pattern whose groups balance: `)|(`
	// wraps into `^(?:)|()$`, which parses as `^(?:)` OR `()$` — a top-level
	// alternation that escaped the anchors and matches the empty string at
	// the start of every path, so the block fires for every directory. A
	// pattern that compiles standalone cannot close the group early, which
	// rules the whole class out rather than the one spelling of it.
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
// The remainder keeps its leading "/" ("./src" -> "/src"), so joining it to
// the directory is plain concatenation and a bare "." resolves to base itself
// with nothing appended.
//
// For that concatenation to hold, the directory must not end in a separator
// of its own when the remainder supplies one, so one is trimmed in that case.
// A filesystem root does end in one: filepath.Dir stops at "/" (at `C:\` for
// a Windows volume), which a long enough "../" chain reaches and which a
// config living in a root has from the start. Two separators make a pattern
// that compiles and then matches nothing.
func splitRelative(raw, base string) (rest, prefix string, relative bool, err error) {
	up, rest, relative := splitDotPrefix(raw)
	if !relative {
		return raw, "", false, nil
	}
	if base == "" {
		// Parse was handed a reader, so there is no directory to resolve
		// against. Compiling the pattern as-is instead would leave a literal
		// "." in it — a regex that quietly matches the wrong thing.
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
// Only a leading "./" or "../" counts, exactly as only a leading "~" does.
// Anything else keeps the meaning it has always had, so `(/opt|/srv)/x` — an
// ordinary alternation that happens not to start with a slash — doesn't
// silently become relative to somebody's config directory, and "..." stays
// the three-any-characters regex it reads as.
func splitDotPrefix(raw string) (up int, rest string, relative bool) {
	for strings.HasPrefix(raw, "../") {
		up++
		raw = raw[3:]
	}

	switch {
	case raw == "":
		// Reachable either from a trailing "../", which means the parent
		// directory itself, or from an empty pattern — which Parse rejects
		// before it gets here, and which is not relative to anything.
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
// Hand-rolled rather than os.Expand because patterns are regexes and `$` is
// a metacharacter: os.Expand would eat `$?`, `$*`, `$#` and `$0`-`$9` as
// shell special variables. Only a `$` followed by a real identifier counts
// as a reference; every other `$` stays the anchor it almost certainly is.
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

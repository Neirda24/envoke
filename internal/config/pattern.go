package config

import (
	"fmt"
	"os"
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
	expanded = expandEnv(expanded)

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
	return regexp.QuoteMeta(home) + pattern[1:], nil
}

// expandEnv replaces $VAR / ${VAR} references with their environment value,
// quoted so the value is matched literally regardless of its contents.
func expandEnv(pattern string) string {
	return os.Expand(pattern, func(name string) string {
		return regexp.QuoteMeta(os.Getenv(name))
	})
}

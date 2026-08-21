//go:build windows

package matcher

import (
	"regexp"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

// TestMatchPath_NormalizesWindowsSeparators covers the reason MatchPath
// exists. Patterns are regexes over paths and are therefore written with
// `/` (in a regex, `\` is the escape character, so nobody writes
// `C:\Users\you`). filepath.Dir hands back backslashes on Windows, so
// without normalization no plausible pattern could ever match and the whole
// matching engine was a silent no-op on the platform — despite Windows
// binaries and a Scoop manifest being published.
//
// This file only builds on Windows, so it does not run in the Linux CI
// containers. The CrossBuild check keeps the platform compiling; this is
// here for anyone actually developing on Windows.
func TestMatchPath_NormalizesWindowsSeparators(t *testing.T) {
	cases := map[string]string{
		`C:\Users\you\Projects`: "C:/Users/you/Projects",
		`C:\`:                   "C:/",
		`\\server\share\dir`:    "//server/share/dir",
	}
	for in, want := range cases {
		if got := MatchPath(in); got != want {
			t.Errorf("MatchPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNewMatch_WindowsPathMatchesSlashPattern is the end-to-end version:
// a slash-written pattern must match a native Windows directory, while
// Dir stays in native form because it is used as a working directory and
// exposed as ENVOKE_DIR.
func TestNewMatch_WindowsPathMatchesSlashPattern(t *testing.T) {
	const dir = `C:\Users\you\Projects\envoke`
	b := config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^C:/Users/you/Projects/([^/]+)$`)}

	m, ok := NewMatch(&config.Config{}, b, dir)
	if !ok {
		t.Fatalf("expected a slash-written pattern to match the native path %q", dir)
	}
	if m.Dir != dir {
		t.Errorf("Dir = %q, want the native form %q", m.Dir, dir)
	}
	if len(m.Groups) != 2 || m.Groups[1] != "envoke" {
		t.Errorf("Groups = %q, want the capture group to be %q", m.Groups, "envoke")
	}
}

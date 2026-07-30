//go:build !windows

package matcher

import (
	"regexp"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

// TestMatchPath_LeavesUnixPathsAlone guards the choice of filepath.ToSlash
// over a blind strings.ReplaceAll. `\` is a legal character in a Unix
// filename, so rewriting it would corrupt real directory names — and the
// corruption would be invisible, showing up only as a rule that mysteriously
// stops firing.
func TestMatchPath_LeavesUnixPathsAlone(t *testing.T) {
	for _, dir := range []string{
		"/home/you/Projects",
		`/home/you/back\slash`,
		`/home/you/\`,
	} {
		if got := MatchPath(dir); got != dir {
			t.Errorf("MatchPath(%q) = %q, want it unchanged on this platform", dir, got)
		}
	}
}

// TestNewMatch_BackslashInDirectoryNameIsMatchable is the same guarantee one
// level up: a Unix directory whose name contains a backslash must still be
// matchable by a pattern that escapes it.
func TestNewMatch_BackslashInDirectoryNameIsMatchable(t *testing.T) {
	const dir = `/home/you/back\slash`
	b := config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/home/you/(back\\slash)$`)}

	m, ok := NewMatch(b, dir)
	if !ok {
		t.Fatalf("expected %q to match", dir)
	}
	if m.Dir != dir {
		t.Errorf("Dir = %q, want %q", m.Dir, dir)
	}
	if len(m.Groups) != 2 || m.Groups[1] != `back\slash` {
		t.Errorf("Groups = %q, want the backslash preserved in the capture", m.Groups)
	}
}

package matcher

import (
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

// tp makes a Unix-style test path absolute on the platform running the
// test: on Windows "/a/b" becomes "C:/a/b", since filepath.IsAbs rejects a
// path with no volume there and Transitions requires absolute paths.
//
// It returns forward slashes on both platforms on purpose. Patterns are
// matched against MatchPath (filepath.ToSlash), so a pattern and a path can
// both go through tp and stay consistent — which is the whole point of
// having one helper rather than two conventions.
func tp(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + p
	}
	return p
}

// np is tp in the platform's *native* form, for comparing against what
// Transitions returns — it filepath.Clean's its output, so Windows gets
// backslashes back.
func np(p string) string {
	return filepath.Clean(tp(p))
}

// nps maps np over a list of expected paths.
func nps(paths ...string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = np(p)
	}
	return out
}

func block(typ config.BlockType, anchoredPattern, script string) config.Block {
	return config.Block{
		Type:       typ,
		Pattern:    regexp.MustCompile(anchoredPattern),
		RawPattern: anchoredPattern,
		Script:     script,
	}
}

// pattern anchors a Unix-style path as a full-match regex, applying the same
// volume prefix tp does so it lines up with the paths under test.
func pattern(p string) string {
	return "^" + regexp.QuoteMeta(tp(p)) + "$"
}

func TestTransitions_Traverse(t *testing.T) {
	left, entered, err := Transitions(tp("/a/b/c/d"), tp("/a/x/y"))
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}

	wantLeft := nps("/a/b/c/d", "/a/b/c", "/a/b")
	if !reflect.DeepEqual(left, wantLeft) {
		t.Errorf("left = %v, want %v", left, wantLeft)
	}

	wantEntered := nps("/a/x", "/a/x/y")
	if !reflect.DeepEqual(entered, wantEntered) {
		t.Errorf("entered = %v, want %v", entered, wantEntered)
	}
}

func TestTransitions_SamePathIsNoOp(t *testing.T) {
	left, entered, err := Transitions(tp("/a/b"), tp("/a/b"))
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if len(left) != 0 || len(entered) != 0 {
		t.Errorf("expected no transitions, got left=%v entered=%v", left, entered)
	}
}

func TestTransitions_DirectSiblingNoCommonSubtree(t *testing.T) {
	left, entered, err := Transitions(tp("/a"), tp("/b"))
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if want := nps("/a"); !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
	if want := nps("/b"); !reflect.DeepEqual(entered, want) {
		t.Errorf("entered = %v, want %v", entered, want)
	}
}

func TestTransitions_RequiresAbsolutePaths(t *testing.T) {
	if _, _, err := Transitions("relative/a", tp("/a")); err == nil {
		t.Errorf("expected error for relative from path")
	}
	if _, _, err := Transitions(tp("/a"), "relative/b"); err == nil {
		t.Errorf("expected error for relative to path")
	}
}

func TestResolve_SegmentMatchingNotPrefix(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Leave, pattern("/home/foo"), "echo leaving foo"),
	}}

	// Regression: ondir's basename-prefix bug. /home/foobar must not trigger
	// a rule written for /home/foo.
	leaves, _, err := Resolve(cfg, tp("/home/foobar"), tp("/tmp"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 0 {
		t.Errorf("expected no leave matches for /home/foobar, got %v", leaves)
	}

	leaves, _, err = Resolve(cfg, tp("/home/foo"), tp("/tmp"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 1 || leaves[0].Dir != np("/home/foo") {
		t.Errorf("expected one leave match on /home/foo, got %v", leaves)
	}
}

func TestResolve_EnterFiresShallowFirstOnTraverse(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a/x"), "echo enter x"),
		block(config.Enter, pattern("/a/x/y"), "echo enter y"),
	}}

	_, enters, err := Resolve(cfg, tp("/a"), tp("/a/x/y"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 2 {
		t.Fatalf("expected 2 enter matches, got %d: %v", len(enters), enters)
	}
	if enters[0].Dir != np("/a/x") || enters[1].Dir != np("/a/x/y") {
		t.Errorf("expected shallow-first order [%s, %s], got [%s, %s]", np("/a/x"), np("/a/x/y"), enters[0].Dir, enters[1].Dir)
	}
}

func TestEnters_MatchesTheWholeAncestorChainShallowFirst(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a/x"), "echo enter x"),
		block(config.Enter, pattern("/a/x/y"), "echo enter y"),
		block(config.Leave, pattern("/a/x"), "echo leave x"),
	}}

	enters, err := Enters(cfg, tp("/a/x/y"))
	if err != nil {
		t.Fatalf("Enters: %v", err)
	}
	if len(enters) != 2 {
		t.Fatalf("expected 2 enter matches, got %d: %v", len(enters), enters)
	}
	if enters[0].Dir != np("/a/x") || enters[1].Dir != np("/a/x/y") {
		t.Errorf("expected shallow-first order [%s, %s], got [%s, %s]", np("/a/x"), np("/a/x/y"), enters[0].Dir, enters[1].Dir)
	}
}

// TestEnters_IncludesTheDirectoryItself is what separates Enters from
// Resolve: Resolve reports what *changed*, so it can never report a block
// for a directory that is on both sides of the transition.
func TestEnters_IncludesTheDirectoryItself(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a"), "echo a"),
	}}

	enters, err := Enters(cfg, tp("/a"))
	if err != nil {
		t.Fatalf("Enters: %v", err)
	}
	if len(enters) != 1 {
		t.Fatalf("expected the directory's own block, got %d matches", len(enters))
	}

	_, resolved, err := Resolve(cfg, tp("/a"), tp("/a"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("Resolve on a no-op transition should report nothing, got %d", len(resolved))
	}
}

func TestEnters_RejectsRelativePath(t *testing.T) {
	if _, err := Enters(&config.Config{}, "a/b"); err == nil {
		t.Error("expected a relative path to be rejected")
	}
}

func TestResolve_LeaveFiresDeepFirstOnTraverse(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Leave, pattern("/a/x"), "echo leave x"),
		block(config.Leave, pattern("/a/x/y"), "echo leave y"),
	}}

	leaves, _, err := Resolve(cfg, tp("/a/x/y"), tp("/a"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leave matches, got %d: %v", len(leaves), leaves)
	}
	if leaves[0].Dir != np("/a/x/y") || leaves[1].Dir != np("/a/x") {
		t.Errorf("expected deep-first order [%s, %s], got [%s, %s]", np("/a/x/y"), np("/a/x"), leaves[0].Dir, leaves[1].Dir)
	}
}

func TestResolve_MultipleBlocksSameDirFireInDeclarationOrder(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a/x"), "echo first"),
		block(config.Enter, pattern("/a/x"), "echo second"),
	}}

	_, enters, err := Resolve(cfg, tp("/a"), tp("/a/x"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 2 || enters[0].Block.Script != "echo first" || enters[1].Block.Script != "echo second" {
		t.Errorf("expected declaration order [first, second], got %v", enters)
	}
}

func TestResolve_NoMatchesReturnsEmptySlices(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/never/matches"), "echo hi"),
	}}

	leaves, enters, err := Resolve(cfg, tp("/a"), tp("/b"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 0 || len(enters) != 0 {
		t.Errorf("expected no matches, got leaves=%v enters=%v", leaves, enters)
	}
}

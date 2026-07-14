package matcher

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

func block(typ config.BlockType, anchoredPattern, script string) config.Block {
	return config.Block{
		Type:       typ,
		Pattern:    regexp.MustCompile(anchoredPattern),
		RawPattern: anchoredPattern,
		Script:     script,
	}
}

func TestTransitions_Traverse(t *testing.T) {
	left, entered, err := Transitions("/a/b/c/d", "/a/x/y")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}

	wantLeft := []string{"/a/b/c/d", "/a/b/c", "/a/b"}
	if !reflect.DeepEqual(left, wantLeft) {
		t.Errorf("left = %v, want %v", left, wantLeft)
	}

	wantEntered := []string{"/a/x", "/a/x/y"}
	if !reflect.DeepEqual(entered, wantEntered) {
		t.Errorf("entered = %v, want %v", entered, wantEntered)
	}
}

func TestTransitions_SamePathIsNoOp(t *testing.T) {
	left, entered, err := Transitions("/a/b", "/a/b")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if len(left) != 0 || len(entered) != 0 {
		t.Errorf("expected no transitions, got left=%v entered=%v", left, entered)
	}
}

func TestTransitions_DirectSiblingNoCommonSubtree(t *testing.T) {
	left, entered, err := Transitions("/a", "/b")
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if !reflect.DeepEqual(left, []string{"/a"}) {
		t.Errorf("left = %v, want [/a]", left)
	}
	if !reflect.DeepEqual(entered, []string{"/b"}) {
		t.Errorf("entered = %v, want [/b]", entered)
	}
}

func TestTransitions_RequiresAbsolutePaths(t *testing.T) {
	if _, _, err := Transitions("relative/a", "/a"); err == nil {
		t.Errorf("expected error for relative from path")
	}
	if _, _, err := Transitions("/a", "relative/b"); err == nil {
		t.Errorf("expected error for relative to path")
	}
}

func TestResolve_SegmentMatchingNotPrefix(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Leave, `^/home/foo$`, "echo leaving foo"),
	}}

	// Regression: ondir's basename-prefix bug. /home/foobar must not trigger
	// a rule written for /home/foo.
	_, _, err := Resolve(cfg, "/home/foobar", "/tmp")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	leaves, _, err := Resolve(cfg, "/home/foobar", "/tmp")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 0 {
		t.Errorf("expected no leave matches for /home/foobar, got %v", leaves)
	}

	leaves, _, err = Resolve(cfg, "/home/foo", "/tmp")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 1 || leaves[0].Dir != "/home/foo" {
		t.Errorf("expected one leave match on /home/foo, got %v", leaves)
	}
}

func TestResolve_EnterFiresShallowFirstOnTraverse(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, `^/a/x$`, "echo enter x"),
		block(config.Enter, `^/a/x/y$`, "echo enter y"),
	}}

	_, enters, err := Resolve(cfg, "/a", "/a/x/y")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 2 {
		t.Fatalf("expected 2 enter matches, got %d: %v", len(enters), enters)
	}
	if enters[0].Dir != "/a/x" || enters[1].Dir != "/a/x/y" {
		t.Errorf("expected shallow-first order [/a/x, /a/x/y], got [%s, %s]", enters[0].Dir, enters[1].Dir)
	}
}

func TestResolve_LeaveFiresDeepFirstOnTraverse(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Leave, `^/a/x$`, "echo leave x"),
		block(config.Leave, `^/a/x/y$`, "echo leave y"),
	}}

	leaves, _, err := Resolve(cfg, "/a/x/y", "/a")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leave matches, got %d: %v", len(leaves), leaves)
	}
	if leaves[0].Dir != "/a/x/y" || leaves[1].Dir != "/a/x" {
		t.Errorf("expected deep-first order [/a/x/y, /a/x], got [%s, %s]", leaves[0].Dir, leaves[1].Dir)
	}
}

func TestResolve_MultipleBlocksSameDirFireInDeclarationOrder(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, `^/a/x$`, "echo first"),
		block(config.Enter, `^/a/x$`, "echo second"),
	}}

	_, enters, err := Resolve(cfg, "/a", "/a/x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 2 || enters[0].Block.Script != "echo first" || enters[1].Block.Script != "echo second" {
		t.Errorf("expected declaration order [first, second], got %v", enters)
	}
}

func TestResolve_NoMatchesReturnsEmptySlices(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, `^/never/matches$`, "echo hi"),
	}}

	leaves, enters, err := Resolve(cfg, "/a", "/b")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 0 || len(enters) != 0 {
		t.Errorf("expected no matches, got leaves=%v enters=%v", leaves, enters)
	}
}

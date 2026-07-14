// Integration tests: real config files on disk, real temp directories, real
// subprocesses — exercising config, matcher and executor together the way a
// shell hook eventually will.
package envoke

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

func TestTransition_VenvActivateDeactivateReadmeExample(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENVOKE_IT_ROOT", root)

	projectDir := filepath.Join(root, "Projects", "foo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := writeConfig(t, root, `
enter $ENVOKE_IT_ROOT/Projects/([^/]+)
    echo "enter:$ENVOKE_MATCH_1" >> "$ENVOKE_IT_ROOT/log.txt"

leave $ENVOKE_IT_ROOT/Projects/([^/]+)
    echo "leave:$ENVOKE_MATCH_1" >> "$ENVOKE_IT_ROOT/log.txt"
`)

	ctx := context.Background()
	if err := Transition(ctx, cfg, root, projectDir); err != nil {
		t.Fatalf("Transition (enter): %v", err)
	}
	if err := Transition(ctx, cfg, projectDir, root); err != nil {
		t.Fatalf("Transition (leave): %v", err)
	}

	assertLog(t, root, "enter:foo\nleave:foo\n")
}

func TestTransition_TraverseFiresIntermediateDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENVOKE_IT_ROOT", root)

	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := writeConfig(t, root, `
enter $ENVOKE_IT_ROOT/a
    echo enter-a >> "$ENVOKE_IT_ROOT/log.txt"

enter $ENVOKE_IT_ROOT/a/b
    echo enter-b >> "$ENVOKE_IT_ROOT/log.txt"

leave $ENVOKE_IT_ROOT/a/b
    echo leave-b >> "$ENVOKE_IT_ROOT/log.txt"

leave $ENVOKE_IT_ROOT/a
    echo leave-a >> "$ENVOKE_IT_ROOT/log.txt"
`)

	ctx := context.Background()
	// Jumping straight from root to a/b (never stopping at a/) must still
	// fire a's rule on the way in — this is ondir's traverse behavior.
	if err := Transition(ctx, cfg, root, deep); err != nil {
		t.Fatalf("Transition (enter): %v", err)
	}
	if err := Transition(ctx, cfg, deep, root); err != nil {
		t.Fatalf("Transition (leave): %v", err)
	}

	assertLog(t, root, "enter-a\nenter-b\nleave-b\nleave-a\n")
}

func TestTransition_SegmentMatchingRejectsPrefixSibling(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENVOKE_IT_ROOT", root)

	foo := filepath.Join(root, "foo")
	foobar := filepath.Join(root, "foobar")
	if err := os.MkdirAll(foo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(foobar, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := writeConfig(t, root, `
enter $ENVOKE_IT_ROOT/foo
    echo entered-foo >> "$ENVOKE_IT_ROOT/log.txt"
`)

	ctx := context.Background()
	// Regression: ondir's basename-prefix bug — entering /root/foobar must
	// not trigger a rule written for /root/foo.
	if err := Transition(ctx, cfg, root, foobar); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	assertLog(t, root, "")

	if err := Transition(ctx, cfg, foobar, foo); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	assertLog(t, root, "entered-foo\n")
}

func TestTransition_StopsAtFirstFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENVOKE_IT_ROOT", root)

	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := writeConfig(t, root, `
leave $ENVOKE_IT_ROOT/child
    exit 1

enter $ENVOKE_IT_ROOT
    echo should-not-run >> "$ENVOKE_IT_ROOT/log.txt"
`)

	err := Transition(context.Background(), cfg, child, root)
	if err == nil {
		t.Fatalf("expected error from failing leave block")
	}
	assertLog(t, root, "")
}

func writeConfig(t *testing.T, root, body string) *config.Config {
	t.Helper()
	path := filepath.Join(root, "envokerc")
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(body, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cfg, err := config.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return cfg
}

func assertLog(t *testing.T, root, want string) {
	t.Helper()
	path := filepath.Join(root, "log.txt")
	got := ""
	if b, err := os.ReadFile(path); err == nil {
		got = string(b)
	} else if !os.IsNotExist(err) {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
}

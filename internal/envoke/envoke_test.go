// Integration tests: real config files on disk, real temp directories, real
// subprocesses — exercising config, matcher and executor together the way a
// shell hook eventually will.
package envoke

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/configset"
	"github.com/Neirda24/envoke/internal/trust"
)

// set loads path as the whole config set, the way cmd/envoke does before
// calling Transition. Reloaded at every call site rather than shared, so a
// test that edits its config between two transitions gets the new content.
func set(t *testing.T, path string) []configset.Entry {
	t.Helper()
	return configset.Load(path, "")
}

// requirePOSIXShell skips when there is no `sh` on PATH -- Transition runs
// every block through executor.Run, which uses `sh -c`. See that function's
// counterpart in internal/executor for why translating to cmd.exe isn't an
// option.
func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX sh on PATH; `envoke exec` requires one")
	}
}

func TestTransition_VenvActivateDeactivateReadmeExample(t *testing.T) {
	requirePOSIXShell(t)
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
	if err := Transition(ctx, set(t, cfg), root, projectDir); err != nil {
		t.Fatalf("Transition (enter): %v", err)
	}
	if err := Transition(ctx, set(t, cfg), projectDir, root); err != nil {
		t.Fatalf("Transition (leave): %v", err)
	}

	assertLog(t, root, "enter:foo\nleave:foo\n")
}

func TestTransition_TraverseFiresIntermediateDirectories(t *testing.T) {
	requirePOSIXShell(t)
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
	if err := Transition(ctx, set(t, cfg), root, deep); err != nil {
		t.Fatalf("Transition (enter): %v", err)
	}
	if err := Transition(ctx, set(t, cfg), deep, root); err != nil {
		t.Fatalf("Transition (leave): %v", err)
	}

	assertLog(t, root, "enter-a\nenter-b\nleave-b\nleave-a\n")
}

func TestTransition_SegmentMatchingRejectsPrefixSibling(t *testing.T) {
	requirePOSIXShell(t)
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
	if err := Transition(ctx, set(t, cfg), root, foobar); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	assertLog(t, root, "")

	if err := Transition(ctx, set(t, cfg), foobar, foo); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	assertLog(t, root, "entered-foo\n")
}

func TestTransition_StopsAtFirstFailure(t *testing.T) {
	requirePOSIXShell(t)
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

	err := Transition(context.Background(), set(t, cfg), child, root)
	if err == nil {
		t.Fatalf("expected error from failing leave block")
	}
	assertLog(t, root, "")
}

// TestTransition_UntrustedConfigRunsNothing is the guard on this package's
// reason for taking a path instead of a parsed config. Transition is the
// only code path in envoke that spawns a shell from config, so the trust
// check lives inside it where a caller cannot forget it — an earlier
// version accepted a *config.Config and did no trust check at all, which
// made it a violation of the trust-before-execution principle waiting for
// its first caller.
func TestTransition_UntrustedConfigRunsNothing(t *testing.T) {
	requirePOSIXShell(t)
	root := t.TempDir()
	t.Setenv("ENVOKE_IT_ROOT", root)

	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := writeConfigUntrusted(t, root, `
enter $ENVOKE_IT_ROOT/child
    echo should-not-run >> "$ENVOKE_IT_ROOT/log.txt"
`)

	err := Transition(context.Background(), set(t, cfg), root, child)
	if !errors.Is(err, ErrUntrusted) {
		t.Fatalf("Transition on an unapproved config = %v, want ErrUntrusted", err)
	}
	assertLog(t, root, "")
}

// TestTransition_EditingAfterApprovalRunsNothing checks the trust gate keeps
// tracking content, not just "was ever approved".
func TestTransition_EditingAfterApprovalRunsNothing(t *testing.T) {
	requirePOSIXShell(t)
	root := t.TempDir()
	t.Setenv("ENVOKE_IT_ROOT", root)

	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := writeConfig(t, root, `
enter $ENVOKE_IT_ROOT/child
    echo approved >> "$ENVOKE_IT_ROOT/log.txt"
`)
	if err := os.WriteFile(cfg, []byte("enter $ENVOKE_IT_ROOT/child\n    echo smuggled >> \"$ENVOKE_IT_ROOT/log.txt\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := Transition(context.Background(), set(t, cfg), root, child)
	if !errors.Is(err, ErrUntrusted) {
		t.Fatalf("Transition on an edited config = %v, want ErrUntrusted", err)
	}
	assertLog(t, root, "")
}

// writeConfig writes a config under root and approves it, returning its
// path. Transition takes a path rather than a parsed config precisely so it
// can enforce trust itself, so every test that expects blocks to run has to
// go through a real approval — the same thing a user does with
// `envoke allow`.
func writeConfig(t *testing.T, root, body string) string {
	t.Helper()
	path := writeConfigUntrusted(t, root, body)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := trust.Allow(path, content); err != nil {
		t.Fatalf("trust.Allow: %v", err)
	}
	return path
}

// writeConfigUntrusted writes a config without approving it, and points the
// trust store at a temp directory so a real ~/.local/share is never touched.
func writeConfigUntrusted(t *testing.T, root, body string) string {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	path := filepath.Join(root, "envokerc")
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(body, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
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

package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/matcher"
)

// requirePOSIXShell skips when there is no `sh` on PATH. Run executes block
// scripts through `sh -c` -- config scripts are POSIX shell, so translating
// them to cmd.exe is not a thing envoke could meaningfully do. On Windows
// that means `envoke exec` needs Git Bash, WSL or MSYS2, which is
// documented rather than worked around (see docs/getting-started.md), and
// these tests follow the same rule.
func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX sh on PATH; `envoke exec` requires one")
	}
}

func TestRun_ScriptRunsWithMatchedDirAsCwd(t *testing.T) {
	requirePOSIXShell(t)
	if runtime.GOOS == "windows" {
		// The `sh` that satisfies requirePOSIXShell on Windows is Git
		// Bash/MSYS, whose `pwd` reports an MSYS path (/d/a/...) rather than
		// the Windows one (D:\a\...). Comparing the two would be testing
		// MSYS's path translation, not envoke's choice of working directory.
		t.Skip("MSYS `pwd` reports a translated path; this assertion is meaningless there")
	}
	dir := t.TempDir()
	m := mustMatch(t, dir, config.Block{
		Type:    config.Enter,
		Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(filepath.ToSlash(dir)) + `$`),
		Script:  `pwd > out.txt`,
	})

	if err := Run(context.Background(), m); err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	got := readFile(t, filepath.Join(dir, "out.txt"))
	if strings.TrimSpace(got) != wantDir {
		t.Errorf("script ran in cwd %q, want %q", strings.TrimSpace(got), wantDir)
	}
}

func TestRun_CaptureGroupsExposedAsEnvVars(t *testing.T) {
	requirePOSIXShell(t)
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	m := mustMatch(t, projectDir, config.Block{
		Type:    config.Enter,
		Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(filepath.ToSlash(dir)) + `/([^/]+)$`),
		Script:  `echo "$ENVOKE_MATCH,$ENVOKE_MATCH_1,$ENVOKE_TYPE" > out.txt`,
	})

	if err := Run(context.Background(), m); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := strings.TrimSpace(readFile(t, filepath.Join(projectDir, "out.txt")))
	// ENVOKE_MATCH is capture group 0, taken against the slash-normalized
	// path (see matcher.MatchPath), unlike ENVOKE_DIR which stays native.
	// The two differ on Windows and are identical everywhere else.
	want := filepath.ToSlash(projectDir) + ",myproject,enter"
	if got != want {
		t.Errorf("env vars = %q, want %q", got, want)
	}
}

func TestRun_LeaveBlockSetsEnvokeType(t *testing.T) {
	requirePOSIXShell(t)
	dir := t.TempDir()
	m := mustMatch(t, dir, config.Block{
		Type:    config.Leave,
		Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(filepath.ToSlash(dir)) + `$`),
		Script:  `echo "$ENVOKE_TYPE" > out.txt`,
	})

	if err := Run(context.Background(), m); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := strings.TrimSpace(readFile(t, filepath.Join(dir, "out.txt")))
	if got != "leave" {
		t.Errorf("ENVOKE_TYPE = %q, want %q", got, "leave")
	}
}

func TestRun_NonZeroExitReturnsError(t *testing.T) {
	requirePOSIXShell(t)
	dir := t.TempDir()
	m := mustMatch(t, dir, config.Block{
		Type:       config.Enter,
		Pattern:    regexp.MustCompile(`^` + regexp.QuoteMeta(filepath.ToSlash(dir)) + `$`),
		RawPattern: dir,
		Script:     `exit 3`,
		Line:       7,
	})

	err := Run(context.Background(), m)
	if err == nil {
		t.Fatalf("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "7") {
		t.Errorf("error %q should mention the matched dir and header line", err.Error())
	}
}

func TestRun_CancelledContextReturnsError(t *testing.T) {
	requirePOSIXShell(t)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := mustMatch(t, dir, config.Block{
		Type:    config.Enter,
		Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(filepath.ToSlash(dir)) + `$`),
		Script:  `echo hi`,
	})

	if err := Run(ctx, m); err == nil {
		t.Errorf("expected error for cancelled context")
	}
}

// mustMatch builds a Match the same way Resolve does — through
// matcher.NewMatch, so the capture groups a script sees come from the one
// place that runs the pattern. A hand-built Match{} literal would silently
// carry no groups and quietly stop testing ENVOKE_MATCH at all.
func mustMatch(t *testing.T, dir string, b config.Block) matcher.Match {
	t.Helper()
	m, ok := matcher.NewMatch(b, dir)
	if !ok {
		t.Fatalf("block pattern %v does not match %s", b.Pattern, dir)
	}
	return m
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(b)
}

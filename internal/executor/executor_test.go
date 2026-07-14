package executor

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/matcher"
)

func TestRun_ScriptRunsWithMatchedDirAsCwd(t *testing.T) {
	dir := t.TempDir()
	m := matcher.Match{
		Dir: dir,
		Block: config.Block{
			Type:    config.Enter,
			Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(dir) + `$`),
			Script:  `pwd > out.txt`,
		},
	}

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
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "myproject")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	m := matcher.Match{
		Dir: projectDir,
		Block: config.Block{
			Type:    config.Enter,
			Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(dir) + `/([^/]+)$`),
			Script:  `echo "$ENVOKE_MATCH,$ENVOKE_MATCH_1,$ENVOKE_TYPE" > out.txt`,
		},
	}

	if err := Run(context.Background(), m); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := strings.TrimSpace(readFile(t, filepath.Join(projectDir, "out.txt")))
	want := projectDir + ",myproject,enter"
	if got != want {
		t.Errorf("env vars = %q, want %q", got, want)
	}
}

func TestRun_LeaveBlockSetsEnvokeType(t *testing.T) {
	dir := t.TempDir()
	m := matcher.Match{
		Dir: dir,
		Block: config.Block{
			Type:    config.Leave,
			Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(dir) + `$`),
			Script:  `echo "$ENVOKE_TYPE" > out.txt`,
		},
	}

	if err := Run(context.Background(), m); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := strings.TrimSpace(readFile(t, filepath.Join(dir, "out.txt")))
	if got != "leave" {
		t.Errorf("ENVOKE_TYPE = %q, want %q", got, "leave")
	}
}

func TestRun_NonZeroExitReturnsError(t *testing.T) {
	dir := t.TempDir()
	m := matcher.Match{
		Dir: dir,
		Block: config.Block{
			Type:       config.Enter,
			Pattern:    regexp.MustCompile(`^` + regexp.QuoteMeta(dir) + `$`),
			RawPattern: dir,
			Script:     `exit 3`,
			Line:       7,
		},
	}

	err := Run(context.Background(), m)
	if err == nil {
		t.Fatalf("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "7") {
		t.Errorf("error %q should mention the matched dir and header line", err.Error())
	}
}

func TestRun_CancelledContextReturnsError(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := matcher.Match{
		Dir: dir,
		Block: config.Block{
			Type:    config.Enter,
			Pattern: regexp.MustCompile(`^` + regexp.QuoteMeta(dir) + `$`),
			Script:  `echo hi`,
		},
	}

	if err := Run(ctx, m); err == nil {
		t.Errorf("expected error for cancelled context")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(b)
}

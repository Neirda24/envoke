package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runFor(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = run(args, &out, &errBuf)
	return out.String(), errBuf.String(), code
}

func TestRun_Version(t *testing.T) {
	stdout, _, code := runFor(t, "version")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke") {
		t.Errorf("stdout %q should mention envoke", stdout)
	}
}

func TestRun_NoArgsPrintsUsageAndFails(t *testing.T) {
	_, stderr, code := runFor(t)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Errorf("expected usage on stderr")
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	_, stderr, code := runFor(t, "frobnicate")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "frobnicate") {
		t.Errorf("stderr %q should mention the unknown command", stderr)
	}
}

func TestRun_Help(t *testing.T) {
	stdout, _, code := runFor(t, "help")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "shell-init") {
		t.Errorf("expected usage to mention shell-init, got %q", stdout)
	}
}

func TestRun_ShellInitBash(t *testing.T) {
	stdout, _, code := runFor(t, "shell-init", "bash")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "PROMPT_COMMAND") {
		t.Errorf("expected bash hook in stdout, got %q", stdout)
	}
}

func TestRun_ShellInitUnsupportedShell(t *testing.T) {
	_, stderr, code := runFor(t, "shell-init", "fish")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "fish") {
		t.Errorf("stderr %q should mention the shell", stderr)
	}
}

func TestRun_ShellInitWrongArgCount(t *testing.T) {
	_, _, code := runFor(t, "shell-init")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRun_ShellHookNoConfigIsSilentNoOp(t *testing.T) {
	isolateHome(t)
	stdout, stderr, code := runFor(t, "shell-hook", "/a", "/b")
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("shell-hook with no config: stdout=%q stderr=%q code=%d, want all empty/0", stdout, stderr, code)
	}
}

func TestRun_ShellHookMatchDoesNotExecuteButReportsOnStderr(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo should-not-run > `+filepath.Join(home, "marker")+`
`)

	stdout, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("shell-hook must never write to stdout yet (no trust mechanism), got %q", stdout)
	}
	if !strings.Contains(stderr, "1 block(s) matched") {
		t.Errorf("expected stderr to report the match, got %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, "marker")); !os.IsNotExist(err) {
		t.Errorf("matched block must not have executed, but marker file exists")
	}
}

func TestRun_ShellHookNoMatchIsSilent(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /never/matches
    echo hi
`)

	stdout, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("shell-hook with no match: stdout=%q stderr=%q code=%d, want all empty/0", stdout, stderr, code)
	}
}

func TestRun_ShellHookInvalidConfigReportsError(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "not a valid block\n")

	_, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected parse error on stderr")
	}
}

func TestRun_ShellHookWrongArgCount(t *testing.T) {
	_, _, code := runFor(t, "shell-hook", "/only-one")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func isolateHome(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENVOKERC", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, ".envokerc")
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(body, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

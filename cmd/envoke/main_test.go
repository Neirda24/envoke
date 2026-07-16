package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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
	// Under `go test`, ldflags are never set, so version/commit/date keep
	// their zero-value defaults -- assert on that structure/format, not
	// literal injected values a unit test can't control (see .goreleaser.yaml
	// for where the real values come from at release time).
	for _, want := range []string{"envoke dev", "commit unknown", "built unknown", runtime.Version(), runtime.GOOS, runtime.GOARCH} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q should contain %q", stdout, want)
		}
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

func TestRun_ShellInitFish(t *testing.T) {
	stdout, _, code := runFor(t, "shell-init", "fish")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "--on-variable PWD") {
		t.Errorf("expected fish hook in stdout, got %q", stdout)
	}
}

func TestRun_ShellInitTcsh(t *testing.T) {
	stdout, _, code := runFor(t, "shell-init", "tcsh")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "cwdcmd") {
		t.Errorf("expected tcsh hook in stdout, got %q", stdout)
	}
}

func TestRun_ShellInitPowershell(t *testing.T) {
	stdout, _, code := runFor(t, "shell-init", "powershell")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "function global:prompt") {
		t.Errorf("expected powershell hook in stdout, got %q", stdout)
	}
}

func TestRun_ShellInitUnsupportedShell(t *testing.T) {
	_, stderr, code := runFor(t, "shell-init", "cmd")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "cmd") {
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

func TestRun_ShellHookUntrustedMatchReportsOnStderrOnly(t *testing.T) {
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
		t.Errorf("untrusted config must never write to stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "1 block(s) matched") || !strings.Contains(stderr, "not trusted") || !strings.Contains(stderr, "envoke allow") {
		t.Errorf("expected stderr to report the match and hint at `envoke allow`, got %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, "marker")); !os.IsNotExist(err) {
		t.Errorf("matched block must not have executed, but marker file exists")
	}
}

func TestRun_ShellHookTrustedMatchPrintsRenderedScript(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Errorf("trusted config should not warn on stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "export ENVOKE_DIR=") || !strings.Contains(stdout, "echo hi") {
		t.Errorf("expected rendered eval script on stdout, got %q", stdout)
	}
}

func TestRun_ShellHookEditingConfigAfterAllowRevokesTrust(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow"); code != 0 {
		t.Fatalf("allow failed")
	}

	// Any edit, even just adding a rule, must require re-approval.
	writeConfig(t, home, `
enter /a
    echo hi
enter /b
    echo bye
`)

	stdout, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("edited config must not execute until re-approved, got stdout %q", stdout)
	}
	if !strings.Contains(stderr, "not trusted") {
		t.Errorf("expected stderr to report the config as untrusted again, got %q", stderr)
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

func TestRun_ShellHookShellFlagSelectsExportSyntax(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, _, code := runFor(t, "shell-hook", "--shell", "fish", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "set -gx ENVOKE_DIR") {
		t.Errorf("expected fish export syntax with --shell fish, got %q", stdout)
	}
}

func TestRun_ShellHookNoShellFlagDefaultsToPosix(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, _, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "export ENVOKE_DIR") {
		t.Errorf("expected POSIX export syntax with no --shell flag, got %q", stdout)
	}
}

func TestRun_AllowLocatedConfig(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, stderr, code := runFor(t, "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, filepath.Join(home, ".envokerc")) {
		t.Errorf("expected confirmation to mention the trusted path, got %q", stdout)
	}
}

func TestRun_AllowExplicitPath(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, "custom-config")
	if err := os.WriteFile(path, []byte("enter /a\n    echo hi\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, stderr, code := runFor(t, "allow", path)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
}

func TestRun_AllowNoConfigFound(t *testing.T) {
	isolateHome(t)

	_, stderr, code := runFor(t, "allow")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected an error message on stderr")
	}
}

func TestRun_AllowInvalidConfigIsError(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "not a valid block\n")

	_, stderr, code := runFor(t, "allow")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected a parse error on stderr")
	}
}

func TestRun_AllowWrongArgCount(t *testing.T) {
	_, _, code := runFor(t, "allow", "a", "b")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRun_AllowPrintsBlockScriptBeforeTrusting(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
    echo bye

leave /b
    deactivate
`)

	stdout, _, code := runFor(t, "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	for _, want := range []string{"enter /a", "echo hi", "echo bye", "leave /b", "deactivate"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected stdout to contain %q for review, got %q", want, stdout)
		}
	}

	reviewIdx := strings.Index(stdout, "echo hi")
	trustedIdx := strings.Index(stdout, "envoke: trusted")
	if reviewIdx == -1 || trustedIdx == -1 || reviewIdx > trustedIdx {
		t.Errorf("expected the script body to be shown before the trusted confirmation, got %q", stdout)
	}
}

func TestRun_AllowPrintsLineNumberPerBlock(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, _, code := runFor(t, "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "line 1") {
		t.Errorf("expected stdout to mention the block's line number, got %q", stdout)
	}
}

func TestRun_DebugWrongArgCount(t *testing.T) {
	_, _, code := runFor(t, "debug", "/only-one")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRun_DebugNoConfigFound(t *testing.T) {
	isolateHome(t)

	_, stderr, code := runFor(t, "debug", "/a", "/b")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected an error message on stderr")
	}
}

func TestRun_DebugInvalidConfigReportsError(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "not a valid block\n")

	_, stderr, code := runFor(t, "debug", "/", "/a")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected parse error on stderr")
	}
}

func TestRun_DebugNeverExecutesEvenIfTrusted(t *testing.T) {
	home := isolateHome(t)
	marker := filepath.Join(home, "marker")
	writeConfig(t, home, `
enter /a
    touch `+marker+`
`)
	if _, _, code := runFor(t, "allow"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, stderr, code := runFor(t, "debug", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "enter") || !strings.Contains(stdout, "/a") {
		t.Errorf("expected the matched enter block described in stdout, got %q", stdout)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("debug must never execute a block, but marker file exists")
	}
}

func TestRun_DebugReportsUntrustedConfig(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)

	stdout, _, code := runFor(t, "debug", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "NOT trusted") {
		t.Errorf("expected stdout to flag the config as untrusted, got %q", stdout)
	}
}

func TestRun_DebugReportsTrustedConfig(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, _, code := runFor(t, "debug", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(stdout, "NOT trusted") || !strings.Contains(stdout, "trusted") {
		t.Errorf("expected stdout to report the config as trusted, got %q", stdout)
	}
}

func TestRun_DebugNoMatchReportsNoBlocks(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /never/matches
    echo hi
`)

	stdout, _, code := runFor(t, "debug", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "no blocks would fire") {
		t.Errorf("expected stdout to report no matches, got %q", stdout)
	}
}

func TestRun_DebugPrintsMatchedBlockScript(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
    echo bye
`)

	stdout, _, code := runFor(t, "debug", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "echo hi") || !strings.Contains(stdout, "echo bye") {
		t.Errorf("expected stdout to include the matched block's script body, got %q", stdout)
	}

	summaryIdx := strings.Index(stdout, "enter /a")
	scriptIdx := strings.Index(stdout, "echo hi")
	if summaryIdx == -1 || scriptIdx == -1 || summaryIdx > scriptIdx {
		t.Errorf("expected the script body to appear after the block's summary line, got %q", stdout)
	}
}

func TestRun_DebugReportsLeavesBeforeEnters(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
leave /a
    echo bye

enter /b
    echo hi
`)

	stdout, _, code := runFor(t, "debug", "/a", "/b")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	leaveIdx := strings.Index(stdout, "leave /a")
	enterIdx := strings.Index(stdout, "enter /b")
	if leaveIdx == -1 || enterIdx == -1 || leaveIdx > enterIdx {
		t.Errorf("expected leave block reported before enter block, got %q", stdout)
	}
}

func TestRun_ShellHookWarnsOnUnsafeConfigPermissions(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	path := filepath.Join(home, ".envokerc")
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "writable by group/other") || !strings.Contains(stderr, path) {
		t.Errorf("expected stderr to warn about unsafe permissions, got %q", stderr)
	}
}

func TestRun_ShellHookNoWarningOnSafeConfigPermissions(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /never/matches
    echo hi
`)
	path := filepath.Join(home, ".envokerc")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(stderr, "writable by group/other") {
		t.Errorf("expected no permissions warning for a safe-mode config, got %q", stderr)
	}
}

func TestRun_AllowWarnsOnUnsafeConfigPermissions(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	path := filepath.Join(home, ".envokerc")
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "writable by group/other") || !strings.Contains(stderr, path) {
		t.Errorf("expected stderr to warn about unsafe permissions, got %q", stderr)
	}
}

func TestRun_DebugWarnsOnUnsafeConfigPermissions(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	path := filepath.Join(home, ".envokerc")
	if err := os.Chmod(path, 0o620); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "debug", "/", "/a")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "writable by group/other") || !strings.Contains(stderr, path) {
		t.Errorf("expected stderr to warn about unsafe permissions, got %q", stderr)
	}
}

func isolateHome(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENVOKERC", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, ".envokerc")
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(body, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

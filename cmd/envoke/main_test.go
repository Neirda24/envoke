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
	return runForStdin(t, "", args...)
}

// runForStdin is like runFor but also feeds stdin content to run, for
// exercising cmdAllow's confirmation prompt.
func runForStdin(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = run(args, &out, &errBuf, strings.NewReader(stdin))
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
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
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
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
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

// TestRun_ShellHookReadsDirectoriesFromEnv covers the tcsh hook's calling
// convention: because tcsh's only way to pipe into `source` is through an
// `eval`, and interpolating directory names into a re-parsed string is a
// command-injection hole, the tcsh hook passes them in the environment
// instead of as arguments (see internal/shellinit's tcshHook comment).
func TestRun_ShellHookReadsDirectoriesFromEnv(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	t.Setenv("ENVOKE_FROM", "/")
	t.Setenv("ENVOKE_TO", "/a")

	stdout, stderr, code := runFor(t, "shell-hook", "--shell", "tcsh")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "setenv ENVOKE_DIR") || !strings.Contains(stdout, "echo hi") {
		t.Errorf("expected the rendered tcsh script for the env-supplied transition, got %q", stdout)
	}
}

// TestRun_ShellHookPositionalArgsWinOverEnv keeps the environment fallback
// strictly a fallback: a stale ENVOKE_FROM/ENVOKE_TO left in the environment
// (they are exported, briefly, by the tcsh hook) must never override what a
// caller passed explicitly.
func TestRun_ShellHookPositionalArgsWinOverEnv(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	t.Setenv("ENVOKE_FROM", "/")
	t.Setenv("ENVOKE_TO", "/a")

	stdout, _, code := runFor(t, "shell-hook", "/", "/never/matches")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("positional arguments should have taken precedence over the environment, got %q", stdout)
	}
}

func TestRun_ShellHookNoArgsAndNoEnvIsUsageError(t *testing.T) {
	_, stderr, code := runFor(t, "shell-hook")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "ENVOKE_FROM") {
		t.Errorf("usage should mention the environment fallback, got %q", stderr)
	}
}

func TestRun_ShellHookShellFlagSelectsExportSyntax(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
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
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
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

	stdout, stderr, code := runFor(t, "allow", "--yes")
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

	_, stderr, code := runFor(t, "allow", "--yes", path)
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

func TestRun_AllowYesFlagWrongArgCountStillErrors(t *testing.T) {
	_, _, code := runForStdin(t, "", "allow", "--yes", "a", "b")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRun_AllowDefaultAbortsOnNoConfirmation(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, stderr, code := runForStdin(t, "n\n", "allow")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "aborted") || !strings.Contains(stderr, "not trusted") {
		t.Errorf("expected stderr to report the abort, got %q", stderr)
	}
	if strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("must not report trusted after aborting, got %q", stdout)
	}

	debugOut, _, dcode := runFor(t, "debug", "/", "/a")
	if dcode != 0 {
		t.Fatalf("debug exit code = %d, want 0", dcode)
	}
	if !strings.Contains(debugOut, "NOT trusted") {
		t.Errorf("expected config to remain untrusted after aborting allow, got %q", debugOut)
	}
}

func TestRun_AllowDefaultAbortsOnEmptyStdin(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	_, stderr, code := runForStdin(t, "", "allow")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "aborted") {
		t.Errorf("expected stderr to report the abort, got %q", stderr)
	}

	debugOut, _, dcode := runFor(t, "debug", "/", "/a")
	if dcode != 0 {
		t.Fatalf("debug exit code = %d, want 0", dcode)
	}
	if !strings.Contains(debugOut, "NOT trusted") {
		t.Errorf("expected config to remain untrusted after empty-stdin abort, got %q", debugOut)
	}
}

func TestRun_AllowDefaultProceedsOnYesConfirmation(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, _, code := runForStdin(t, "y\n", "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("expected trusted confirmation, got %q", stdout)
	}

	debugOut, _, dcode := runFor(t, "debug", "/", "/a")
	if dcode != 0 {
		t.Fatalf("debug exit code = %d, want 0", dcode)
	}
	if strings.Contains(debugOut, "NOT trusted") {
		t.Errorf("expected config to be trusted after confirming, got %q", debugOut)
	}
}

func TestRun_AllowDefaultConfirmationIsCaseInsensitive(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, _, code := runForStdin(t, "YES\n", "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("expected trusted confirmation, got %q", stdout)
	}
}

func TestRun_AllowYesFlagSkipsPromptEntirely(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	// Empty stdin would abort under the default prompt; --yes must never
	// read it.
	stdout, _, code := runForStdin(t, "", "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("expected trusted confirmation, got %q", stdout)
	}
}

func TestRun_AllowShortYesFlagSkipsPromptEntirely(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, _, code := runForStdin(t, "", "allow", "-y")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("expected trusted confirmation, got %q", stdout)
	}
}

func TestRun_AllowYesFlagComposesWithPathBeforeOrAfter(t *testing.T) {
	home := isolateHome(t)
	path := filepath.Join(home, "custom-config")
	if err := os.WriteFile(path, []byte("enter /a\n    echo hi\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, _, code := runForStdin(t, "", "allow", "--yes", path); code != 0 {
		t.Errorf("--yes before path: exit code = %d, want 0", code)
	}
	if _, _, code := runForStdin(t, "", "allow", path, "--yes"); code != 0 {
		t.Errorf("--yes after path: exit code = %d, want 0", code)
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

	stdout, _, code := runFor(t, "allow", "--yes")
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

	stdout, _, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "line 1") {
		t.Errorf("expected stdout to mention the block's line number, got %q", stdout)
	}
}

func TestRun_AllowFirstTimeShowsFullDump(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, _, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "review each block below") || !strings.Contains(stdout, "echo hi") {
		t.Errorf("expected full block dump on first-time allow, got %q", stdout)
	}
	if strings.Contains(stdout, "changed since it was last trusted") || strings.Contains(stdout, "unchanged since it was last trusted") {
		t.Errorf("first-time allow must not mention a diff or unchanged state, got %q", stdout)
	}
}

func TestRun_AllowReallowUnchangedSkipsPromptAndReview(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("first allow failed")
	}

	// Empty stdin would abort under the default y/N prompt -- if the
	// unchanged case still prompted, this would fail with code 1.
	stdout, stderr, code := runForStdin(t, "", "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q, want 0", code, stderr)
	}
	if !strings.Contains(stdout, "unchanged since it was last trusted") {
		t.Errorf("expected unchanged-state message, got %q", stdout)
	}
	if strings.Contains(stdout, "review each block below") {
		t.Errorf("unchanged re-allow must not re-dump the full config, got %q", stdout)
	}
	if strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("unchanged re-allow should report the already-trusted state, not re-announce trust, got %q", stdout)
	}

	debugOut, _, dcode := runFor(t, "debug", "/", "/a")
	if dcode != 0 {
		t.Fatalf("debug exit code = %d, want 0", dcode)
	}
	if strings.Contains(debugOut, "NOT trusted") {
		t.Errorf("expected config to remain trusted after an unchanged re-allow, got %q", debugOut)
	}
}

func TestRun_AllowReallowChangedShowsDiffNotFullDump(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("first allow failed")
	}

	writeConfig(t, home, "enter /a\n    echo hi\n    echo new-line\n")

	stdout, _, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "changed since it was last trusted") {
		t.Errorf("expected diff header on a changed re-allow, got %q", stdout)
	}
	if !strings.Contains(stdout, "+ "+"    echo new-line") {
		t.Errorf("expected added line marked with '+ ', got %q", stdout)
	}
	if strings.Contains(stdout, "review each block below") {
		t.Errorf("changed re-allow must show a diff, not the full block dump, got %q", stdout)
	}
	// The unchanged "echo hi" line must not be re-printed as changed.
	if strings.Contains(stdout, "- "+"    echo hi") || strings.Contains(stdout, "+ "+"    echo hi") {
		t.Errorf("unchanged line must not appear in the diff, got %q", stdout)
	}
}

func TestRun_AllowReallowShowsRemovedAndAddedLines(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo old-line\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("first allow failed")
	}

	writeConfig(t, home, "enter /a\n    echo new-line\n")

	stdout, _, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "- "+"    echo old-line") {
		t.Errorf("expected removed line marked with '- ', got %q", stdout)
	}
	if !strings.Contains(stdout, "+ "+"    echo new-line") {
		t.Errorf("expected added line marked with '+ ', got %q", stdout)
	}
}

func TestRun_AllowReallowChangedStillHonorsAbort(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("first allow failed")
	}

	writeConfig(t, home, "enter /a\n    echo bye\n")

	stdout, stderr, code := runForStdin(t, "n\n", "allow")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "aborted") {
		t.Errorf("expected abort on stderr, got %q", stderr)
	}
	if strings.Contains(stdout, "envoke: trusted") {
		t.Errorf("must not report trusted after aborting a changed re-allow, got %q", stdout)
	}

	debugOut, _, dcode := runFor(t, "debug", "/", "/a")
	if dcode != 0 {
		t.Fatalf("debug exit code = %d, want 0", dcode)
	}
	if !strings.Contains(debugOut, "NOT trusted") {
		t.Errorf("expected config to be untrusted after aborting a changed re-allow, got %q", debugOut)
	}
}

func TestDiffLines(t *testing.T) {
	tests := []struct {
		name string
		old  []string
		new  []string
		want []string
	}{
		{
			name: "identical",
			old:  []string{"a", "b", "c"},
			new:  []string{"a", "b", "c"},
			want: nil,
		},
		{
			name: "single line added",
			old:  []string{"a", "b"},
			new:  []string{"a", "b", "c"},
			want: []string{"+ c"},
		},
		{
			name: "single line removed",
			old:  []string{"a", "b", "c"},
			new:  []string{"a", "b"},
			want: []string{"- c"},
		},
		{
			name: "single line changed in the middle",
			old:  []string{"a", "b", "c"},
			new:  []string{"a", "x", "c"},
			want: []string{"- b", "+ x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffLines(tt.old, tt.new)
			if len(got) != len(tt.want) {
				t.Fatalf("diffLines(%v, %v) = %v, want %v", tt.old, tt.new, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("diffLines(%v, %v)[%d] = %q, want %q", tt.old, tt.new, i, got[i], tt.want[i])
				}
			}
		})
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
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
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
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
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

	_, stderr, code := runFor(t, "allow", "--yes")
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

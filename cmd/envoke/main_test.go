package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/shellinit"
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

// tp makes a Unix-style test path absolute on the platform running the test:
// on Windows "/a" becomes "C:/a", since filepath.IsAbs rejects a path with no
// volume there and every subcommand taking a transition demands absolute
// paths. Same helper, same name, as internal/matcher's — one convention, not
// two.
//
// It returns forward slashes on both platforms, and that is what lets one
// helper serve two different jobs. A *pattern* is a regex matched against
// matcher.MatchPath, which is filepath.ToSlash, so a pattern is written with
// `/` on every platform and a Windows volume there is spelled `C:/a`. A path
// *argument* is native — but `C:/a` is already absolute as far as Go is
// concerned, and filepath.Clean turns it into `C:\a` before anything compares
// it. So tp is the right spelling for both, and np below is what the same path
// looks like once envoke prints it back.
func tp(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + p
	}
	return p
}

// np is tp in the platform's native form, for asserting on a path envoke
// printed rather than one handed to it: a matched block's Dir has been through
// filepath.Clean, which gives Windows its backslashes back. A block's
// RawPattern has not — that is still the slash-written pattern, so assertions
// on it use tp.
func np(p string) string {
	return filepath.Clean(tp(p))
}

// configBody prepares a fixture config for writing. It drops the newline a
// raw-string literal starts with, and gives every Unix-style absolute pattern
// in it the volume prefix tp adds, so a single fixture body works on both
// platforms without every test spelling out the concatenation.
//
// Only a pattern beginning with `/` is rewritten. A `./src` resolves against
// the config's own directory and must stay relative, a pattern built from a
// real directory already carries its volume, and a deliberately malformed one
// has to stay malformed.
func configBody(body string) string {
	body = strings.TrimPrefix(body, "\n")
	if runtime.GOOS != "windows" {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		for _, kw := range []string{"enter ", "leave "} {
			if strings.HasPrefix(line, kw) && strings.HasPrefix(line[len(kw):], "/") {
				lines[i] = kw + tp(line[len(kw):])
			}
		}
	}
	return strings.Join(lines, "\n")
}

// requirePOSIXShell skips when there is no `sh` on PATH. Only `envoke exec`
// needs one — it is the single subcommand that runs a block rather than
// printing it — and on Windows that means Git Bash, WSL or MSYS2, which is
// documented rather than worked around. The counterparts in internal/executor
// and internal/envoke say the same thing about the same dependency.
func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX sh on PATH; `envoke exec` requires one")
	}
}

// requirePermissionBits skips a test that asserts one of the "writable by
// group/other" warnings, where there is no such thing to warn about.
// internal/fsperm answers false on Windows on purpose -- Go's os.Stat
// synthesises the permission word there from the read-only attribute alone, so
// testing 0o022 against it would report every config as world-writable -- and
// a test expecting the warning would fail for that reason and no other.
//
// It deliberately does not skip as root: the mode bits are still readable
// there, so the warning still fires, and the CI container runs as root.
func requirePermissionBits(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no permission bits to warn about on Windows -- internal/fsperm reports nothing there")
	}
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

// `help` is a dispatched verb like any other, and the usage text is the only
// list of them a user ever sees -- a verb missing from it is a verb that does
// not exist as far as anyone reading is concerned.
func TestUsage_ListsHelpAsASubcommand(t *testing.T) {
	stdout, _, code := runFor(t, "help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke help ") {
		t.Errorf("expected `help` to be listed as a subcommand, got %q", stdout)
	}
}

// The order the paths are listed in is the order config.Locate consults them,
// so a reader can stop at the first one that applies to them. Listing the
// override second reads as a fallback, which is the opposite of what it is.
func TestUsage_ListsConfigLocationsInPrecedenceOrder(t *testing.T) {
	stdout, _, code := runFor(t, "help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, group := range [][2]string{
		{"$ENVOKERC,", "~/.envokerc or"},
		{"$ENVOKERC_D,", "~/.envokerc.d or"},
	} {
		first, second := strings.Index(stdout, group[0]), strings.Index(stdout, group[1])
		if first < 0 || second < 0 {
			t.Fatalf("expected both %q and %q in the usage text, got %q", group[0], group[1], stdout)
		}
		if first > second {
			t.Errorf("%s takes precedence over %s and must be listed first", group[0], group[1])
		}
	}
}

// cmdList reconciles two things, and a synopsis promising only "every trusted
// config" describes the version that just dumped the store.
func TestUsage_ListLineDescribesTheReconciliation(t *testing.T) {
	stdout, _, code := runFor(t, "help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var line string
	for _, l := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "envoke list ") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no `envoke list` line in the usage text, got %q", stdout)
	}
	if !strings.Contains(line, "would load") || !strings.Contains(line, "left over") {
		t.Errorf("the list line must cover both halves of what it reports, got %q", line)
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

// TestRun_ShellInitWrongArgCount is deliberately about *too many*
// arguments. Zero arguments used to be the error case, and this test still
// asserted that after shell-init learned to guess from $SHELL -- it kept
// passing only because the Linux CI container happens to have no $SHELL set,
// so detection failed and returned 2 for the wrong reason. The macOS runner,
// which does set $SHELL, is what exposed it.
//
// The zero-argument paths are covered explicitly, with $SHELL controlled, by
// TestRun_ShellInitDetectsShellFromEnv and
// TestRun_ShellInitUndetectableShellIsError.
func TestRun_ShellInitWrongArgCount(t *testing.T) {
	// Pinned so the assertion never depends on the ambient environment
	// again, in either direction.
	t.Setenv("SHELL", "/bin/bash")

	_, stderr, code := runFor(t, "shell-init", "bash", "zsh")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("expected usage on stderr, got %q", stderr)
	}
}

func TestRun_ShellHookNoConfigIsSilentNoOp(t *testing.T) {
	isolateHome(t)
	stdout, stderr, code := runFor(t, "shell-hook", tp("/a"), tp("/b"))
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

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
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

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
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

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
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

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("shell-hook with no match: stdout=%q stderr=%q code=%d, want all empty/0", stdout, stderr, code)
	}
}

// TestRun_ShellHookMissingEnvokercIsSilent covers a real papercut:
// $ENVOKERC is honoured verbatim, so pointing it at a file you haven't
// written yet is an ordinary state — but shell-hook reported the resulting
// ENOENT as an error, on every single `cd`, until the file appeared.
func TestRun_ShellHookMissingEnvokercIsSilent(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("ENVOKERC", filepath.Join(home, "not-written-yet"))

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("shell-hook with a missing $ENVOKERC: stdout=%q stderr=%q code=%d, want all empty/0", stdout, stderr, code)
	}
}

// A config that exists but is unreadable is the opposite case: something is
// wrong with a config its owner believes is in effect, so it stays loud.
func TestRun_ShellHookUnreadableConfigStillReportsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Chmod there sets the read-only attribute and nothing else, so a
		// 0o000 config still opens and reads fine. Making a file genuinely
		// unreadable needs a DACL, which is the same advapi32 dependency
		// internal/fsperm declined to take on for a warning.
		t.Skip("a config cannot be made unreadable through os.Chmod on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission bits are not enforced")
	}
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if err := os.Chmod(filepath.Join(home, ".envokerc"), 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected an error for an unreadable config")
	}
}

func TestRun_ShellHookInvalidConfigReportsError(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "not a valid block\n")

	_, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected parse error on stderr")
	}
}

func TestRun_ShellHookWrongArgCount(t *testing.T) {
	_, _, code := runFor(t, "shell-hook", tp("/only-one"))
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
	t.Setenv("ENVOKE_FROM", tp("/"))
	t.Setenv("ENVOKE_TO", tp("/a"))

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
	t.Setenv("ENVOKE_FROM", tp("/"))
	t.Setenv("ENVOKE_TO", tp("/a"))

	stdout, _, code := runFor(t, "shell-hook", tp("/"), tp("/never/matches"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("positional arguments should have taken precedence over the environment, got %q", stdout)
	}
}

func TestRun_ShellHookNoArgsAndNoEnvIsUsageError(t *testing.T) {
	unsetEnv(t, "ENVOKE_FROM")
	unsetEnv(t, "ENVOKE_TO")

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

	stdout, _, code := runFor(t, "shell-hook", "--shell", "fish", tp("/"), tp("/a"))
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

	stdout, _, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "export ENVOKE_DIR") {
		t.Errorf("expected POSIX export syntax with no --shell flag, got %q", stdout)
	}
}

// TestRun_ShellHookUnknownShellIsRejected guards the CLI boundary rather
// than Render's own behavior: Render deliberately falls back to POSIX for an
// unknown dialect, which would mean a typo silently feeds `export` to a fish
// or tcsh session on every directory change.
func TestRun_ShellHookUnknownShellIsRejected(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, stderr, code := runFor(t, "shell-hook", "--shell", "fsh", tp("/"), tp("/a"))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("nothing may reach the shell for eval, got %q", stdout)
	}
	if !strings.Contains(stderr, `unknown shell "fsh"`) {
		t.Errorf("expected the rejected name in the error, got %q", stderr)
	}
}

// allowedConfig writes a one-block config matching /a and trusts it, which
// is the starting point for every switch test: the only thing left that can
// stop a block from running is the switch itself.
func allowedConfig(t *testing.T) {
	t.Helper()
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
}

func TestRun_DisableStopsShellHookSilently(t *testing.T) {
	allowedConfig(t)

	if _, _, code := runFor(t, "disable"); code != 0 {
		t.Fatalf("disable exit code = %d, want 0", code)
	}

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("a disabled envoke must render nothing, got %q", stdout)
	}
	// This runs on every single directory change, so any output at all
	// would be a per-cd nuisance for as long as the switch is off.
	if stderr != "" {
		t.Errorf("a disabled envoke must stay silent, got %q", stderr)
	}
}

func TestRun_EnableRestoresShellHook(t *testing.T) {
	allowedConfig(t)

	if _, _, code := runFor(t, "disable"); code != 0 {
		t.Fatalf("disable failed")
	}
	if _, _, code := runFor(t, "enable"); code != 0 {
		t.Fatalf("enable failed")
	}

	stdout, _, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "echo hi") {
		t.Errorf("expected the block back after enable, got %q", stdout)
	}
}

// TestRun_EnvDisableOverridesTheFlag covers both directions of the
// per-session override, which is the whole point of having two switches.
func TestRun_EnvDisableOverridesTheFlag(t *testing.T) {
	allowedConfig(t)

	t.Setenv("ENVOKE_DISABLE", "1")
	stdout, _, _ := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if stdout != "" {
		t.Errorf("ENVOKE_DISABLE=1 must stop the hook, got %q", stdout)
	}

	if _, _, code := runFor(t, "disable"); code != 0 {
		t.Fatalf("disable failed")
	}
	t.Setenv("ENVOKE_DISABLE", "0")
	stdout, _, _ = runFor(t, "shell-hook", tp("/"), tp("/a"))
	if !strings.Contains(stdout, "echo hi") {
		t.Errorf("ENVOKE_DISABLE=0 must re-enable this shell, got %q", stdout)
	}
}

// TestRun_DisableWarnsWhenTheEnvOverrideWins keeps `envoke disable` from
// looking like it did nothing in a shell that has already overridden it.
func TestRun_DisableWarnsWhenTheEnvOverrideWins(t *testing.T) {
	allowedConfig(t)
	t.Setenv("ENVOKE_DISABLE", "0")

	stdout, stderr, code := runFor(t, "disable")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "disabled for every shell") {
		t.Errorf("expected the switch to be reported as set, got %q", stdout)
	}
	if !strings.Contains(stderr, "ENVOKE_DISABLE") {
		t.Errorf("expected a warning that the override wins here, got %q", stderr)
	}
}

func TestRun_ExecReportsBeingDisabled(t *testing.T) {
	allowedConfig(t)
	if _, _, code := runFor(t, "disable"); code != 0 {
		t.Fatalf("disable failed")
	}

	// Exit 0: the user asked for envoke to be off, which is not a failure.
	// But exec is invoked deliberately, so silence would leave a script
	// mysteriously missing its environment.
	_, stderr, code := runFor(t, "exec", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "disabled") {
		t.Errorf("expected exec to say why it did nothing, got %q", stderr)
	}
}

func TestRun_DebugStillWorksWhenDisabled(t *testing.T) {
	allowedConfig(t)
	if _, _, code := runFor(t, "disable"); code != 0 {
		t.Fatalf("disable failed")
	}

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "echo hi") {
		t.Errorf("debug must keep listing blocks while disabled, got %q", stdout)
	}
	if !strings.Contains(stdout, "disabled") {
		t.Errorf("debug must report the switch, got %q", stdout)
	}
}

func TestRun_SwitchRejectsArguments(t *testing.T) {
	isolateHome(t)
	for _, cmd := range []string{"disable", "enable"} {
		if _, _, code := runFor(t, cmd, "extra"); code != 2 {
			t.Errorf("%s with an argument: exit code = %d, want 2", cmd, code)
		}
	}
}

// TestRun_ReloadAppliesEntersForTheCurrentDirectory covers what reload
// exists for: allow is a child process and cannot export into the shell that
// ran it, so a freshly approved config would otherwise only take effect on
// the next cd.
func TestRun_ReloadAppliesEntersForTheCurrentDirectory(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo outer

enter /a/b
    echo inner

leave /a
    echo bye
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	t.Setenv("PWD", tp("/a/b"))

	stdout, _, code := runFor(t, "reload")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	outer, inner := strings.Index(stdout, "echo outer"), strings.Index(stdout, "echo inner")
	if outer == -1 || inner == -1 || outer > inner {
		t.Errorf("expected both enter blocks, shallowest first, got %q", stdout)
	}
	// Nothing has been left, and envoke does not snapshot state to unwind.
	if strings.Contains(stdout, "echo bye") {
		t.Errorf("reload must not run leave blocks, got %q", stdout)
	}
}

func TestRun_ReloadRefusesUntrustedConfig(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	t.Setenv("PWD", tp("/a"))

	stdout, stderr, code := runFor(t, "reload")
	// Unlike shell-hook, which only notes it: this was typed, so silence
	// would look like it worked.
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("nothing may reach the shell for eval, got %q", stdout)
	}
	if !strings.Contains(stderr, "envoke allow") {
		t.Errorf("expected the allow hint, got %q", stderr)
	}
}

func TestRun_ReloadShellFlagSelectsExportSyntax(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	t.Setenv("PWD", tp("/a"))

	stdout, _, code := runFor(t, "reload", "--shell", "fish")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "set -gx ENVOKE_DIR") {
		t.Errorf("expected fish syntax, got %q", stdout)
	}
}

func TestRun_ReloadRejectsArgumentsAndUnknownShell(t *testing.T) {
	isolateHome(t)
	if _, _, code := runFor(t, "reload", tp("/a")); code != 2 {
		t.Errorf("positional argument: exit code = %d, want 2", code)
	}
	if _, _, code := runFor(t, "reload", "--shell", "fsh"); code != 2 {
		t.Errorf("unknown shell: exit code = %d, want 2", code)
	}
}

func TestRun_AllowPointsAtReload(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)

	stdout, _, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "envoke reload") {
		t.Errorf("expected allow to name the way to apply it now, got %q", stdout)
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
	if err := os.WriteFile(path, []byte(configBody("enter /a\n    echo hi\n")), 0o644); err != nil {
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

	debugOut, _, dcode := runFor(t, "debug", tp("/"), tp("/a"))
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

	debugOut, _, dcode := runFor(t, "debug", tp("/"), tp("/a"))
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

	debugOut, _, dcode := runFor(t, "debug", tp("/"), tp("/a"))
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
	if err := os.WriteFile(path, []byte(configBody("enter /a\n    echo hi\n")), 0o644); err != nil {
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

	for _, want := range []string{"enter " + tp("/a"), "echo hi", "echo bye", "leave " + tp("/b"), "deactivate"} {
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

	debugOut, _, dcode := runFor(t, "debug", tp("/"), tp("/a"))
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

	debugOut, _, dcode := runFor(t, "debug", tp("/"), tp("/a"))
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
	_, _, code := runFor(t, "debug", tp("/a"), tp("/b"), tp("/c"))
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRun_DebugNoConfigFound(t *testing.T) {
	isolateHome(t)

	_, stderr, code := runFor(t, "debug", tp("/a"), tp("/b"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Errorf("expected an error message on stderr")
	}
}

// "You have no envokerc.d" and "you have one and nothing in it counted" are the
// same empty set but opposite next moves, so the message has to tell them
// apart: a user staring at a directory full of files does not need to be told
// it isn't there.
func TestRun_DebugDistinguishesAnEmptyFragmentDirFromNoneAtAll(t *testing.T) {
	t.Run("no directory at all", func(t *testing.T) {
		isolateHome(t)

		_, stderr, code := runFor(t, "debug", tp("/a"), tp("/b"))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "no envokerc.d directory") {
			t.Errorf("expected the message to report a missing directory, got %q", stderr)
		}
	})

	t.Run("a directory whose every file was skipped", func(t *testing.T) {
		isolateHome(t)
		dir := fragmentDir(t)
		writeFragment(t, dir, "10-work~", "enter /work\n    echo hi\n")

		_, stderr, code := runFor(t, "debug", tp("/a"), tp("/b"))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if strings.Contains(stderr, "no envokerc.d directory") {
			t.Errorf("the directory exists -- the message must not deny it, got %q", stderr)
		}
		if !strings.Contains(stderr, dir) || !strings.Contains(stderr, "skipped") {
			t.Errorf("expected the message to name the directory and say why it looks empty, got %q", stderr)
		}
	})

	t.Run("a directory that does not exist", func(t *testing.T) {
		isolateHome(t)
		missing := filepath.Join(t.TempDir(), "envokerc.d")
		t.Setenv("ENVOKERC_D", missing)

		_, stderr, code := runFor(t, "debug", tp("/a"), tp("/b"))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, missing) {
			t.Errorf("$ENVOKERC_D is honoured verbatim, so the message must name it, got %q", stderr)
		}
	})
}

func TestRun_DebugInvalidConfigReportsError(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "not a valid block\n")

	_, stderr, code := runFor(t, "debug", tp("/"), tp("/a"))
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

	stdout, stderr, code := runFor(t, "debug", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "enter") || !strings.Contains(stdout, tp("/a")) {
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

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/a"))
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

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/a"))
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

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/a"))
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

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "echo hi") || !strings.Contains(stdout, "echo bye") {
		t.Errorf("expected stdout to include the matched block's script body, got %q", stdout)
	}

	summaryIdx := strings.Index(stdout, "enter "+np("/a"))
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

	stdout, _, code := runFor(t, "debug", tp("/a"), tp("/b"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	leaveIdx := strings.Index(stdout, "leave "+np("/a"))
	enterIdx := strings.Index(stdout, "enter "+np("/b"))
	if leaveIdx == -1 || enterIdx == -1 || leaveIdx > enterIdx {
		t.Errorf("expected leave block reported before enter block, got %q", stdout)
	}
}

// TestRun_DebugNotesWorkingDirOnlyWhenItDiffers covers the note that makes
// the exec/shell-hook working-directory split visible. Listing the matched
// directory next to a block reads as "relative paths resolve from here",
// which holds for exec and not for the hook.
func TestRun_DebugNotesWorkingDirOnlyWhenItDiffers(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)

	deep, _, code := runFor(t, "debug", tp("/"), tp("/a/b/c"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(deep, "$ENVOKE_DIR") {
		t.Errorf("expected a working-directory note when landing below the match, got %q", deep)
	}

	exact, _, code := runFor(t, "debug", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(exact, "$ENVOKE_DIR") {
		t.Errorf("no note is warranted when the match is the destination, got %q", exact)
	}
}

func TestRun_ShellHookWarnsOnUnsafeConfigPermissions(t *testing.T) {
	requirePermissionBits(t)
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	path := filepath.Join(home, ".envokerc")
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
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

	_, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(stderr, "writable by group/other") {
		t.Errorf("expected no permissions warning for a safe-mode config, got %q", stderr)
	}
}

func TestRun_AllowWarnsOnUnsafeConfigPermissions(t *testing.T) {
	requirePermissionBits(t)
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
	requirePermissionBits(t)
	home := isolateHome(t)
	writeConfig(t, home, `
enter /a
    echo hi
`)
	path := filepath.Join(home, ".envokerc")
	if err := os.Chmod(path, 0o620); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "debug", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "writable by group/other") || !strings.Contains(stderr, path) {
		t.Errorf("expected stderr to warn about unsafe permissions, got %q", stderr)
	}
}

// TestRun_AllowRecordsTheContentItReviewed is the CLI-level half of the
// TOCTOU fix (internal/trust's TestIsTrusted_JudgesGivenContentNotFileOnDisk
// is the other): `envoke allow` used to read the config once to parse it,
// once to display it, and a third time inside trust.Allow to hash it. What
// got recorded as trusted was therefore whatever the file held at that last
// read, not what the user actually reviewed and confirmed.
//
// A single read now feeds all three, so an edit that lands after the review
// simply leaves the config untrusted -- which is what this asserts: approve,
// then modify, and the modified config must not inherit the approval.
// TestDiffLines_LargeConfigFallsBackToFullDump guards the bound on the LCS
// table. The algorithm is O(n*m) in time and memory; "config files are
// small" is true of anything written by hand but nothing enforces it, so
// past diffCap `envoke allow` shows the full block dump instead of
// allocating a table quadratic in the file's size.
func TestDiffLines_LargeConfigFallsBackToFullDump(t *testing.T) {
	small := strings.Repeat("line\n", 10)
	large := strings.Repeat("line\n", diffCap+1)

	if !canDiff(small, small) {
		t.Errorf("a normal-sized config should be diffed")
	}
	if canDiff(small, large) || canDiff(large, small) {
		t.Errorf("a config past diffCap on either side must fall back to the full dump")
	}
}

func TestDiffLines_ReportsOnlyChangedLines(t *testing.T) {
	got := diffLines(
		[]string{"enter /a", "    echo one", "    echo two"},
		[]string{"enter /a", "    echo one", "    echo THREE"},
	)
	want := []string{"-     echo two", "+     echo THREE"}
	if len(got) != len(want) {
		t.Fatalf("diffLines = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diffLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRun_WarnsWhenTrustStoreIsGroupWritable covers the more serious half of
// the permission warnings: a writable config can be tampered with, but the
// tamper revokes its own trust. A writable store lets someone forge an
// approval outright. Allow's 0o700 does not cover it, because os.MkdirAll
// only applies its mode to directories it creates.
func TestRun_WarnsWhenTrustStoreIsGroupWritable(t *testing.T) {
	requirePermissionBits(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission bits are not enforced")
	}
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	store := filepath.Join(home, ".local", "share", "envoke", "allow")
	if err := os.Chmod(store, 0o777); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 -- this warns, it never blocks", code)
	}
	if !strings.Contains(stderr, "trust store") || !strings.Contains(stderr, "forge") {
		t.Errorf("expected a warning that the store is writable, got %q", stderr)
	}
}

// The store check stats the store's ancestors, on the path every `cd` goes
// through, to warn about a forged approval -- which can only take effect where
// a block would actually run. Hence its position below the zero-match
// early-out, and hence both halves asserted together: the cheap version that
// warns about nothing is one line away from the one that warns about nothing
// ever.
func TestRun_ShellHookChecksTheTrustStoreOnlyWhereABlockWouldRun(t *testing.T) {
	requirePermissionBits(t)
	if os.Geteuid() == 0 {
		t.Skip("running as root, permission bits are not enforced")
	}
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	store := filepath.Join(home, ".local", "share", "envoke", "allow")
	if err := os.Chmod(store, 0o777); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", "--", tp("/x"), tp("/y"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(stderr, "trust store") {
		t.Errorf("a cd that matches nothing must not pay for the store check, got %q", stderr)
	}

	_, stderr, code = runFor(t, "shell-hook", "--", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, "trust store") {
		t.Errorf("a matching cd must still be warned, got %q", stderr)
	}
}

func TestRun_ListRevokePruneLifecycle(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	configPath := filepath.Join(home, ".envokerc")

	// The config exists but has never been approved. That is the case worth
	// surfacing -- a file being loaded and skipped on every cd -- and listing
	// only trust records used to hide it entirely.
	t.Run("nothing trusted yet", func(t *testing.T) {
		stdout, _, code := runFor(t, "list")
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
		if !strings.Contains(stdout, configPath) || !strings.Contains(stdout, "untrusted") {
			t.Errorf("expected %s listed as untrusted, got %q", configPath, stdout)
		}
	})

	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	t.Run("lists the trusted config", func(t *testing.T) {
		stdout, _, code := runFor(t, "list")
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
		if !strings.Contains(stdout, configPath) || !strings.Contains(stdout, "trusted") {
			t.Errorf("expected %s listed as trusted, got %q", configPath, stdout)
		}
	})

	t.Run("reports an edited config as changed", func(t *testing.T) {
		writeConfig(t, home, "enter /a\n    echo edited\n")
		stdout, _, code := runFor(t, "list")
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
		if !strings.Contains(stdout, "changed") {
			t.Errorf("expected the edited config reported as changed, got %q", stdout)
		}
		writeConfig(t, home, "enter /a\n    echo hi\n")
	})

	t.Run("revoke withdraws trust", func(t *testing.T) {
		stdout, _, code := runFor(t, "revoke")
		if code != 0 || !strings.Contains(stdout, "revoked") {
			t.Fatalf("revoke: %q code %d", stdout, code)
		}
		// The config is still in the set -- revoke withdraws the approval, it
		// does not remove the file -- so it goes back to reading as untrusted.
		if stdout, _, _ := runFor(t, "list"); !strings.Contains(stdout, "untrusted") {
			t.Errorf("expected the config to read as untrusted after revoke, got %q", stdout)
		}
		// shell-hook must actually stop acting on it, not just stop listing it.
		if stdout, _, _ := runFor(t, "shell-hook", tp("/"), tp("/a")); stdout != "" {
			t.Errorf("a revoked config must not render, got %q", stdout)
		}
	})

	t.Run("revoking again is a no-op, not an error", func(t *testing.T) {
		stdout, _, code := runFor(t, "revoke")
		if code != 0 || !strings.Contains(stdout, "nothing to revoke") {
			t.Errorf("second revoke: %q code %d, want a clean no-op", stdout, code)
		}
	})

	t.Run("prune drops records for deleted configs", func(t *testing.T) {
		if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
			t.Fatalf("allow failed")
		}
		if err := os.Remove(configPath); err != nil {
			t.Fatalf("Remove: %v", err)
		}

		stdout, _, code := runFor(t, "prune")
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
		if !strings.Contains(stdout, "removed the trust record") || !strings.Contains(stdout, configPath) {
			t.Errorf("expected the stale record removed, got %q", stdout)
		}
		if stdout, _, _ := runFor(t, "prune"); !strings.Contains(stdout, "nothing to prune") {
			t.Errorf("expected a second prune to be a no-op, got %q", stdout)
		}
	})
}

// A config whose file has been deleted is listed as missing rather than
// omitted -- its plaintext copy is still sitting in the trust store, which
// is exactly what `envoke prune` is for.
func TestRun_ListReportsMissingConfig(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	if err := os.Remove(filepath.Join(home, ".envokerc")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	stdout, _, code := runFor(t, "list")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stdout, "missing") {
		t.Errorf("expected the deleted config reported as missing, got %q", stdout)
	}
}

func TestRun_RevokeExplicitPath(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	path := filepath.Join(home, ".envokerc")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, stderr, code := runFor(t, "revoke", path)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "revoked") {
		t.Errorf("expected trust revoked, got %q", stdout)
	}
}

// A path that is not a config envoke would load is still revocable: the store
// is keyed on paths, and a record for a file outside the set is exactly what a
// user pointing $ENVOKERC somewhere else leaves behind.
func TestRun_RevokeExplicitPathOutsideTheSet(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}
	other := filepath.Join(t.TempDir(), "elsewhere.conf")

	stdout, stderr, code := runFor(t, "revoke", other)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "nothing to revoke") || !strings.Contains(stdout, other) {
		t.Errorf("expected a named no-op for %s, got %q", other, stdout)
	}
	// Naming a path must not touch anything else, least of all the set.
	if got := trustRecords(t, home); len(got) != 1 {
		t.Errorf("expected the central config's record untouched, store holds %v", got)
	}
}

// revoke prompts for nothing, so it has no --yes to pick back out of the
// positionals the way allow does: an unknown flag after a path stays a usage
// error rather than being read as a second path.
func TestRun_RevokeWrongArgCount(t *testing.T) {
	for _, args := range [][]string{{"revoke", "a", "b"}, {"revoke", "a", "--yes"}, {"revoke", "--yes"}} {
		if _, _, code := runFor(t, args...); code != 2 {
			t.Errorf("%v: exit code = %d, want 2", args, code)
		}
	}
}

func TestRun_VersionFlagsMatchVersionSubcommand(t *testing.T) {
	want, _, _ := runFor(t, "version")
	for _, arg := range []string{"--version", "-V"} {
		got, _, code := runFor(t, arg)
		if code != 0 || got != want {
			t.Errorf("%s: got %q code %d, want same as `envoke version` (%q)", arg, got, code, want)
		}
	}
}

func TestRun_ShellInitDetectsShellFromEnv(t *testing.T) {
	cases := map[string]string{
		"/bin/bash":              "PROMPT_COMMAND",
		"/usr/bin/zsh":           "chpwd_functions",
		"/usr/local/bin/fish":    "--on-variable PWD",
		"/bin/tcsh":              "cwdcmd",
		"/bin/csh":               "cwdcmd",
		"/usr/bin/pwsh":          "function global:prompt",
		"/opt/homebrew/bin/-zsh": "chpwd_functions",
	}
	for shellPath, want := range cases {
		t.Run(shellPath, func(t *testing.T) {
			t.Setenv("SHELL", shellPath)
			stdout, stderr, code := runFor(t, "shell-init")
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr)
			}
			if !strings.Contains(stdout, want) {
				t.Errorf("expected the hook for %s (containing %q), got:\n%s", shellPath, want, stdout)
			}
		})
	}
}

// Guessing wrong would write a broken rc file whose breakage surfaces much
// later than an error here, so an unrecognised $SHELL is never silently
// defaulted.
func TestRun_ShellInitUndetectableShellIsError(t *testing.T) {
	for _, shellPath := range []string{"", "/usr/bin/ksh"} {
		t.Setenv("SHELL", shellPath)
		_, stderr, code := runFor(t, "shell-init")
		if code != 2 {
			t.Errorf("SHELL=%q: exit code = %d, want 2", shellPath, code)
		}
		if !strings.Contains(stderr, "explicitly") {
			t.Errorf("SHELL=%q: expected a hint to name the shell explicitly, got %q", shellPath, stderr)
		}
	}
}

// TestRun_ShellHookDoubleDashSeparatesPaths covers the reason the generated
// hooks pass `--`: without it, a directory whose name starts with `-` would
// be parsed as a flag by the very command meant to react to entering it.
func TestRun_ShellHookDoubleDashSeparatesPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The literals here stay Unix-only rather than going through tp,
		// because an absolute path on Windows always begins with a volume or
		// `\\`: no directory argument shell-hook could ever be handed starts
		// with `-`, so there is nothing for `--` to separate and no way to
		// spell the case this asserts.
		t.Skip("no absolute Windows path can look like a flag")
	}
	home := isolateHome(t)
	writeConfig(t, home, "enter /--shell\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	stdout, stderr, code := runFor(t, "shell-hook", "--", "/", "/--shell")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "echo hi") {
		t.Errorf("expected the block for a flag-looking directory to render, got %q", stdout)
	}
}

func TestRun_AllowAcceptsYesAfterPath(t *testing.T) {
	// `envoke allow <path> --yes` shipped as documented behaviour, and
	// stdlib flag stops at the first positional, so it is handled explicitly.
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")

	stdout, stderr, code := runFor(t, "allow", filepath.Join(home, ".envokerc"), "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "trusted") {
		t.Errorf("expected the config to be trusted, got %q", stdout)
	}
}

// TestTransitionArgs covers the argument handling `exec` and `debug` share.
// Both are typed by a human, unlike shell-hook which only ever receives
// generated arguments — so both take relative paths (`envoke debug . /tmp`
// used to fail outright with "not absolute"), both infer <to> when it is left
// off, and both fill in <from> from $OLDPWD where the shell exports one.
func TestTransitionArgs(t *testing.T) {
	t.Run("relative paths are made absolute", func(t *testing.T) {
		from, to, err := transitionArgs("debug", []string{".", "sub"})
		if err != nil {
			t.Fatalf("transitionArgs: %v", err)
		}
		if !filepath.IsAbs(from) || !filepath.IsAbs(to) {
			t.Errorf("expected absolute paths, got %q and %q", from, to)
		}
		if filepath.Base(to) != "sub" {
			t.Errorf("to = %q, want it to end in %q", to, "sub")
		}
	})

	t.Run("no arguments uses OLDPWD and PWD", func(t *testing.T) {
		t.Setenv("OLDPWD", tp("/tmp/from"))
		t.Setenv("PWD", tp("/tmp/to"))
		from, to, err := transitionArgs("debug", nil)
		if err != nil {
			t.Fatalf("transitionArgs: %v", err)
		}
		if from != np("/tmp/from") || to != np("/tmp/to") {
			t.Errorf("got %q -> %q, want /tmp/from -> /tmp/to", from, to)
		}
	})

	// The asymmetric half of the pair: <to> is inferable and <from> is not, so
	// the single argument can only be <from>.
	t.Run("one argument is from, with to inferred", func(t *testing.T) {
		t.Setenv("PWD", tp("/tmp/here"))
		unsetEnv(t, "OLDPWD")
		from, to, err := transitionArgs("debug", []string{tp("/tmp/from")})
		if err != nil {
			t.Fatalf("transitionArgs: %v", err)
		}
		if from != np("/tmp/from") || to != np("/tmp/here") {
			t.Errorf("got %q -> %q, want /tmp/from -> /tmp/here", from, to)
		}
	})

	// PowerShell's state on every invocation: neither variable exists, since
	// $PWD there is a shell variable and there is no OLDPWD at all.
	t.Run("with neither variable set the error names the one-argument form", func(t *testing.T) {
		unsetEnv(t, "OLDPWD")
		unsetEnv(t, "PWD")
		_, _, err := transitionArgs("debug", nil)
		if err == nil {
			t.Fatalf("expected an error when there's no previous directory to infer")
		}
		if !strings.Contains(err.Error(), "envoke debug <from>") {
			t.Errorf("the error must say what to type instead, got %q", err)
		}
		// `envoke debug $OLDPWD $PWD` is exactly what cannot be typed in the
		// shell that produces this error, on any platform.
		if strings.Contains(err.Error(), "$PWD") {
			t.Errorf("the error must not suggest a POSIX-only form, got %q", err)
		}
	})

	t.Run("three arguments is an error", func(t *testing.T) {
		if _, _, err := transitionArgs("debug", []string{tp("/a"), tp("/b"), tp("/c")}); err == nil {
			t.Errorf("expected an error for three arguments")
		}
	})
}

func TestRun_DebugDefaultsToShellTransition(t *testing.T) {
	home := isolateHome(t)
	target := filepath.Join(home, "proj")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeConfig(t, home, "enter "+filepath.ToSlash(target)+"\n    echo hi\n")

	t.Setenv("OLDPWD", home)
	t.Setenv("PWD", target)

	stdout, stderr, code := runFor(t, "debug")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "enter "+target) {
		t.Errorf("expected $OLDPWD -> $PWD to be used, got %q", stdout)
	}
}

// `envoke debug <from>` is the form every shell can type: PowerShell exports
// neither OLDPWD nor PWD, so the no-argument form can never work there, and
// there is no way to spell `$PWD` as an argument either.
func TestRun_DebugInfersOnlyTheDestination(t *testing.T) {
	home := isolateHome(t)
	target := filepath.Join(home, "proj")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeConfig(t, home, "enter "+filepath.ToSlash(target)+"\n    echo hi\n")

	unsetEnv(t, "OLDPWD")
	t.Setenv("PWD", target)

	stdout, stderr, code := runFor(t, "debug", home)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "envoke debug: "+home+" -> "+target) {
		t.Errorf("expected the current directory to be used as <to>, got %q", stdout)
	}
	if !strings.Contains(stdout, "enter "+target) {
		t.Errorf("expected the enter block for %s to be listed, got %q", target, stdout)
	}
}

// The message a Windows user gets for `envoke debug` with no arguments. It has
// to name a command they can actually retype, which rules out both variables:
// the one that is missing is why they are reading this, and the other has no
// argument spelling in PowerShell.
func TestRun_DebugWithNothingToInferSaysWhatToType(t *testing.T) {
	isolateHome(t)
	unsetEnv(t, "OLDPWD")
	unsetEnv(t, "PWD")

	_, stderr, code := runFor(t, "debug")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, "envoke debug <from>") {
		t.Errorf("expected the error to name the form that works, got %q", stderr)
	}
	if strings.Contains(stderr, "$PWD") {
		t.Errorf("expected no POSIX-only form to be suggested, got %q", stderr)
	}
}

func TestRun_ExecRunsTrustedBlocksInSubprocesses(t *testing.T) {
	requirePOSIXShell(t)
	home := isolateHome(t)
	target := filepath.Join(home, "proj")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	marker := filepath.Join(home, "marker")
	writeConfig(t, home, "enter "+filepath.ToSlash(target)+"\n    echo ran > "+filepath.ToSlash(marker)+"\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	if _, stderr, code := runFor(t, "exec", home, target); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("expected the enter block to have run: %v", err)
	}
}

// TestRun_ExecRefusesUntrustedConfig is the CLI-visible half of the trust
// gate that now lives inside envoke.Transition. `envoke exec` is the only
// subcommand that spawns shells directly, so it is the one that would have
// been the first caller of the previously unguarded code path.
func TestRun_ExecRefusesUntrustedConfig(t *testing.T) {
	home := isolateHome(t)
	target := filepath.Join(home, "proj")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	marker := filepath.Join(home, "marker")
	writeConfig(t, home, "enter "+filepath.ToSlash(target)+"\n    echo ran > "+filepath.ToSlash(marker)+"\n")

	_, stderr, code := runFor(t, "exec", home, target)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "not trusted") || !strings.Contains(stderr, "envoke allow") {
		t.Errorf("expected an untrusted-config error hinting at `envoke allow`, got %q", stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("an unapproved block must not have run")
	}
}

func TestRun_ExecWrongArgCount(t *testing.T) {
	if _, _, code := runFor(t, "exec", tp("/a"), tp("/b"), tp("/c")); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// One argument is <from>: the half of the pair envoke cannot work out for
// itself. Asserted here as well as in TestTransitionArgs because exec shares
// that resolver with debug, and a form only one of the two accepts is a form
// the shared path has stopped being shared. No config exists, so this fails on
// the config set rather than on the arguments -- which is the point.
func TestRun_ExecAcceptsFromAlone(t *testing.T) {
	home := isolateHome(t)

	_, stderr, code := runFor(t, "exec", home)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, "no central config") {
		t.Errorf("expected the argument form to be accepted and the missing config reported, got %q", stderr)
	}
}

// `envoke exec <from>` can be read as "run the blocks for this directory",
// which is the opposite direction and, for exec, means running that
// directory's leave blocks. debug leads with the pair it resolved; this is
// exec's equivalent, and it is scoped to the one form whose meaning a reader
// can get backwards. The negative half is what protects existing callers: a
// script using the no-argument or two-argument form must go on printing
// exactly what it printed before.
//
// On stderr, which is also the only stream run hands cmdExec: stdout belongs
// to the blocks, and a caller capturing it must not collect a diagnostic.
func TestRun_ExecEchoesTheTransitionOnlyForTheOneArgumentForm(t *testing.T) {
	t.Run("one argument echoes the resolved pair", func(t *testing.T) {
		home := isolateHome(t)
		here := filepath.Join(home, "here")
		if err := os.Mkdir(here, 0o755); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		unsetEnv(t, "OLDPWD")
		t.Setenv("PWD", here)

		// No config, so nothing runs: the echo is printed before the set is
		// even loaded, and exit 1 confirms these got past argument handling
		// rather than failing on it, which is what would make the two negative
		// cases below pass for the wrong reason.
		_, stderr, code := runFor(t, "exec", home)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1 (stderr %q)", code, stderr)
		}
		if !strings.Contains(stderr, "envoke exec: "+home+" -> "+here) {
			t.Errorf("expected the inferred transition echoed, got %q", stderr)
		}
	})

	t.Run("two arguments echo nothing", func(t *testing.T) {
		home := isolateHome(t)

		_, stderr, code := runFor(t, "exec", home, filepath.Join(home, "here"))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1 (stderr %q)", code, stderr)
		}
		if strings.Contains(stderr, "envoke exec:") {
			t.Errorf("the two-argument form states the pair already, got %q", stderr)
		}
	})

	t.Run("no arguments echo nothing", func(t *testing.T) {
		home := isolateHome(t)
		here := filepath.Join(home, "here")
		t.Setenv("OLDPWD", home)
		t.Setenv("PWD", here)

		_, stderr, code := runFor(t, "exec")
		if code != 1 {
			t.Fatalf("exit code = %d, want 1 (stderr %q)", code, stderr)
		}
		if strings.Contains(stderr, "envoke exec:") {
			t.Errorf("a script on the no-argument form must see no new output, got %q", stderr)
		}
	})
}

func TestRun_ExecNoConfigIsError(t *testing.T) {
	isolateHome(t)
	_, stderr, code := runFor(t, "exec", tp("/a"), tp("/b"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "no config found") {
		t.Errorf("expected a no-config error, got %q", stderr)
	}
}

func TestRun_AllowRecordsTheContentItReviewed(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo reviewed\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	writeConfig(t, home, "enter /a\n    echo swapped-in-after-review\n")

	stdout, stderr, code := runFor(t, "shell-hook", tp("/"), tp("/a"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("content that was never reviewed must not run, got stdout %q", stdout)
	}
	if !strings.Contains(stderr, "not trusted") {
		t.Errorf("expected the swapped-in config to report as untrusted, got %q", stderr)
	}
}

// TestRun_AllowReapprovesWhenTrustRecordIsHalfWritten covers the "unchanged
// since it was last trusted" shortcut refusing to fire on a torn trust
// record. trust.Allow writes the content copy before the hash record, so a
// crash in between leaves a .content that matches the config while the hash
// record still holds the previous value. Comparing content alone would then
// report "nothing to review" forever on a config that shell-hook considers
// untrusted -- an unfixable state reachable only through `envoke allow`.
func TestRun_AllowReapprovesWhenTrustRecordIsHalfWritten(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /a\n    echo hi\n")
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow failed")
	}

	// Simulate the torn write: keep the content copy, corrupt the hash.
	allowDir := filepath.Join(home, ".local", "share", "envoke", "allow")
	entries, err := os.ReadDir(allowDir)
	if err != nil {
		t.Fatalf("ReadDir trust store: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".content") {
			continue
		}
		if err := os.WriteFile(filepath.Join(allowDir, e.Name()), []byte("stale-hash"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	stdout, stderr, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "nothing to review") {
		t.Errorf("a config the trust record does not actually cover must be re-approved, got %q", stdout)
	}
	if !strings.Contains(stdout, "trusted") {
		t.Errorf("expected the config to be re-trusted, got %q", stdout)
	}

	if stdout, _, code := runFor(t, "shell-hook", tp("/"), tp("/a")); code != 0 || !strings.Contains(stdout, "echo hi") {
		t.Errorf("expected the re-approved config to run, got stdout %q code %d", stdout, code)
	}
}

func isolateHome(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
	t.Setenv("USERPROFILE", home)
	t.Setenv("ENVOKERC", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	// Every variable the CLI reads gets pinned, not just the ones a given
	// test cares about. A test that passes because of what happens to be in
	// the developer's or the runner's environment is worse than no test:
	// TestRun_ShellInitWrongArgCount asserted the wrong thing for a while
	// and passed anyway, purely because the Linux CI container has no
	// $SHELL, and only the macOS runner ever showed it.
	//
	// currentDir reads $PWD ahead of os.Getwd, so exec, debug and reload take
	// their <to> from it on every form that leaves it off. Tests that care set it
	// to what they mean; this is so the rest don't read the ambient shell's.
	t.Setenv("PWD", home)
	unsetEnv(t, "ENVOKE_FROM")
	unsetEnv(t, "ENVOKE_TO")
	unsetEnv(t, "ENVOKE_DISABLE")
	t.Setenv("ENVOKERC_D", "")
	return home
}

// unsetEnv removes a variable for the duration of the test. t.Setenv is
// called first purely for its cleanup, which restores the original value (or
// its absence) afterwards -- os.Unsetenv alone would leak across tests.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("Unsetenv(%s): %v", name, err)
	}
}

// TestRun_CompletionCoversEverySubcommand is the cross-check that keeps the
// completion candidate list honest. internal/shellinit can't see the
// dispatcher, so the authority here is `envoke help`, which is the one
// thing a contributor adding a subcommand certainly updates. Anything it
// documents must be completable. The reverse is not checked, here or anywhere:
// a completion candidate naming a command that no longer exists passes, so a
// removed subcommand has to be taken out of internal/shellinit's list by hand.
func TestRun_CompletionCoversEverySubcommand(t *testing.T) {
	help, _, code := runFor(t, "help")
	if code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	script, _, code := runFor(t, "completion", "bash")
	if code != 0 {
		t.Fatalf("completion exit code = %d", code)
	}

	var documented []string
	for _, line := range strings.Split(help, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "envoke" {
			continue
		}
		name := fields[1]
		if strings.HasPrefix(name, "[") || strings.HasPrefix(name, "<") {
			continue
		}
		documented = append(documented, name)
	}
	if len(documented) < 5 {
		t.Fatalf("failed to parse the subcommand list out of `envoke help`, got %q", documented)
	}

	for _, name := range documented {
		// shell-hook is documented as internal but still completable, so
		// every documented name is expected here.
		if !strings.Contains(script, name) {
			t.Errorf("subcommand %q is documented in `envoke help` but never offered by completion", name)
		}
	}
}

func TestRun_CompletionDetectsShellAndRejectsUnsupported(t *testing.T) {
	t.Run("detects from $SHELL", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/bin/zsh")
		stdout, stderr, code := runFor(t, "completion")
		if code != 0 {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr)
		}
		if !strings.Contains(stdout, "compdef _envoke envoke") {
			t.Errorf("expected the zsh completion, got:\n%s", stdout)
		}
	})

	t.Run("too many arguments is a usage error", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/bash")
		_, _, code := runFor(t, "completion", "bash", "zsh")
		if code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
	})

	t.Run("unsupported shell is an explicit error", func(t *testing.T) {
		_, stderr, code := runFor(t, "completion", "tcsh")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "bash, zsh, fish") {
			t.Errorf("expected the error to name what is supported, got %q", stderr)
		}
	})
}

// writeConfig writes the central config, ~/.envokerc. The body goes through
// configBody, so a pattern written as "/a" is one the platform can match.
func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, ".envokerc")
	if err := os.WriteFile(path, []byte(configBody(body)), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// fragmentDir points $ENVOKERC_D at a fresh directory and returns it, in the
// spelling envoke will report the files it finds there.
//
// Resolved, because the fragment walk resolves the directory before walking it
// (see resolvedPath): a test comparing its own $TMPDIR-derived path against
// that output otherwise depends on $TMPDIR being neither a symlink -- it is one
// on every Mac -- nor an 8.3 short name, which %TMP% on a Windows runner
// usually is (`C:\Users\RUNNER~1\...`).
func fragmentDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dir = resolvedPath(t, dir)
	t.Setenv("ENVOKERC_D", dir)
	return dir
}

func writeFragment(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(configBody(body)), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRun_ShellHookRunsATrustedFragment(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	writeFragment(t, dir, "10-work", "enter /work\n    export FROM_FRAGMENT=1\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	stdout, stderr, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "export FROM_FRAGMENT=1") {
		t.Errorf("expected the fragment's block to be rendered, got %q", stdout)
	}
}

// An untrusted fragment is reported, never asked about: every file in the set
// lives in a directory the user owns, so `envoke allow` is the answer.
func TestRun_ShellHookReportsAnUntrustedFragmentWithoutAsking(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	writeFragment(t, dir, "10-work", "enter /work\n    export NOPE=1\n")

	var out, errBuf bytes.Buffer
	stdin := strings.NewReader("y\ny\n")
	code := run([]string{"shell-hook", "--", tp("/"), tp("/work")}, &out, &errBuf, stdin)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if out.String() != "" {
		t.Errorf("nothing may run without an approval, got %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "envoke allow") {
		t.Errorf("expected the hint on stderr, got %q", errBuf.String())
	}
	if stdin.Len() != len("y\ny\n") {
		t.Errorf("shell-hook must never read stdin, %d bytes consumed", len("y\ny\n")-stdin.Len())
	}
}

// Filename order is what the directory is for.
func TestRun_ShellHookAppliesFragmentsInFilenameOrder(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	writeFragment(t, dir, "20-second", "enter /work\n    echo second\n")
	writeFragment(t, dir, "10-first", "enter /work\n    echo first\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	stdout, _, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if first, second := strings.Index(stdout, "echo first"), strings.Index(stdout, "echo second"); first < 0 || second < 0 || first > second {
		t.Errorf("expected 10-first's block before 20-second's, got %q", stdout)
	}
}

// The central config and the fragments are complementary, and the central one
// is the outermost: it applies first on the way in.
func TestRun_ShellHookAppliesCentralConfigBeforeFragments(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /work\n    echo central\n")
	writeFragment(t, dir, "10-work", "enter /work\n    echo fragment\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	stdout, _, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if c, f := strings.Index(stdout, "echo central"), strings.Index(stdout, "echo fragment"); c < 0 || f < 0 || c > f {
		t.Errorf("expected the central config's block first, got %q", stdout)
	}
}

// A fragment symlinked into a project is how a committed config joins the
// set: its "./" resolves against the project, and it may only match inside it.
func TestRun_SymlinkedFragmentUsesProjectRelativePatterns(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	project := t.TempDir()

	target := filepath.Join(project, "envoke.conf")
	if err := os.WriteFile(target, []byte("enter ./src\n    export IN_SRC=1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	stdout, stderr, code := runFor(t, "shell-hook", "--", project, filepath.Join(project, "src"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "export IN_SRC=1") {
		t.Errorf("expected ./src to resolve against the project, got %q", stdout)
	}
}

// However its patterns are written, a fragment that points into a project
// cannot fire outside it — the content is what someone else's commit can
// rewrite.
func TestRun_SymlinkedFragmentCannotMatchOutsideItsProject(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	project := t.TempDir()
	outside := t.TempDir()

	target := filepath.Join(project, "envoke.conf")
	// The pattern names the resolved spelling of the directory it must not
	// reach: for a confined config both the bound and the pattern apply to the
	// symlink-resolved path, so a pattern written any other way would fail to
	// match for a reason other than the bound -- and pass this test without
	// exercising it.
	body := "enter " + filepath.ToSlash(resolvedPath(t, outside)) + "\n    export ESCAPED=1\n"
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	stdout, _, code := runFor(t, "shell-hook", "--", filepath.Dir(outside), outside)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("a project fragment must not fire outside its project, got %q", stdout)
	}
}

// With no path, allow covers the whole set: splitting rules across files is an
// organisational choice, not a decision to approve them one at a time.
func TestRun_AllowCoversEveryFragment(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /a\n    echo central\n")
	writeFragment(t, dir, "10-one", "enter /b\n    echo one\n")
	writeFragment(t, dir, "20-two", "enter /c\n    echo two\n")

	stdout, stderr, code := runForStdin(t, "y\n", "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{"echo central", "echo one", "echo two"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("review should show %q, got %q", want, stdout)
		}
	}

	listOut, _, code := runFor(t, "list")
	if code != 0 {
		t.Fatalf("list exit code = %d", code)
	}
	// "untrusted" contains "trusted", so the count on its own was satisfied by two
	// trusted configs and one that was not -- and this is the only thing the test
	// asserts about the result of approving the set.
	if strings.Contains(listOut, "untrusted") {
		t.Errorf("no config in the set may read as untrusted, got %q", listOut)
	}
	if got := strings.Count(listOut, "trusted"); got != 3 {
		t.Errorf("expected all three configs trusted, got %d in %q", got, listOut)
	}
}

// One broken fragment must not block approving the rest of the set.
func TestRun_AllowReportsABrokenFragmentAndApprovesTheRest(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	writeFragment(t, dir, "10-broken", "this is not a block\n")
	writeFragment(t, dir, "20-good", "enter /b\n    echo good\n")

	stdout, stderr, code := runForStdin(t, "y\n", "allow")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 to report the broken fragment", code)
	}
	if !strings.Contains(stderr, "10-broken") {
		t.Errorf("expected the broken fragment named on stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "trusted") || !strings.Contains(stdout, "20-good") {
		t.Errorf("expected the good fragment to be approved anyway, got %q", stdout)
	}
}

// trustRecords reports the config paths the trust store currently holds a trust
// token for, keyed by path. It reads the store off disk rather than through
// internal/trust so a test asserting what revoke removed doesn't go through the
// code that removed it, and it keys off the hash record rather than the .path
// sibling because the hash record is the token: a record whose siblings survived
// is still no approval.
func trustRecords(t *testing.T, home string) map[string]bool {
	t.Helper()
	dir := filepath.Join(home, ".local", "share", "envoke", "allow")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadDir trust store: %v", err)
	}

	paths := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || strings.Contains(e.Name(), ".") {
			continue
		}
		recorded, err := os.ReadFile(filepath.Join(dir, e.Name()+".path"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		paths[string(recorded)] = true
	}
	return paths
}

// recordedPath is the store's record for the config whose path ends in name.
//
// A fragment's recorded path is not always the string writeFragment returned:
// the fragment walk resolves the directory before walking it, so under a $TMPDIR
// that is itself a symlink the two differ by prefix. Asserting against the
// recorded path tests what envoke actually keyed on.
func recordedPath(t *testing.T, home, name string) string {
	t.Helper()
	var found string
	for path := range trustRecords(t, home) {
		if filepath.Base(path) != name {
			continue
		}
		if found != "" {
			t.Fatalf("two records end in %s: %s and %s", name, found, path)
		}
		found = path
	}
	if found == "" {
		t.Fatalf("no trust record for a config named %s", name)
	}
	return found
}

// With no path, revoke covers the whole set, for the same reason allow does --
// otherwise the headline command of the fragment feature has no inverse.
func TestRun_RevokeCoversTheWholeSet(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /a\n    echo central\n")
	writeFragment(t, dir, "10-one", "enter /a\n    echo one\n")
	writeFragment(t, dir, "20-two", "enter /a\n    echo two\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	allowed := trustRecords(t, home)
	if len(allowed) != 3 {
		t.Fatalf("expected three records after allow, store holds %v", allowed)
	}

	stdout, stderr, code := runFor(t, "revoke")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	// Every path allow recorded is a path revoke reports removing, which is the
	// equality the store keys on.
	for path := range allowed {
		if !strings.Contains(stdout, "revoked trust for "+path) {
			t.Errorf("expected %s named as revoked, got %q", path, stdout)
		}
	}
	if got := trustRecords(t, home); len(got) != 0 {
		t.Errorf("expected an empty trust store, it holds %v", got)
	}
	// The set must actually stop running, not merely stop being listed.
	if out, _, _ := runFor(t, "shell-hook", tp("/"), tp("/a")); out != "" {
		t.Errorf("a revoked set must not render, got %q", out)
	}
}

// The case that used to fail outright: with fragments and no central config,
// revoke resolved a single path through config.Locate and reported "no config
// found", leaving no way to withdraw what allow had granted.
func TestRun_RevokeWithoutACentralConfig(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeFragment(t, dir, "10-work", "enter /a\n    echo fragment\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	fragment := recordedPath(t, home, "10-work")

	stdout, stderr, code := runFor(t, "revoke")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "revoked trust for "+fragment) {
		t.Errorf("expected the fragment named as revoked, got %q", stdout)
	}
	if got := trustRecords(t, home); len(got) != 0 {
		t.Errorf("expected an empty trust store, it holds %v", got)
	}
}

// Only part of a set is normally approved, so a file with no record to remove is
// reported and skipped -- and revoking nothing at all still succeeds, since the
// end state revoke asks for already holds.
func TestRun_RevokePartlyTrustedSet(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /a\n    echo central\n")
	writeFragment(t, dir, "10-one", "enter /a\n    echo one\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	approved := trustRecords(t, home)
	if len(approved) != 2 {
		t.Fatalf("expected two records after allow, store holds %v", approved)
	}
	// A fragment that joined the set after the approval -- a `git pull` in a
	// dotfiles repository is all it takes -- has no record to remove.
	writeFragment(t, dir, "20-two", "enter /a\n    echo two\n")

	stdout, stderr, code := runFor(t, "revoke")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for path := range approved {
		if !strings.Contains(stdout, "revoked trust for "+path) {
			t.Errorf("expected %s named as revoked, got %q", path, stdout)
		}
	}
	// Matched on the trailing path element for the reason recordedPath explains.
	if !strings.Contains(stdout, "20-two was not trusted") {
		t.Errorf("expected the unapproved fragment reported as nothing to revoke, got %q", stdout)
	}
	if got := trustRecords(t, home); len(got) != 0 {
		t.Errorf("expected an empty trust store, it holds %v", got)
	}

	// Nothing left to remove anywhere in the set: still a success.
	stdout, _, code = runFor(t, "revoke")
	if code != 0 {
		t.Errorf("revoking an untrusted set: exit code = %d, want 0", code)
	}
	if strings.Contains(stdout, "revoked trust for") {
		t.Errorf("expected no revocations reported, got %q", stdout)
	}
}

// A path narrows revoke to that one file, so a set can still be withdrawn a
// piece at a time.
func TestRun_RevokeExplicitPathLeavesTheRestOfTheSet(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /a\n    echo central\n")
	writeFragment(t, dir, "10-one", "enter /a\n    echo one\n")
	writeFragment(t, dir, "20-two", "enter /a\n    echo two\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	one := recordedPath(t, home, "10-one")
	two := recordedPath(t, home, "20-two")

	stdout, stderr, code := runFor(t, "revoke", one)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, two) {
		t.Errorf("revoking one path must not mention another config, got %q", stdout)
	}
	got := trustRecords(t, home)
	if len(got) != 2 {
		t.Errorf("expected the other two records kept, store holds %v", got)
	}
	for path := range got {
		if path == one {
			t.Errorf("expected %s revoked, it still has a record", one)
		}
	}
}

// Trust is keyed on the path envoke reached the file by, and the fragment walk
// resolves the config directory before walking it -- so in the ordinary dotfiles
// layout, where envokerc.d is a symlink into a repository, both allow and revoke
// must key on the file inside that repository. Anything that computed one key
// from the link and the other from its target would leave a revoked config still
// trusted.
func TestRun_RevokeMatchesAllowThroughASymlinkedFragmentDir(t *testing.T) {
	home := isolateHome(t)
	dotfiles := t.TempDir()
	realDir := filepath.Join(dotfiles, "envokerc.d")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("ENVOKERC_D", link)

	writeFragment(t, realDir, "10-work", "enter /a\n    echo fragment\n")
	// A fragment that is itself a symlink into a project: the walk reports the
	// link, not its target, and the record follows the link's own path.
	project := t.TempDir()
	target := filepath.Join(project, "envoke.conf")
	if err := os.WriteFile(target, []byte("enter ./src\n    echo project\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(realDir, "20-project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	allowed := trustRecords(t, home)
	if len(allowed) != 2 {
		t.Fatalf("expected a record per fragment, store holds %v", allowed)
	}

	stdout, stderr, code := runFor(t, "revoke")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	// Every path allow recorded is a path revoke reports, which is the same
	// equality the store keys on.
	for path := range allowed {
		if !strings.Contains(stdout, "revoked trust for "+path) {
			t.Errorf("expected %s named as revoked, got %q", path, stdout)
		}
	}
	if got := trustRecords(t, home); len(got) != 0 {
		t.Errorf("expected an empty trust store, it holds %v", got)
	}
	if out, _, _ := runFor(t, "shell-hook", tp("/"), tp("/a")); out != "" {
		t.Errorf("a revoked fragment must not render, got %q", out)
	}
}

// The set's spelling of a file and the user's are not the same string, and only
// the set's is the one the hook looks up. config.Fragments resolves the config
// directory before walking it, so in the ordinary dotfiles layout -- envokerc.d
// a symlink into a repository -- `envoke allow <link>/<fragment>`, which is what
// tab completion hands you, names a file the set holds under another name.
//
// Matched on the path text that fell out of the set entirely: the review then
// described a standalone file with no bound and a "./" base taken from the link's
// own directory, and the record landed under a key nothing reads -- so allow
// printed "trusted" and the next cd still ran nothing.
func TestRun_AllowThroughASymlinkedFragmentDirTrustsWhatTheHookLooksUp(t *testing.T) {
	home := isolateHome(t)
	realDir := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("ENVOKERC_D", link)

	// A fragment symlinked in from a project, so there is a bound to state and a
	// "./" base that differs between the link's directory and the target's.
	project := t.TempDir()
	target := filepath.Join(project, "envoke.conf")
	if err := os.WriteFile(target, []byte("enter ./src\n    export IN_SRC=1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(realDir, "20-project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	stdout, stderr, code := runFor(t, "allow", "--yes", filepath.Join(link, "20-project"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	// Both notes come from the set and from nowhere else, so their absence is how
	// this used to show in the review a user is asked to approve.
	for _, want := range []string{
		"symlink to " + resolvedPath(t, target),
		"confined to " + resolvedPath(t, project),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the review should state %q, got %q", want, stdout)
		}
	}

	if got, want := recordedPath(t, home, "20-project"), filepath.Join(resolvedPath(t, realDir), "20-project"); got != want {
		t.Errorf("trust record keyed on %s, want the name the set reaches it by, %s", got, want)
	}
	// The assertion that cannot pass by accident: the hook renders the block.
	hookOut, stderr, code := runFor(t, "shell-hook", "--", project, filepath.Join(project, "src"))
	if code != 0 {
		t.Fatalf("shell-hook exit code = %d (stderr %q)", code, stderr)
	}
	if !strings.Contains(hookOut, "export IN_SRC=1") {
		t.Errorf("the approved fragment must run on the next cd, got %q (stderr %q)", hookOut, stderr)
	}
}

// The inverse spelling, for the reason configTargets exists: a record allow
// wrote under the set's name has to come off by the name the user has.
func TestRun_RevokeThroughASymlinkedFragmentDirRemovesTheSetsRecord(t *testing.T) {
	home := isolateHome(t)
	realDir := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("ENVOKERC_D", link)
	writeFragment(t, realDir, "10-work", "enter /a\n    echo fragment\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	recorded := recordedPath(t, home, "10-work")

	stdout, stderr, code := runFor(t, "revoke", filepath.Join(link, "10-work"))
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "revoked trust for "+recorded) {
		t.Errorf("expected the set's own record named as revoked, got %q", stdout)
	}
	if got := trustRecords(t, home); len(got) != 0 {
		t.Errorf("expected an empty trust store, it holds %v", got)
	}
}

// A record can be keyed on a spelling the set does not use, because trust is
// keyed on the path text and the central config is honoured exactly as written.
// Withdrawing the file has to take that record too: `prune` only drops records
// whose config file is gone and this one's is not, and nothing else in the CLI
// can name it, so what would be left behind is a plaintext copy of a config the
// user believes they have withdrawn.
func TestRun_RevokeAlsoWithdrawsARecordUnderTheSpellingTyped(t *testing.T) {
	home := isolateHome(t)
	realDir := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("ENVOKERC_D", link)
	writeFragment(t, realDir, "10-work", "enter /a\n    echo fragment\n")

	// $ENVOKERC pointed at the file through the link: the set then reaches one
	// file two ways, keeps the central spelling, and keys the record on it. That
	// also exercises the dedup -- one record, and the blocks reviewed once.
	typed := filepath.Join(link, "10-work")
	t.Setenv("ENVOKERC", typed)
	stdout, stderr, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	if got := strings.Count(stdout, "echo fragment"); got != 1 {
		t.Errorf("one file reached twice must be reviewed once, got %d dumps in %q", got, stdout)
	}
	t.Setenv("ENVOKERC", "")
	if got := trustRecords(t, home); len(got) != 1 || !got[typed] {
		t.Fatalf("expected one record keyed on %s, store holds %v", typed, got)
	}

	stdout, stderr, code = runFor(t, "revoke", typed)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "revoked trust for "+filepath.Join(resolvedPath(t, realDir), "10-work")) {
		t.Errorf("expected the file reported under the name the set reaches it by, got %q", stdout)
	}
	if got := trustRecords(t, home); len(got) != 0 {
		t.Errorf("expected an empty trust store, it holds %v", got)
	}
}

// Identity is asked only about a target no name in the set matches, and this is
// the shape that tells the two orders apart: a fragment hard-linked to the
// central config is one inode the set holds under two names, which the dedup's
// EvalSymlinks key cannot collapse because neither path is a symlink. Matching
// such a target on identity first answers with whichever entry the inode reaches
// first -- so the whole-set form recorded the central config, took the fragment
// for a file it had already reviewed, and left the name the hook looks up for it
// untrusted. Each entry is keyed on its own name.
func TestRun_AllowKeysEachEntryOfAHardLinkedConfigOnItsOwnName(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /a\n    echo shared\n")
	central := filepath.Join(home, ".envokerc")
	fragment := filepath.Join(dir, "10-hardlink")
	if err := os.Link(central, fragment); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	records := trustRecords(t, home)
	for _, want := range []string{central, fragment} {
		if !records[want] {
			t.Errorf("expected a record keyed on %s, store holds %v", want, records)
		}
	}
	// The assertion that cannot pass by accident: the set loads the file under
	// both names, so a record under only one of them stops a block from running.
	if _, stderr, _ := runFor(t, "shell-hook", "--", tp("/"), tp("/a")); strings.Contains(stderr, "not trusted") {
		t.Errorf("every name the set loads the file under must be trusted, got %q", stderr)
	}

	// And naming one of the two withdraws that one: revoke acts on the file the
	// record is keyed on, not on every path that shares its inode.
	if _, stderr, code := runFor(t, "revoke", fragment); code != 0 {
		t.Fatalf("revoke exit code = %d: %s", code, stderr)
	}
	if got := trustRecords(t, home); len(got) != 1 || !got[central] {
		t.Errorf("expected only %s still trusted, store holds %v", central, got)
	}
}

// `envoke allow --yes` is what a dotfiles bootstrap runs, on a machine whose
// $ENVOKERC may name a file it does not have. The hot path calls that state
// ordinary and stays silent about it; allow approved every fragment and then
// exited 1, which under `set -e` fails the bootstrap on a success.
func TestRun_AllowSucceedsWithAMissingCentralConfigAndFragments(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	t.Setenv("ENVOKERC", filepath.Join(t.TempDir(), "not-written-yet"))
	writeFragment(t, dir, "10-work", "enter /a\n    echo fragment\n")

	stdout, stderr, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if strings.Contains(stderr, "not-written-yet") {
		t.Errorf("a central config nobody has written yet is not a failure, got %q", stderr)
	}
	if !strings.Contains(stdout, "trusted "+recordedPath(t, home, "10-work")) {
		t.Errorf("expected the fragment approved, got %q", stdout)
	}

	// The exemption is for a path envoke chose, not for one a user typed: that
	// one is a typo and still has to fail.
	typo := filepath.Join(t.TempDir(), "typo")
	_, stderr, code = runFor(t, "allow", "--yes", typo)
	if code != 1 {
		t.Errorf("a typed missing path: exit code = %d, want 1 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, typo) {
		t.Errorf("expected the typed path named in the error, got %q", stderr)
	}
}

// resolvedPath is the spelling envoke reports for a file reached through the
// fragment walk, which resolves the config directory before walking it. A test
// comparing its own $TMPDIR-derived path against that output passes only where
// $TMPDIR happens not to be a symlink: on macOS it is one (/var ->
// /private/var), on GitHub's macOS runner included.
//
// The central config is deliberately not put through this: config.Locate hands
// back ~/.envokerc as spelled, so envoke reports the unresolved path there.
func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return resolved
}

func TestRun_DebugListsEveryConfigInTheSetWithItsStatus(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /work\n    echo central\n")
	fragment := resolvedPath(t, writeFragment(t, dir, "10-work", "enter /work\n    echo fragment\n"))

	central := filepath.Join(home, ".envokerc")
	if _, _, code := runFor(t, "allow", central, "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d", code)
	}

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// Each status is asserted against the config it belongs to: "NOT trusted"
	// contains "trusted", so checking the two words as bare substrings never
	// established that the central config was the trusted one.
	for _, want := range []string{
		"config " + central + " (trusted)",
		"config " + fragment + " (NOT trusted",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("debug output should mention %q, got %q", want, stdout)
		}
	}
	if !strings.Contains(stdout, "line 1 of "+fragment) {
		t.Errorf("expected each block to name its config, got %q", stdout)
	}
	// Neither the central config nor a fragment that really lives in envokerc.d
	// is a symlink or confined, so neither gets any of that reported about it.
	for _, unwanted := range []string{"symlink to", "confined to"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("debug should not mention %q for an ordinary config set, got %q", unwanted, stdout)
		}
	}
}

// The case that used to be undiagnosable: a fragment symlinked into a project,
// approved, listing as trusted, whose pattern points out of that project -- so
// matcher.NewMatch refuses every match and nothing ever fires. debug is where a
// user goes when a block doesn't fire, so debug has to name the bound.
func TestRun_DebugReportsTheBoundOnAConfinedFragment(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	project := t.TempDir()
	outside := t.TempDir()

	target := filepath.Join(project, "envoke.conf")
	// The pattern names the resolved spelling of the directory it must not
	// reach: for a confined config both the bound and the pattern apply to the
	// symlink-resolved path, so a pattern written any other way would fail to
	// match for a reason other than the bound -- and pass this test without
	// exercising it.
	body := "enter " + filepath.ToSlash(resolvedPath(t, outside)) + "\n    export ESCAPED=1\n"
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	stdout, _, code := runFor(t, "debug", filepath.Dir(outside), outside)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// Trusted, loaded, and firing nothing: the bound is the only thing that
	// explains the combination. "NOT trusted" contains "trusted", so the whole
	// premise of the test rests on excluding it too.
	if !strings.Contains(stdout, "(trusted)") || strings.Contains(stdout, "NOT trusted") {
		t.Fatalf("expected a trusted config, got %q", stdout)
	}
	if !strings.Contains(stdout, "no blocks would fire") {
		t.Fatalf("expected a trusted config that fires nothing, got %q", stdout)
	}
	for _, want := range []string{
		"symlink to " + resolvedPath(t, target),
		"confined to " + resolvedPath(t, project),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("debug should report %q, got %q", want, stdout)
		}
	}
}

// A fragment that is a symlink to a file inside the config directory is not
// confined -- it is a file that really lives there, reached by another name -- so
// its target is reported and no bound is.
func TestRun_DebugReportsNoBoundForALinkInsideTheConfigDir(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	// A dotfile so the walk skips the target itself: were it listed too, the
	// set's dedup would drop the link and there would be no entry to report on.
	target := writeFragment(t, dir, ".shared.conf", "enter /work\n    echo shared\n")
	if err := os.Symlink(target, filepath.Join(dir, "10-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	stdout, _, code := runFor(t, "debug", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "symlink to "+resolvedPath(t, target)) {
		t.Errorf("debug should report the link's target, got %q", stdout)
	}
	if strings.Contains(stdout, "confined to") {
		t.Errorf("a link inside the config directory is not confined, got %q", stdout)
	}
}

// Approving a fragment means approving content whose effective meaning is set by
// the file it links to and the tree it is bounded to, so the review has to state
// both -- before the confirmation, on a first approval and on a re-approval
// after someone else's commit rewrote it alike.
func TestRun_AllowReviewStatesAFragmentsBound(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	project := t.TempDir()

	target := filepath.Join(project, "envoke.conf")
	if err := os.WriteFile(target, []byte("enter ./src\n    export FIRST=1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	wantLink := "symlink to " + resolvedPath(t, target)
	wantBound := "confined to " + resolvedPath(t, project)

	stdout, stderr, code := runForStdin(t, "y\n", "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{wantLink, wantBound} {
		at := strings.Index(stdout, want)
		if at < 0 {
			t.Fatalf("the review should state %q, got %q", want, stdout)
		}
		if prompt := strings.Index(stdout, "[y/N]"); prompt >= 0 && at > prompt {
			t.Errorf("%q must be stated before the confirmation, got %q", want, stdout)
		}
	}

	// The re-approval path shows a diff rather than the blocks, and is the one a
	// rewritten project config actually goes through.
	if err := os.WriteFile(target, []byte("enter ./src\n    export SECOND=1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	stdout, stderr, code = runForStdin(t, "y\n", "allow")
	if code != 0 {
		t.Fatalf("re-approval exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "+ ") {
		t.Fatalf("expected a diff on re-approval, got %q", stdout)
	}
	for _, want := range []string{wantLink, wantBound} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the re-approval review should state %q, got %q", want, stdout)
		}
	}
}

// The same output must not appear where it means nothing: a central config that
// is a plain file is neither symlinked nor confined.
func TestRun_AllowReviewSaysNothingAboutTheBoundForACentralConfig(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, home, "enter /work\n    echo central\n")

	stdout, stderr, code := runForStdin(t, "y\n", "allow")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	for _, unwanted := range []string{"symlink to", "confined to"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("the review should not mention %q for a central config, got %q", unwanted, stdout)
		}
	}
}

func TestRun_ExecRunsATrustedFragment(t *testing.T) {
	requirePOSIXShell(t)
	isolateHome(t)
	dir := fragmentDir(t)
	work := t.TempDir()
	marker := filepath.Join(work, "marker")
	writeFragment(t, dir, "10-work", "enter "+filepath.ToSlash(work)+"\n    echo ran > "+filepath.ToSlash(marker)+"\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	if _, stderr, code := runFor(t, "exec", filepath.Dir(work), work); code != 0 {
		t.Fatalf("exec exit code = %d (stderr %q)", code, stderr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("expected the fragment's block to have run: %v", err)
	}
}

// One untrusted config must not stop the trusted ones.
func TestRun_ExecRunsTrustedConfigsDespiteAnUntrustedOne(t *testing.T) {
	requirePOSIXShell(t)
	isolateHome(t)
	dir := fragmentDir(t)
	work := t.TempDir()
	marker := filepath.Join(work, "marker")

	trusted := writeFragment(t, dir, "10-trusted", "enter "+filepath.ToSlash(work)+"\n    echo ran > "+filepath.ToSlash(marker)+"\n")
	writeFragment(t, dir, "20-untrusted", "enter "+filepath.ToSlash(work)+"\n    echo untrusted\n")
	if _, _, code := runFor(t, "allow", trusted, "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d", code)
	}

	_, stderr, code := runFor(t, "exec", filepath.Dir(work), work)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 to report the skipped config", code)
	}
	if !strings.Contains(stderr, "not trusted") {
		t.Errorf("expected the untrusted config to be reported, got %q", stderr)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the trusted config must still have run: %v", err)
	}
}

func TestRun_ReloadAppliesFragments(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	work := t.TempDir()
	writeFragment(t, dir, "10-work", "enter "+filepath.ToSlash(work)+"\n    export RELOADED=1\n")

	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}
	t.Setenv("PWD", work)

	stdout, stderr, code := runFor(t, "reload")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "export RELOADED=1") {
		t.Errorf("expected reload to apply the fragment, got %q", stdout)
	}
}

// A fragment the directory scan listed and that then fails to open is not the
// "$ENVOKERC points at a file you haven't written yet" case: the user linked
// it in on purpose, so it has to be reported rather than quietly dropped.
func TestRun_ShellHookReportsABrokenFragmentSymlink(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "project") {
		t.Errorf("expected the broken fragment named on stderr, got %q", stderr)
	}
}

// A central config that doesn't exist yet stays silent: $ENVOKERC is honoured
// verbatim, and this runs on every cd.
func TestRun_ShellHookStaysSilentForAMissingCentralConfig(t *testing.T) {
	isolateHome(t)
	t.Setenv("ENVOKERC", filepath.Join(t.TempDir(), "not-written-yet"))

	stdout, stderr, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("expected silence, got stdout %q stderr %q", stdout, stderr)
	}
}

// Everything envoke prints that came out of a config file or a directory name
// is escaped first. Both are attacker-controllable in ordinary situations — an
// extracted archive, a cloned repository — and both reach a terminal, where an
// escape sequence can redraw what the user is reading rather than appear in it.
func TestRun_EscapesControlCharactersFromConfigsAndPaths(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	work := filepath.Join(t.TempDir(), "proj\x1bAtrusted")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Skipf("cannot create a directory with an escape in its name: %v", err)
	}
	writeFragment(t, dir, "10-work",
		"enter "+filepath.ToSlash(work)+"\n    echo \x1b[2K\x1b[Aspoofed\n")

	// The untrusted notice names the config and the transition.
	_, stderr, code := runFor(t, "shell-hook", "--", filepath.Dir(work), work)
	if code != 0 {
		t.Fatalf("shell-hook exit code = %d, want 0", code)
	}
	if strings.Contains(stderr, "\x1b") {
		t.Errorf("an escape sequence reached the terminal verbatim: %q", stderr)
	}

	// The review dump prints the block bodies of a config not yet trusted.
	allowOut, _, _ := runForStdin(t, "n\n", "allow")
	if strings.Contains(allowOut, "\x1b") {
		t.Errorf("allow's review dump let an escape through: %q", allowOut)
	}

	// debug prints the paths, the matched directories and the patterns.
	debugOut, _, code := runFor(t, "debug", filepath.Dir(work), work)
	if code != 0 {
		t.Fatalf("debug exit code = %d, want 0", code)
	}
	if strings.Contains(debugOut, "\x1b") {
		t.Errorf("debug let an escape through: %q", debugOut)
	}
	if !strings.Contains(debugOut, `\x1b`) {
		t.Errorf("expected the escape shown escaped, got %q", debugOut)
	}

	// list prints the recorded path of every approved config.
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow --yes exit code = %d", code)
	}
	listOut, _, _ := runFor(t, "list")
	if strings.Contains(listOut, "\x1b") {
		t.Errorf("list let an escape through: %q", listOut)
	}
}

// An error message that quotes a config path or the pattern it is complaining
// about is the same untrusted text by another route, and has to be escaped too.
func TestRun_EscapesControlCharactersInLoadErrors(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	writeFragment(t, dir, "10-broken", "enter (\x1b[1Aunclosed\n    echo hi\n")

	_, stderr, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.Contains(stderr, "\x1b") {
		t.Errorf("a parse error let an escape through: %q", stderr)
	}
}

// writableFragmentDir points $ENVOKERC_D at a directory anyone can write, with
// one config in it whose own permissions are fine. That is the case the file
// check alone misses: the config looks safe, the directory it sits in doesn't.
func writableFragmentDir(t *testing.T) (dir, fragment string) {
	t.Helper()
	requirePermissionBits(t)
	dir = filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fragment = filepath.Join(dir, "10-work")
	if err := os.WriteFile(fragment, []byte(configBody("enter /work\n    echo hi\n")), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Setenv("ENVOKERC_D", dir)
	return dir, fragment
}

func TestRun_AllowWarnsAboutAWritableConfigDirectory(t *testing.T) {
	isolateHome(t)
	dir, _ := writableFragmentDir(t)

	_, stderr, code := runFor(t, "allow", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, dir) || !strings.Contains(stderr, "writable by group/other") {
		t.Errorf("expected a warning naming the directory, got %q", stderr)
	}
}

func TestRun_DebugWarnsAboutAWritableConfigDirectory(t *testing.T) {
	isolateHome(t)
	dir, _ := writableFragmentDir(t)

	_, stderr, code := runFor(t, "debug", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, dir) {
		t.Errorf("expected a warning naming the directory, got %q", stderr)
	}
}

// The directory check costs a second stat per config and can realistically
// only be true of your own config directory, so it deliberately stays out of
// the path every `cd` goes through. The file check remains.
func TestRun_ShellHookDoesNotStatTheConfigDirectory(t *testing.T) {
	isolateHome(t)
	dir, fragment := writableFragmentDir(t)
	if err := os.Chmod(fragment, 0o666); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	_, stderr, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stderr, fragment) {
		t.Errorf("the hook must still warn about the file itself, got %q", stderr)
	}
	if strings.Contains(stderr, "the directory "+dir) {
		t.Errorf("the hook must not pay for the directory check, got %q", stderr)
	}
}

// `envoke list` answers two questions that are not the same: what envoke would
// load, and what the store has recorded. A record for a config that is no
// longer in the set must still be listed — that is what `envoke prune` acts on
// — but it must not be mixed in with the live ones.
func TestRun_ListSeparatesTheConfigSetFromLeftoverRecords(t *testing.T) {
	home := isolateHome(t)
	dir := fragmentDir(t)
	writeConfig(t, home, "enter /a\n    echo central\n")
	fragment := writeFragment(t, dir, "10-work", "enter /b\n    echo fragment\n")

	// A record for a config that is not in the set at all.
	gone := filepath.Join(t.TempDir(), "old-config")
	if err := os.WriteFile(gone, []byte(configBody("enter /c\n    echo old\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, code := runFor(t, "allow", gone, "--yes"); code != 0 {
		t.Fatalf("allow %s exit code = %d", gone, code)
	}
	if _, _, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d", code)
	}

	stdout, _, code := runFor(t, "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	setAt := strings.Index(stdout, "configs envoke would load")
	leftoverAt := strings.Index(stdout, "other trust records")
	if setAt < 0 || leftoverAt < 0 || setAt > leftoverAt {
		t.Fatalf("expected the loaded set listed before the leftover records, got %q", stdout)
	}

	live := stdout[setAt:leftoverAt]
	for _, want := range []string{filepath.Join(home, ".envokerc"), "central", fragment, "fragment"} {
		if !strings.Contains(live, want) {
			t.Errorf("the loaded-set section should mention %q, got %q", want, live)
		}
	}
	if strings.Contains(live, gone) {
		t.Errorf("a record outside the set must not be listed as loaded, got %q", live)
	}
	if !strings.Contains(stdout[leftoverAt:], gone) {
		t.Errorf("expected %s under the leftover records, got %q", gone, stdout[leftoverAt:])
	}
}

// A config in the set that was approved and then edited reads as "changed",
// not merely untrusted: the user needs a diff, not a first review.
func TestRun_ListDistinguishesChangedFromNeverApproved(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	approved := writeFragment(t, dir, "10-approved", "enter /a\n    echo one\n")
	writeFragment(t, dir, "20-never", "enter /b\n    echo two\n")

	if _, _, code := runFor(t, "allow", approved, "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d", code)
	}
	writeFragment(t, dir, "10-approved", "enter /a\n    echo edited\n")

	stdout, _, code := runFor(t, "list")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, line := range strings.Split(stdout, "\n") {
		switch {
		case strings.Contains(line, "10-approved") && !strings.Contains(line, "changed"):
			t.Errorf("an edited config should read as changed, got %q", line)
		case strings.Contains(line, "20-never") && !strings.Contains(line, "untrusted"):
			t.Errorf("a never-approved config should read as untrusted, got %q", line)
		}
	}
}

// The escape lives in the config's *path* here, not in its content. That is
// the axis the first three rounds of this fix kept missing: the block bodies
// were escaped, then the path was, then the errors quoting the path were, and
// each round found the next line that merely described the untrusted text
// rather than being it. The fragment names below reach the terminal through
// `allow`'s review header, its "trusted" line, `debug`'s config listing, the
// load-failure report, `list` and `prune`.
func TestRun_EscapesControlCharactersInAConfigPath(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	good := "10-\x1b[1Awork"
	if err := os.WriteFile(filepath.Join(dir, good), []byte(configBody("enter /work\n    echo hi\n")), 0o644); err != nil {
		t.Skipf("cannot create a file with an escape in its name: %v", err)
	}
	writeFragment(t, dir, "20-\x1b[1Abroken", "this is not a block\n")

	// A first-time approval prints the review header and the trusted line for
	// one fragment, and the parse failure for the other.
	allowOut, allowErr, _ := runForStdin(t, "y\n", "allow")
	assertNoControlChars(t, "allow stdout", allowOut)
	assertNoControlChars(t, "allow stderr", allowErr)
	if !strings.Contains(allowOut, `\x1b`) {
		t.Errorf("expected the escape shown escaped in allow's output, got %q", allowOut)
	}

	// A second approval takes the diff path rather than the full dump.
	if err := os.WriteFile(filepath.Join(dir, good), []byte(configBody("enter /work\n    echo bye\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	diffOut, diffErr, _ := runForStdin(t, "y\n", "allow")
	assertNoControlChars(t, "allow diff stdout", diffOut)
	assertNoControlChars(t, "allow diff stderr", diffErr)

	// debug names every config in the set, including the one that fails to
	// load -- whose error embeds the path rather than being it.
	debugOut, debugErr, _ := runFor(t, "debug", tp("/"), tp("/work"))
	assertNoControlChars(t, "debug stdout", debugOut)
	assertNoControlChars(t, "debug stderr", debugErr)

	listOut, listErr, _ := runFor(t, "list")
	assertNoControlChars(t, "list stdout", listOut)
	assertNoControlChars(t, "list stderr", listErr)

	// prune reports the recorded path of a config that has since gone away.
	if err := os.Remove(filepath.Join(dir, good)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	pruneOut, pruneErr, _ := runFor(t, "prune")
	assertNoControlChars(t, "prune stdout", pruneOut)
	assertNoControlChars(t, "prune stderr", pruneErr)
}

// debug's note about where a block actually runs prints the destination
// directory, which the shell handed over and which an extracted archive or a
// cloned repository controls. It only appears when a matched directory
// differs from the destination, which is why the fixture matches an ancestor.
func TestRun_EscapesTheDirectoryNameInDebugsWorkingDirNote(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	work := filepath.Join(t.TempDir(), "work")
	nested := filepath.Join(work, "child\x1b[1A")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Skipf("cannot create a directory with an escape in its name: %v", err)
	}
	writeFragment(t, dir, "10-work", "enter "+filepath.ToSlash(work)+"\n    echo hi\n")

	stdout, stderr, code := runFor(t, "debug", filepath.Dir(work), nested)
	if code != 0 {
		t.Fatalf("debug exit code = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "note: via the shell hook") {
		t.Fatalf("expected the working-directory note, got %q", stdout)
	}
	assertNoControlChars(t, "debug stdout", stdout)
}

// A symlink's target is filesystem-derived like every other path envoke prints,
// and for a link that cannot be followed what gets printed is the link's own
// text -- whatever was written into the filesystem, escape sequences included.
func TestRun_EscapesASymlinkTargetInDebugsOutput(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	target := filepath.Join(t.TempDir(), "gone\x1b[1A.conf")
	if err := os.Symlink(target, filepath.Join(dir, "10-project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	stdout, stderr, code := runFor(t, "debug", tp("/"), tp("/work"))
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for a fragment that fails to load (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "symlink to ") || !strings.Contains(stdout, `\x1b`) {
		t.Errorf("expected the link's target shown, escaped, got %q", stdout)
	}
	assertNoControlChars(t, "debug stdout", stdout)
	assertNoControlChars(t, "debug stderr", stderr)
}

// The counterweight to every test above: the subcommands whose stdout is
// shell code for the caller to eval must emit it byte for byte. Escaping it
// would corrupt any script containing a byte outside printable ASCII, and the
// breakage would surface inside the user's shell rather than here -- so this
// is what stops the escaping from being widened onto the wrong stream.
func TestRun_GeneratedShellCodeIsNeverEscaped(t *testing.T) {
	isolateHome(t)
	dir := fragmentDir(t)
	// A real ESC byte in the block body, as a script setting a coloured
	// prompt would have, plus a non-ASCII character.
	const body = "export GREETING='\x1b[32mbonjour Ada — ok\x1b[0m'"
	writeFragment(t, dir, "10-work", "enter /work\n    "+body+"\n")
	if _, stderr, code := runFor(t, "allow", "--yes"); code != 0 {
		t.Fatalf("allow exit code = %d: %s", code, stderr)
	}

	hookOut, _, code := runFor(t, "shell-hook", "--", tp("/"), tp("/work"))
	if code != 0 {
		t.Fatalf("shell-hook exit code = %d", code)
	}
	if !strings.Contains(hookOut, body) {
		t.Errorf("shell-hook mangled the script body: %q", hookOut)
	}

	t.Setenv("PWD", tp("/work"))
	reloadOut, _, code := runFor(t, "reload")
	if code != 0 {
		t.Fatalf("reload exit code = %d", code)
	}
	if !strings.Contains(reloadOut, body) {
		t.Errorf("reload mangled the script body: %q", reloadOut)
	}

	for _, shell := range []string{"bash", "zsh", "fish", "tcsh", "powershell"} {
		want, err := shellinit.Generate(shell)
		if err != nil {
			t.Fatalf("Generate(%q): %v", shell, err)
		}
		if got, _, _ := runFor(t, "shell-init", shell); got != want {
			t.Errorf("shell-init %s output differs from the generated hook", shell)
		}
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		want, err := shellinit.Completion(shell)
		if err != nil {
			t.Fatalf("Completion(%q): %v", shell, err)
		}
		if got, _, _ := runFor(t, "completion", shell); got != want {
			t.Errorf("completion %s output differs from the generated script", shell)
		}
	}
}

// Every invisible or reordering character sanitize claims to handle. The
// first three rounds covered the Trojan Source set and stopped there; the
// marks and zero-widths do the same job with less ceremony.
func TestSanitize_InvisibleAndReorderingCharacters(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"bidi override", "\u202e", `\u202e`},
		{"bidi isolate", "\u2066", `\u2066`},
		{"right-to-left mark", "\u200f", `\u200f`},
		{"arabic letter mark", "\u061c", `\u061c`},
		{"zero width space", "\u200b", `\u200b`},
		{"line separator", "\u2028", `\u2028`},
		{"byte order mark", "\ufeff", `\ufeff`},
		{"C1 next-line", "\u0085", `\x85`},
		{"escape", "\x1b", `\x1b`},
		{"newline", "\n", `\x0a`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitize("a" + tc.in + "b"); got != "a"+tc.want+"b" {
				t.Errorf("sanitize(%q) = %q, want %q", "a"+tc.in+"b", got, "a"+tc.want+"b")
			}
		})
	}

	// A byte that is not valid UTF-8 at all, which every Unix filename may
	// contain. Decoding it to U+FFFD would hide which byte it was.
	if got := sanitize("a\xffb"); got != `a\xffb` {
		t.Errorf("sanitize of an invalid byte = %q, want %q", got, `a\xffb`)
	}
	// Ordinary non-ASCII is not a control character and must survive intact.
	if got := sanitize("café — 日本"); got != "café — 日本" {
		t.Errorf("sanitize mangled ordinary text: %q", got)
	}
	// Tab is the one C0 character worth keeping: it is layout, not control.
	if got := sanitize("a\tb"); got != "a\tb" {
		t.Errorf("sanitize escaped a tab: %q", got)
	}
}

// assertNoControlChars fails if anything that can move a terminal's cursor
// reached the stream. It checks the raw bytes rather than looking for the
// escaped form, so a new output path that forgets is caught by what it emits
// rather than by what it was expected to emit.
func assertNoControlChars(t *testing.T, what, s string) {
	t.Helper()
	if strings.ContainsAny(s, "\x1b\x07\r") {
		t.Errorf("%s let a control character through: %q", what, s)
	}
}

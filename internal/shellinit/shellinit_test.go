package shellinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGenerate_UnsupportedShellIsError(t *testing.T) {
	_, err := Generate("cmd")
	if err == nil {
		t.Fatalf("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "cmd") {
		t.Errorf("error %q should mention the shell name", err.Error())
	}
}

func TestGenerate_BashUsesPromptCommandNotCdOverride(t *testing.T) {
	script, err := Generate("bash")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(script, "PROMPT_COMMAND") {
		t.Errorf("expected bash hook to use PROMPT_COMMAND, got:\n%s", script)
	}
	assertNeverRedefinesCd(t, script)
}

func TestGenerate_ZshUsesChpwdFunctionsNotCdOverride(t *testing.T) {
	script, err := Generate("zsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(script, "chpwd_functions") {
		t.Errorf("expected zsh hook to use chpwd_functions, got:\n%s", script)
	}
	assertNeverRedefinesCd(t, script)
}

func TestGenerate_FishUsesOnVariablePwdNotCdOverride(t *testing.T) {
	script, err := Generate("fish")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(script, "--on-variable PWD") {
		t.Errorf("expected fish hook to use --on-variable PWD, got:\n%s", script)
	}
	if !strings.Contains(script, "--shell fish") {
		t.Errorf("expected fish hook to tell shell-hook its shell, got:\n%s", script)
	}
	assertNeverRedefinesCd(t, script)
}

func TestGenerate_TcshUsesCwdcmdNotCdOverride(t *testing.T) {
	script, err := Generate("tcsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(script, "cwdcmd") {
		t.Errorf("expected tcsh hook to use the cwdcmd special alias, got:\n%s", script)
	}
	if !strings.Contains(script, "--shell tcsh") {
		t.Errorf("expected tcsh hook to tell shell-hook its shell, got:\n%s", script)
	}
	assertNeverRedefinesCd(t, script)
}

func TestGenerate_PowershellWrapsPromptNotCdOverride(t *testing.T) {
	script, err := Generate("powershell")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(script, "function global:prompt") {
		t.Errorf("expected powershell hook to wrap the prompt function, got:\n%s", script)
	}
	if !strings.Contains(script, "--shell powershell") {
		t.Errorf("expected powershell hook to tell shell-hook its shell, got:\n%s", script)
	}
	assertNeverRedefinesCd(t, script)
}

// TestGenerate_PowershellHookUsesFilesystemProviderPaths is the string-level
// half of two properties: the provider gate (fire only for a filesystem
// location) and the accessor (send .ProviderPath, the resolved filesystem
// path, never .Path, which is the PowerShell spelling — see powershellHook).
//
// The behavioural half below drives a real pwsh and now runs on Windows too,
// where HKLM:, Cert: and UNC paths actually occur; this stays because a string
// check fails wherever pwsh is merely absent.
func TestGenerate_PowershellHookUsesFilesystemProviderPaths(t *testing.T) {
	script, err := Generate("powershell")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, want := range []string{"Provider.Name", "'FileSystem'", "ProviderPath"} {
		if !strings.Contains(script, want) {
			t.Errorf("expected powershell hook to report only real filesystem paths (%q missing), got:\n%s", want, script)
		}
	}
	if strings.Contains(script, ".Path") {
		t.Errorf("powershell hook must send .ProviderPath, not the drive-qualified .Path, got:\n%s", script)
	}
}

func assertNeverRedefinesCd(t *testing.T, script string) {
	t.Helper()
	// Regression: ondir's zsh integration overrode `cd` directly. Guard
	// against reintroducing that instead of using the shell's own hook.
	if strings.Contains(script, "cd()") || strings.Contains(script, "cd ()") || strings.Contains(script, "function cd") {
		t.Errorf("hook script must not redefine cd, got:\n%s", script)
	}
}

func TestGenerate_BashScriptIsSyntacticallyValid(t *testing.T) {
	requireInterpreter(t, "bash")
	script, err := Generate("bash")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertShellSyntaxOK(t, "bash", script)
}

func TestGenerate_ZshScriptIsSyntacticallyValid(t *testing.T) {
	requireInterpreter(t, "zsh")
	script, err := Generate("zsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertShellSyntaxOK(t, "zsh", script)
}

func TestGenerate_FishScriptIsSyntacticallyValid(t *testing.T) {
	requireInterpreter(t, "fish")
	script, err := Generate("fish")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertShellSyntaxOK(t, "fish", script)
}

func TestGenerate_TcshScriptIsSyntacticallyValid(t *testing.T) {
	requireInterpreter(t, "tcsh")
	script, err := Generate("tcsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	cmd := exec.Command("tcsh", "-n", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("tcsh -n rejected generated script: %v\n%s\nscript:\n%s", err, out, script)
	}
}

func TestGenerate_PowershellScriptIsSyntacticallyValid(t *testing.T) {
	requireInterpreter(t, "pwsh")
	script, err := Generate("powershell")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// PowerShell's syntax-check idiom: parse without executing. $input
	// (pipeline input as a line-by-line enumerable) is the wrong thing to
	// feed ParseInput — it drops the newlines that separate statements, so
	// the parser sees one run-on line. Read stdin as a single raw string
	// instead.
	cmd := exec.Command("pwsh", "-NoProfile", "-Command",
		"$errors = $null; $script = [Console]::In.ReadToEnd(); [System.Management.Automation.Language.Parser]::ParseInput($script, [ref]$null, [ref]$errors) | Out-Null; if ($errors) { $errors | ForEach-Object { Write-Error $_ }; exit 1 }")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("pwsh parser rejected generated script: %v\n%s\nscript:\n%s", err, out, script)
	}
}

// requirePOSIXHarness skips tests whose *harness* is Unix-specific, as
// opposed to tests whose subject is: bash, zsh, fish and tcsh are not
// interpreters Windows has, and their drivers quote paths the way a POSIX
// shell reads them.
//
// PowerShell's drivers are not among them — pwsh is native on Windows, and
// installStub hands it an `envoke` that platform can execute — so they run
// everywhere and requireHarness is what a cross-shell table calls.
func requirePOSIXHarness(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this test drives a POSIX interpreter, which Windows does not have")
	}
}

// requireHarness is requirePOSIXHarness for one entry of a cross-shell table:
// every shell but PowerShell needs a POSIX harness, and skipping the whole
// table on Windows would take the one shell whose hook point is a Windows
// concept down with the four that cannot run there.
func requireHarness(t *testing.T, shell string) {
	t.Helper()
	if shell != "powershell" {
		requirePOSIXHarness(t)
	}
}

// providerDrive names a PowerShell drive that is *not* backed by the
// filesystem, for the tests that assert the hook stays silent off it.
//
// HKLM: is the provider that prompted the gate, and only exists on Windows.
// Env: is the one that exists everywhere, including the Linux pwsh the
// drivers run against in .dagger's test-shell-powershell — so each platform
// is asked with the drive it actually has. The gate itself is not
// platform-specific.
func providerDrive() string {
	if runtime.GOOS == "windows" {
		return "HKLM:"
	}
	return "Env:"
}

// hookDir is t.TempDir() spelled the way the shell will report it back.
//
// A test comparing what the hook printed against a path it built itself needs
// one spelling throughout, and %TMP% on a Windows runner is often the 8.3
// short form (`RUNNER~1`); filepath.EvalSymlinks canonicalises that away, and
// resolves any symlink on the way, which is the same problem under a macOS
// $TMPDIR.
func hookDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return dir
}

func requireInterpreter(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available on this system, skipping syntax check", name)
	}
}

// assertShellSyntaxOK runs the interpreter's syntax-check-only mode (-n) so
// the generated hook is verified to actually parse, without executing it.
func assertShellSyntaxOK(t *testing.T, interpreter, script string) {
	t.Helper()
	cmd := exec.Command(interpreter, "-n", "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("%s -n rejected generated script: %v\n%s\nscript:\n%s", interpreter, err, out, script)
	}
}

// TestGenerate_BashHookFiresOnFirstCd is a regression test: the bash hook
// used to lazily default its "previous directory" baseline to $PWD inside
// the hook function itself, so the very first PROMPT_COMMAND firing after
// installing the hook compared the new directory against itself and missed
// the transition entirely. The baseline must be seeded once at install
// time, using the shell's directory *before* any cd happens.
func TestGenerate_BashHookFiresOnFirstCd(t *testing.T) {
	requirePOSIXHarness(t)
	requireInterpreter(t, "bash")
	script, err := Generate("bash")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "calls.log")
	writeEnvokeStub(t, stubDir, logPath)

	startDir := t.TempDir()
	targetDir := t.TempDir()

	// This mirrors real usage: the hook is eval'd once from a shell rc file
	// while sitting in startDir, then bash runs PROMPT_COMMAND itself
	// before showing the next prompt after `cd targetDir`.
	driver := "cd " + shellQuote(startDir) + "\n" +
		script + "\n" +
		"cd " + shellQuote(targetDir) + "\n" +
		`eval "$PROMPT_COMMAND"` + "\n"

	cmd := exec.Command("bash", "--noprofile", "--norc", "-i", "-c", driver)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("envoke stub was never called (log file missing): %v", err)
	}
	want := startDir + " " + targetDir + "\n"
	if string(got) != want {
		t.Errorf("first cd reported as %q, want %q", got, want)
	}
}

// TestGenerate_TcshHookFiresOnCd drives a real tcsh through the generated
// cwdcmd alias, exercising the same concern as the bash regression above:
// that a cd is reported with the correct from/to directories. Unlike bash,
// tcsh needs no baseline seeding (it maintains $owd/$cwd itself), so this
// is a plain behavioral check rather than a first-cd regression test.
func TestGenerate_TcshHookFiresOnCd(t *testing.T) {
	requirePOSIXHarness(t)
	requireInterpreter(t, "tcsh")
	script, err := Generate("tcsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "calls.log")
	writeEnvokeStub(t, stubDir, logPath)

	startDir := t.TempDir()
	targetDir := t.TempDir()

	driver := "set prompt = '% '\n" +
		"cd " + startDir + "\n" +
		script + "\n" +
		"cd " + targetDir + "\n"

	rcPath := filepath.Join(stubDir, "driver.csh")
	if err := os.WriteFile(rcPath, []byte(driver), 0o644); err != nil {
		t.Fatalf("WriteFile driver: %v", err)
	}

	cmd := exec.Command("tcsh", "-f", rcPath)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("envoke stub was never called (log file missing): %v", err)
	}
	want := startDir + " " + targetDir + "\n"
	if string(got) != want {
		t.Errorf("cd reported as %q, want %q", got, want)
	}
}

// TestGenerate_TcshHookSetenvPersistsInCallingShell is a regression test for
// a real bug: tcsh's cwdcmd special alias runs through a restricted internal
// execution path that doesn't honor `|`/`>` directly, so a naive
// `cmd | source /dev/stdin` inside cwdcmd silently no-ops (the piped text
// prints to the terminal instead of being sourced) and any setenv it
// contains never reaches the interactive shell. A test that only checks
// envoke was *called* with the right args (like the one above) doesn't
// catch this — the call happens fine, only the resulting environment change
// is lost. This drives a real tcsh through the actual generated hook with a
// stub that emits `setenv`, and asserts the variable is visible afterward.
func TestGenerate_TcshHookSetenvPersistsInCallingShell(t *testing.T) {
	requirePOSIXHarness(t)
	requireInterpreter(t, "tcsh")
	script, err := Generate("tcsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	writeEnvokeSetenvStub(t, stubDir, "ENVOKE_TEST_MARKER", "hit")

	startDir := t.TempDir()
	// Deliberately contains a space, regression-testing the eval-string
	// quoting that keeps $owd/$cwd as one argument each once eval
	// re-tokenizes the inner command string.
	targetDir := filepath.Join(t.TempDir(), "target with space")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("Mkdir targetDir: %v", err)
	}

	// `set prompt` is what makes this tcsh look interactive, which the hook
	// now requires before installing itself -- exactly as a real .tcshrc
	// would.
	driver := "set prompt = '% '\n" +
		"cd " + shellQuote(startDir) + "\n" +
		script + "\n" +
		"cd " + shellQuote(targetDir) + "\n" +
		`echo "MARKER=$ENVOKE_TEST_MARKER"` + "\n"

	rcPath := filepath.Join(stubDir, "driver.csh")
	if err := os.WriteFile(rcPath, []byte(driver), 0o644); err != nil {
		t.Fatalf("WriteFile driver: %v", err)
	}

	cmd := exec.Command("tcsh", "-f", rcPath)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MARKER=hit") {
		t.Errorf("expected setenv from the hooked block to persist in the calling shell, got:\n%s", out)
	}
}

// TestGenerate_ZshHookSetenvPersistsInCallingShell drives a real zsh through
// the generated chpwd_functions hook and asserts that a variable exported by
// the matched block is actually visible in the calling shell afterward —
// not just that envoke was invoked with the right args. Unlike bash (no
// baseline seeding needed: chpwd_functions fires on a real `cd` and
// $OLDPWD is already maintained by zsh itself) and unlike tcsh (no cwdcmd
// pipe/eval restrictions to work around), this is a plain behavioral check.
func TestGenerate_ZshHookSetenvPersistsInCallingShell(t *testing.T) {
	requirePOSIXHarness(t)
	requireInterpreter(t, "zsh")
	script, err := Generate("zsh")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	writeEnvokeSetenvStubZsh(t, stubDir, "ENVOKE_TEST_MARKER", "hit")

	startDir := t.TempDir()
	targetDir := t.TempDir()

	driver := "cd " + shellQuote(startDir) + "\n" +
		script + "\n" +
		"cd " + shellQuote(targetDir) + "\n" +
		`echo "MARKER=$ENVOKE_TEST_MARKER"` + "\n"

	cmd := exec.Command("zsh", "--no-rcs", "-i", "-c", driver)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MARKER=hit") {
		t.Errorf("expected setenv from the hooked block to persist in the calling shell, got:\n%s", out)
	}
}

// TestGenerate_FishHookSetenvPersistsInCallingShell drives a real fish
// through the generated --on-variable PWD hook and asserts a variable set
// (via `set -gx`) by the matched block persists in the calling shell. The
// driver is written to a temp .fish file and run as `fish <file>` (fish's
// own non-interactive script mode) rather than passed via `-c`, since a
// multi-statement `-c` string is more fragile to get right than a real
// script file — this also exercises `string collect`'s multi-line-output
// handling exactly as it runs when sourced from a real fish rc file.
func TestGenerate_FishHookSetenvPersistsInCallingShell(t *testing.T) {
	requirePOSIXHarness(t)
	requireInterpreter(t, "fish")
	script, err := Generate("fish")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	writeEnvokeSetenvStubFish(t, stubDir, "ENVOKE_TEST_MARKER", "hit")

	startDir := t.TempDir()
	targetDir := t.TempDir()

	driver := "cd " + shellQuote(startDir) + "\n" +
		script + "\n" +
		"cd " + shellQuote(targetDir) + "\n" +
		`echo "MARKER=$ENVOKE_TEST_MARKER"` + "\n"

	driverPath := filepath.Join(t.TempDir(), "driver.fish")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatalf("WriteFile driver: %v", err)
	}

	cmd := exec.Command("fish", "--no-config", "-i", driverPath)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MARKER=hit") {
		t.Errorf("expected setenv from the hooked block to persist in the calling shell, got:\n%s", out)
	}
}

// TestGenerate_PowershellHookSetenvPersistsInCallingShell drives a real
// pwsh through the generated prompt-wrapping hook and asserts a variable
// set (via `$env:NAME = ...`) by the matched block persists in the calling
// shell. Unlike bash/zsh/fish/tcsh, PowerShell's hook has no native
// "on cd" event at all — it piggybacks on the `prompt` function, which an
// interactive shell calls before every prompt redraw. There's no REPL here,
// so the driver calls `prompt` explicitly right after Set-Location to
// simulate that redraw. Its return value — the prompt string the host would
// have drawn — is discarded with `$null = prompt` so it cannot be mistaken
// for the marker this greps for, and the marker is written afterwards by a
// separate, unpiped Write-Output: whatever swallows the redraw's output must
// not be able to swallow the assertion's.
func TestGenerate_PowershellHookSetenvPersistsInCallingShell(t *testing.T) {
	requireInterpreter(t, "pwsh")
	script, err := Generate("powershell")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	writeEnvokeSetenvStubPowershell(t, stubDir, "ENVOKE_TEST_MARKER", "hit")

	startDir := t.TempDir()
	targetDir := t.TempDir()

	driver := "Set-Location " + psQuote(startDir) + "\n" +
		script + "\n" +
		"Set-Location " + psQuote(targetDir) + "\n" +
		"$null = prompt\n" +
		`Write-Output "MARKER=$env:ENVOKE_TEST_MARKER"` + "\n"

	cmd := exec.Command("pwsh", "-NoProfile", "-Command", driver)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "MARKER=hit") {
		t.Errorf("expected setenv from the hooked block to persist in the calling shell, got:\n%s", out)
	}
}

// psThenPrompt runs command and then calls the wrapped prompt, the way an
// interactive host redraws after every command. PowerShell has no cd event, so
// calling `prompt` explicitly is the only way to fire the hook in a driver with
// no REPL. The return value is discarded with `$null =` rather than a pipe (see
// TestGenerate_PowershellHookSetenvPersistsInCallingShell).
func psThenPrompt(command string) string {
	return command + "\n$null = prompt\n"
}

// TestGenerate_PowershellHookIgnoresNonFilesystemProviders asserts the hook
// stays silent for a location that isn't a filesystem path. PowerShell drives
// are provider-backed, so `Set-Location Env:` (or HKLM:, Cert:, Function:) is
// an ordinary thing to do and makes the current location something like
// `Env:\`, which is not a filesystem path at all — an ungated hook hands it to
// `envoke shell-hook`, which rejects it on stderr from inside `prompt`, once
// on the way in and once on the way out.
//
// Which drive stands in for "not the filesystem" is providerDrive's call:
// HKLM: on Windows, where this actually bites, and Env: elsewhere, since that
// is the one a Linux pwsh has. The provider gate itself is not
// platform-specific.
//
// The three cases are one property each: an ordinary cd still fires (a gate
// that never opens would pass the other two), the excursion itself fires
// nothing, and returning to the filesystem reports the transition from the
// last *filesystem* directory — which is what proves the previous-directory
// state survived the excursion instead of being overwritten with `HKLM:\`.
func TestGenerate_PowershellHookIgnoresNonFilesystemProviders(t *testing.T) {
	requireInterpreter(t, "pwsh")
	script, err := Generate("powershell")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	drive := providerDrive()
	for _, tc := range []struct {
		name string
		// steps is the driver text following hook installation, which happens
		// with the shell sitting in the start directory.
		steps func(target string) string
		// wantTransition says the stub must have been called exactly once,
		// reporting start -> target; false means never called at all.
		wantTransition bool
	}{
		{
			name:           "filesystem cd",
			steps:          func(target string) string { return psThenPrompt("Set-Location -LiteralPath " + psQuote(target)) },
			wantTransition: true,
		},
		{
			name:           "into a provider drive",
			steps:          func(string) string { return psThenPrompt("Set-Location " + drive) },
			wantTransition: false,
		},
		{
			// Push/Pop-Location is how one visits a provider drive and comes
			// back, and it keeps the return step off the provider: how
			// PowerShell resolves a `/`-rooted path while a non-filesystem
			// drive is current is PowerShell's business, not this hook's, and
			// the property under test is only that the excursion left
			// $_envokePrevPwd alone — a hook that stored `HKLM:\` would report
			// that as `from` here instead of the start directory.
			name: "back out of a provider drive",
			steps: func(target string) string {
				return psThenPrompt("Push-Location "+drive) + "Pop-Location\n" +
					psThenPrompt("Set-Location -LiteralPath "+psQuote(target))
			},
			wantTransition: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubDir := t.TempDir()
			logPath := filepath.Join(stubDir, "calls.log")
			writeEnvokeStub(t, stubDir, logPath)

			start, target := hookDir(t), hookDir(t)
			driver := "Set-Location -LiteralPath " + psQuote(start) + "\n" +
				script + "\n" + tc.steps(target)

			out, err := runDriver(t, stubDir, exec.Command("pwsh", "-NoProfile", "-Command", driver))
			if err != nil {
				t.Fatalf("driver script failed: %v\n%s", err, out)
			}

			got, readErr := os.ReadFile(logPath)
			if !tc.wantTransition {
				if readErr == nil {
					t.Errorf("hook fired for a non-filesystem location, reporting %q\noutput:\n%s", got, out)
				}
				return
			}
			if readErr != nil {
				t.Fatalf("envoke stub was never called (log file missing): %v\noutput:\n%s", readErr, out)
			}
			if want := start + " " + target + "\n"; string(got) != want {
				t.Errorf("transition reported as %q, want %q\noutput:\n%s", got, want, out)
			}
		})
	}
}

// TestGenerate_PowershellHookReportsThePathBehindAPSDrive asserts the hook
// reports the resolved filesystem path, not the drive-qualified spelling
// PowerShell displays. `New-PSDrive -PSProvider FileSystem` is how people give
// a long path a short name, and inside such a drive the location is
// `envoketest:/sub` while the real directory is elsewhere: the provider gate
// opens (it is the FileSystem provider), so it is the accessor that has to be
// right. A drive name of more than one letter is not an absolute path and
// `shell-hook` rejects it on stderr; a one-letter name is worse, being
// absolute and matching a config's patterns against a directory that does not
// exist. .ProviderPath is the same string for an ordinary location and the
// resolved one here, which is why the assertion is on the path itself rather
// than on the stub merely having run.
//
// The prompt is called once, after both moves: `Set-Location envoketest:` sits
// at the drive root, and whether PowerShell spells a root with a trailing
// separator is its business, not something this test should encode.
func TestGenerate_PowershellHookReportsThePathBehindAPSDrive(t *testing.T) {
	requireInterpreter(t, "pwsh")
	script, err := Generate("powershell")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "calls.log")
	writeEnvokeStub(t, stubDir, logPath)

	start := hookDir(t)
	root := hookDir(t)
	target := filepath.Join(root, "sub")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir target: %v", err)
	}

	driver := "Set-Location -LiteralPath " + psQuote(start) + "\n" +
		script + "\n" +
		"$null = New-PSDrive -Name envoketest -PSProvider FileSystem -Root " + psQuote(root) + "\n" +
		"Set-Location envoketest:\n" +
		psThenPrompt("Set-Location sub")

	out, err := runDriver(t, stubDir, exec.Command("pwsh", "-NoProfile", "-Command", driver))
	if err != nil {
		t.Fatalf("driver script failed: %v\n%s", err, out)
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("envoke stub was never called (log file missing): %v\noutput:\n%s", err, out)
	}
	if want := start + " " + target + "\n"; string(got) != want {
		t.Errorf("hook reported %q, want %q — the drive-qualified spelling (envoketest:/sub) is not a path envoke can match or even accept\noutput:\n%s", got, want, out)
	}
}

func writeEnvokeSetenvStub(t *testing.T, dir, name, value string) {
	t.Helper()
	stub := "#!/bin/sh\n" +
		`if [ "$1" = "shell-hook" ]; then echo "setenv ` + name + " " + value + `"; fi` + "\n"
	path := filepath.Join(dir, "envoke")
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("WriteFile stub: %v", err)
	}
}

// stubSpec is what the stand-in `envoke` on PATH does when a hook invokes it.
// Exactly one field is set by each of the three writers below; installStub
// turns it into whatever the platform running the test can execute — a
// `#!/bin/sh` script, or a native binary on Windows, which has no shebang
// handling. The fields are exported because the Windows stub reads them back
// out of a JSON file.
type stubSpec struct {
	// LogPath, when set, is a file the stub appends "<from> <to>\n" to for
	// every `shell-hook` invocation.
	LogPath string
	// Emit, when set, is one line the stub prints verbatim on stdout for
	// every `shell-hook` invocation.
	Emit string
	// ExitCode, when non-zero, is the status the stub exits with having done
	// and printed nothing at all.
	ExitCode int
}

// writeEnvokeStubEmitting writes an `envoke` stub whose `shell-hook`
// subcommand prints exactly line (verbatim, one line) to stdout.
func writeEnvokeStubEmitting(t *testing.T, dir, line string) {
	t.Helper()
	installStub(t, dir, stubSpec{Emit: line})
}

// writeEnvokeSetenvStubZsh emits POSIX `export NAME=VALUE` syntax — the zsh
// hook invokes `command envoke shell-hook` with no `--shell` flag, so
// executor.Render falls back to its POSIX profile (see internal/executor's
// profileFor: "" and unrecognized names both default to posixProfile).
func writeEnvokeSetenvStubZsh(t *testing.T, dir, name, value string) {
	t.Helper()
	writeEnvokeStubEmitting(t, dir, "export "+name+"="+value)
}

// writeEnvokeSetenvStubFish emits fish's `set -gx NAME VALUE` syntax, the
// fish profile's export spelling (internal/executor's fishExport).
func writeEnvokeSetenvStubFish(t *testing.T, dir, name, value string) {
	t.Helper()
	writeEnvokeStubEmitting(t, dir, "set -gx "+name+" "+value)
}

// writeEnvokeSetenvStubPowershell emits PowerShell's `$env:NAME = 'VALUE'`
// syntax, the powershell profile's export spelling
// (internal/executor's powershellExport).
func writeEnvokeSetenvStubPowershell(t *testing.T, dir, name, value string) {
	t.Helper()
	writeEnvokeStubEmitting(t, dir, "$env:"+name+" = '"+value+"'")
}

// psQuote quotes s as a PowerShell single-quoted literal (a literal `'` is
// written as `”`), matching internal/executor's powershellQuote.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// writeEnvokeStub writes an `envoke` stub whose `shell-hook` subcommand
// appends the from/to directories it was given to logPath. Both
// implementations of installStub mirror the real cmdShellHook's argument
// handling, including the ENVOKE_FROM/ENVOKE_TO environment fallback the tcsh
// hook relies on (see internal/shellinit's tcshHook comment) — so a hook that
// stops passing the directories at all fails the assertion rather than
// silently logging an empty line.
func writeEnvokeStub(t *testing.T, dir, logPath string) {
	t.Helper()
	installStub(t, dir, stubSpec{LogPath: logPath})
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fishQuote quotes s as a fish single-quoted literal (only `\` and `'` are
// special inside fish single quotes), matching internal/executor's
// fishQuote.
func fishQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// tcshQuote quotes s as a csh single-quoted literal. csh performs history
// expansion even inside single quotes, so `!` needs a leading backslash on
// top of POSIX's close/escape/reopen trick for `'` — matching
// internal/executor's tcshQuote.
func tcshQuote(s string) string {
	return strings.ReplaceAll(shellQuote(s), "!", `\!`)
}

// hookShell describes how to drive one shell's generated hook end to end:
// install the hook while sitting in start, change to target, and force the
// hook to fire, returning the interpreter's combined output. It exists so a
// cross-shell property (see TestGenerate_HooksNeverExecuteDirectoryNames)
// can be asserted once for all five shells instead of five times by hand.
type hookShell struct {
	// shell is the name passed to Generate.
	shell string
	// interpreter is the binary the test needs on PATH.
	interpreter string
	run         func(t *testing.T, script, start, target, stubDir string) (string, error)
}

func hookShells() []hookShell {
	return []hookShell{
		{shell: "bash", interpreter: "bash", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			// bash has no cd event: PROMPT_COMMAND is what the interactive
			// shell would run before redrawing, so the driver runs it
			// explicitly.
			driver := "cd " + shellQuote(start) + "\n" + script + "\n" +
				"cd " + shellQuote(target) + "\n" + `eval "$PROMPT_COMMAND"` + "\n"
			return runDriver(t, stubDir, exec.Command("bash", "--noprofile", "--norc", "-i", "-c", driver))
		}},
		{shell: "zsh", interpreter: "zsh", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			driver := "cd " + shellQuote(start) + "\n" + script + "\n" + "cd " + shellQuote(target) + "\n"
			return runDriver(t, stubDir, exec.Command("zsh", "--no-rcs", "-i", "-c", driver))
		}},
		{shell: "fish", interpreter: "fish", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			driver := "cd " + fishQuote(start) + "\n" + script + "\n" + "cd " + fishQuote(target) + "\n"
			return runDriver(t, stubDir, exec.Command("fish", "--no-config", "-i", writeDriver(t, "driver.fish", driver)))
		}},
		{shell: "tcsh", interpreter: "tcsh", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			driver := "set prompt = '% '\n" + "cd " + tcshQuote(start) + "\n" + script + "\n" + "cd " + tcshQuote(target) + "\n"
			return runDriver(t, stubDir, exec.Command("tcsh", "-f", writeDriver(t, "driver.csh", driver)))
		}},
		{shell: "powershell", interpreter: "pwsh", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			// Same as bash: no cd event, so call the wrapped prompt directly.
			driver := "Set-Location -LiteralPath " + psQuote(start) + "\n" + script + "\n" +
				"Set-Location -LiteralPath " + psQuote(target) + "\n" + "$null = prompt\n"
			return runDriver(t, stubDir, exec.Command("pwsh", "-NoProfile", "-Command", driver))
		}},
	}
}

// writeFailingEnvokeStub writes an `envoke` stub that prints nothing and
// exits non-zero, so a hook that leaks its own result into the shell's
// last-status is caught rather than accidentally passing because the stub
// happened to succeed.
func writeFailingEnvokeStub(t *testing.T, dir string) {
	t.Helper()
	installStub(t, dir, stubSpec{ExitCode: 3})
}

// TestGenerate_HooksAreTransparentToLastCommandStatus asserts that
// installing a hook never changes what the shell reports as the previous
// command's exit status.
//
// This is a regression test for a bug that shipped: the bash hook prepends
// itself to PROMPT_COMMAND, and bash sets $? for PROMPT_COMMAND to the last
// command's status — so without saving and restoring it, every prompt using
// the ubiquitous `PROMPT_COMMAND='__status=$?; ...'` idiom silently started
// reporting envoke's status instead of the user's last command. The same
// class of leak exists in each of the other hooks, in its own dialect:
// zsh/fish run their hook as part of the `cd` itself (so a failing hook
// would break `cd foo && ...`), and PowerShell's hook invokes a native
// command inside `prompt`, overwriting $LASTEXITCODE.
//
// Each case deliberately uses a stub that exits non-zero, so the assertion
// fails if the hook's own result leaks through.
func TestGenerate_HooksAreTransparentToLastCommandStatus(t *testing.T) {
	cases := []struct {
		shell       string
		interpreter string
		want        string
		// driver builds the shell text: install the hook, produce a known
		// status, fire the hook, then echo "STATUS=<observed>".
		run func(t *testing.T, script, start, target, stubDir string) (string, error)
	}{
		{
			shell: "bash", interpreter: "bash", want: "STATUS=42",
			run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
				// A pre-existing PROMPT_COMMAND that captures $? the way real
				// prompt frameworks do; the hook prepends itself to it.
				driver := "cd " + shellQuote(start) + "\n" +
					`PROMPT_COMMAND='__envoke_seen=$?; echo "STATUS=$__envoke_seen"'` + "\n" +
					script + "\n" +
					"cd " + shellQuote(target) + "\n" +
					"(exit 42)\n" +
					`eval "$PROMPT_COMMAND"` + "\n"
				return runDriver(t, stubDir, exec.Command("bash", "--noprofile", "--norc", "-i", "-c", driver))
			},
		},
		{
			shell: "zsh", interpreter: "zsh", want: "STATUS=0",
			run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
				// chpwd_functions run inside `cd`, so a leaking hook makes a
				// perfectly good `cd` report failure.
				driver := "cd " + shellQuote(start) + "\n" + script + "\n" +
					"cd " + shellQuote(target) + "\n" + `echo "STATUS=$?"` + "\n"
				return runDriver(t, stubDir, exec.Command("zsh", "--no-rcs", "-i", "-c", driver))
			},
		},
		{
			shell: "fish", interpreter: "fish", want: "STATUS=0",
			run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
				driver := "cd " + fishQuote(start) + "\n" + script + "\n" +
					"cd " + fishQuote(target) + "\n" + `echo "STATUS=$status"` + "\n"
				return runDriver(t, stubDir, exec.Command("fish", "--no-config", "-i", writeDriver(t, "driver.fish", driver)))
			},
		},
		{
			shell: "tcsh", interpreter: "tcsh", want: "STATUS=0",
			run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
				driver := "set prompt = '% '\n" + "cd " + tcshQuote(start) + "\n" + script + "\n" +
					"cd " + tcshQuote(target) + "\n" + `echo "STATUS=$status"` + "\n"
				return runDriver(t, stubDir, exec.Command("tcsh", "-f", writeDriver(t, "driver.csh", driver)))
			},
		},
		{
			shell: "powershell", interpreter: "pwsh", want: "STATUS=42",
			run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
				// $LASTEXITCODE is what a PowerShell prompt reads for a
				// native command's status; the hook runs one of its own.
				driver := "Set-Location -LiteralPath " + psQuote(start) + "\n" + script + "\n" +
					"Set-Location -LiteralPath " + psQuote(target) + "\n" +
					"$global:LASTEXITCODE = 42\n" +
					"$null = prompt\n" +
					`Write-Output "STATUS=$global:LASTEXITCODE"` + "\n"
				return runDriver(t, stubDir, exec.Command("pwsh", "-NoProfile", "-Command", driver))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			requireHarness(t, tc.shell)
			requireInterpreter(t, tc.interpreter)
			script, err := Generate(tc.shell)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}

			stubDir := t.TempDir()
			writeFailingEnvokeStub(t, stubDir)

			out, _ := tc.run(t, script, t.TempDir(), t.TempDir(), stubDir)
			if !strings.Contains(out, tc.want) {
				t.Errorf("hook leaked its own exit status: want %q in output, got:\n%s", tc.want, out)
			}
		})
	}
}

func writeDriver(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile driver: %v", err)
	}
	return path
}

func runDriver(t *testing.T, stubDir string, cmd *exec.Cmd) (string, error) {
	t.Helper()
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// nastyDirName is a single path component packed with every metacharacter
// that matters to at least one of the five supported shells: a single quote
// (closes a quoted string), `;`/`|`/`&` (command separators), `$( )` and a
// backtick (command substitution), a double quote and a space. Each
// injection attempt inside it would create a sentinel file in the shell's
// current directory — which, by the time the hook fires, is this very
// directory.
const nastyDirName = "nasty';touch pwn-sq;echo '" + "\"$(touch pwn-dollar)`touch pwn-backtick`" + " |touch pwn-pipe| &"

// pwnSentinels are the files nastyDirName's payloads would create if any
// hook let a directory name reach a shell parser as code.
var pwnSentinels = []string{"pwn-sq", "pwn-dollar", "pwn-backtick", "pwn-pipe"}

// TestGenerate_HooksNeverExecuteDirectoryNames is the regression test for a
// real, exploited command-injection bug: the tcsh hook used to interpolate
// $owd/$cwd into the string it passed to `eval`, so `cd` into a directory
// whose name contained a single quote closed the quoting and ran the rest of
// the name as shell code — with no config file and no `envoke allow`, which
// bypassed the trust model completely.
//
// The property asserted here is cross-shell on purpose: no hook, for any
// shell, may ever let a directory name be parsed as code. It's asserted by
// driving the real interpreter into a directory whose name would create
// sentinel files if it were executed, then checking none appeared — and
// that the hook still reported the transition correctly, so a hook that
// "passes" by not firing at all is caught too.
func TestGenerate_HooksNeverExecuteDirectoryNames(t *testing.T) {
	// Not requirePOSIXHarness: what stops this on Windows is nastyDirName
	// itself, PowerShell's driver included. `"` and `|` are illegal in a
	// filename there, so the directory cannot be created, and every payload
	// inside it invokes `touch`, which Windows has no equivalent of — so the
	// sentinels would fail to appear even if the name were executed. A Windows
	// version needs a different name and a different payload per shell; it is
	// not this test with a flag flipped.
	if runtime.GOOS == "windows" {
		t.Skip("nastyDirName cannot exist on Windows (`\"` and `|` are illegal in a filename) and its payloads call touch")
	}
	for _, hs := range hookShells() {
		t.Run(hs.shell, func(t *testing.T) {
			requireInterpreter(t, hs.interpreter)
			script, err := Generate(hs.shell)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}

			stubDir := t.TempDir()
			logPath := filepath.Join(stubDir, "calls.log")
			writeEnvokeStub(t, stubDir, logPath)

			start := t.TempDir()
			target := filepath.Join(t.TempDir(), nastyDirName)
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatalf("Mkdir target: %v", err)
			}

			out, err := hs.run(t, script, start, target, stubDir)
			if err != nil {
				t.Fatalf("driver script failed: %v\n%s", err, out)
			}

			for _, sentinel := range pwnSentinels {
				if _, statErr := os.Stat(filepath.Join(target, sentinel)); statErr == nil {
					t.Fatalf("directory name was executed as code: %s created\noutput:\n%s", sentinel, out)
				}
			}

			got, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("envoke stub was never called (log file missing): %v\noutput:\n%s", err, out)
			}
			if want := start + " " + target + "\n"; string(got) != want {
				t.Errorf("transition reported as %q, want %q\noutput:\n%s", got, want, out)
			}
		})
	}
}

func TestCompletion_UnsupportedShellsAreExplicitErrors(t *testing.T) {
	for _, shell := range []string{"tcsh", "powershell", "cmd", ""} {
		if _, err := Completion(shell); err == nil {
			t.Errorf("Completion(%q) should be an error, not a silently wrong script", shell)
		}
	}
}

// TestCompletion_ListsEverySubcommand checks the candidate list actually
// reaches every generated script. The list itself is cross-checked against
// the CLI's own usage output in cmd/envoke's tests, where the subcommands
// are actually defined.
func TestCompletion_ListsEverySubcommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			script, err := Completion(shell)
			if err != nil {
				t.Fatalf("Completion: %v", err)
			}
			for _, cmd := range subcommands {
				if !strings.Contains(script, cmd) {
					t.Errorf("%s completion never mentions subcommand %q", shell, cmd)
				}
			}
		})
	}
}

func TestCompletion_ScriptsAreSyntacticallyValid(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			requireInterpreter(t, shell)
			script, err := Completion(shell)
			if err != nil {
				t.Fatalf("Completion: %v", err)
			}
			assertShellSyntaxOK(t, shell, script)
		})
	}
}

// TestCompletion_BashActuallyCompletes drives a real bash: source the
// script, invoke the completion function the way bash would, and check the
// candidates. A syntax check alone would pass on a script that registers
// nothing useful.
func TestCompletion_BashActuallyCompletes(t *testing.T) {
	requireInterpreter(t, "bash")
	script, err := Completion("bash")
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}

	cases := []struct {
		words   string
		cword   string
		want    string
		notWant string
	}{
		// First word: subcommands, filtered by the prefix typed so far.
		{words: `(envoke re)`, cword: "1", want: "revoke", notWant: "allow"},
		{words: `(envoke "")`, cword: "1", want: "shell-init"},
		// Second word of shell-init: shell names, not files.
		{words: `(envoke shell-init f)`, cword: "2", want: "fish", notWant: "bash"},
		{words: `(envoke completion "")`, cword: "2", want: "zsh"},
	}

	for _, tc := range cases {
		t.Run(tc.words, func(t *testing.T) {
			driver := script + "\n" +
				"COMP_WORDS=" + tc.words + "\n" +
				"COMP_CWORD=" + tc.cword + "\n" +
				"_envoke_complete\n" +
				`printf '%s\n' "${COMPREPLY[@]}"` + "\n"

			// Non-interactive on purpose, unlike the hook drivers: the
			// completion script carries no interactivity guard, and bash -i
			// without a tty prints job-control warnings containing the word
			// "bash" — which is exactly what one of these cases asserts is
			// absent from the candidate list.
			out, err := exec.Command("bash", "--noprofile", "--norc", "-c", driver).CombinedOutput()
			if err != nil {
				t.Fatalf("driver failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("expected %q among the candidates, got:\n%s", tc.want, out)
			}
			if tc.notWant != "" && strings.Contains(string(out), tc.notWant) {
				t.Errorf("did not expect %q among the candidates, got:\n%s", tc.notWant, out)
			}
		})
	}
}

// rcContinuedSentinel is printed by whatever follows the hook text, so a hook
// that ends the shell evaluating it is distinguishable from one that merely
// installs nothing.
const rcContinuedSentinel = "ENVOKE-RC-CONTINUED"

// TestGenerate_HooksDoNotInstallInNonInteractiveShells is the regression test
// for the case that made this guard necessary: `.cshrc` is read by
// non-interactive tcsh and `.zshenv` by non-interactive zsh, so a hook
// installed in either fires for every `tcsh -c` or `zsh -c` that changes
// directory — running enter/leave blocks for a script that never asked for
// them. envoke is a tool for a person moving around; `envoke exec` is the
// deliberate non-interactive entry point.
//
// The drivers here are the same ones the behavioural tests use, minus the
// flag that makes the shell interactive. Nothing should reach the stub — and
// the driver must still run to its end, which is what rcContinuedSentinel
// checks: on its own, "the stub was never reached" is also true of a guard
// that aborts the shell installing it, so both assertions are needed to tell
// declining to install from taking the caller down.
//
// PowerShell is absent on purpose: its hook point is the `prompt` function,
// which only an interactive host ever calls, so there is nothing to guard and
// nothing to assert here (see powershellHook).
func TestGenerate_HooksDoNotInstallInNonInteractiveShells(t *testing.T) {
	requirePOSIXHarness(t)

	// echo is spelled the same in all four shells, so the sentinel line is
	// appended to every driver verbatim.
	sentinelEcho := "echo " + rcContinuedSentinel + "\n"

	for _, tc := range []struct {
		shell       string
		interpreter string
		run         func(t *testing.T, script, start, target, stubDir string) (string, error)
	}{
		{shell: "bash", interpreter: "bash", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			driver := "cd " + shellQuote(start) + "\n" + script + "\n" +
				"cd " + shellQuote(target) + "\n" + `eval "${PROMPT_COMMAND:-:}"` + "\n" + sentinelEcho
			return runDriver(t, stubDir, exec.Command("bash", "--noprofile", "--norc", "-c", driver))
		}},
		{shell: "zsh", interpreter: "zsh", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			driver := "cd " + shellQuote(start) + "\n" + script + "\n" + "cd " + shellQuote(target) + "\n" + sentinelEcho
			return runDriver(t, stubDir, exec.Command("zsh", "--no-rcs", "-c", driver))
		}},
		{shell: "fish", interpreter: "fish", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			driver := "cd " + fishQuote(start) + "\n" + script + "\n" + "cd " + fishQuote(target) + "\n" + sentinelEcho
			return runDriver(t, stubDir, exec.Command("fish", "--no-config", writeDriver(t, "driver.fish", driver)))
		}},
		{shell: "tcsh", interpreter: "tcsh", run: func(t *testing.T, script, start, target, stubDir string) (string, error) {
			// No `set prompt`: this is what a non-interactive tcsh looks like.
			driver := "cd " + tcshQuote(start) + "\n" + script + "\n" + "cd " + tcshQuote(target) + "\n" + sentinelEcho
			return runDriver(t, stubDir, exec.Command("tcsh", "-f", writeDriver(t, "driver.csh", driver)))
		}},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			requireInterpreter(t, tc.interpreter)
			script, err := Generate(tc.shell)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}

			stubDir := t.TempDir()
			logPath := filepath.Join(stubDir, "calls.log")
			writeEnvokeStub(t, stubDir, logPath)

			out, err := tc.run(t, script, t.TempDir(), t.TempDir(), stubDir)
			if err != nil {
				t.Fatalf("driver script failed: %v\n%s", err, out)
			}

			if !strings.Contains(out, rcContinuedSentinel) {
				t.Errorf("the hook aborted the non-interactive shell instead of declining to install: %q never printed, output:\n%s", rcContinuedSentinel, out)
			}

			if _, err := os.Stat(logPath); err == nil {
				got, _ := os.ReadFile(logPath)
				t.Errorf("the hook installed itself in a non-interactive shell and fired: %q", got)
			}
		})
	}
}

// TestGenerate_BashHookNeverAbortsTheRcFileThatSourcesIt installs the hook the
// way the docs tell users to — `eval "$(envoke shell-init bash)"` from a
// sourced rc file — and asserts the rc file's later lines still run.
//
// This is the sourced counterpart of the guard assertion above, and a
// different failure: `return` inside `eval` inside a sourced file pops the
// sourced file's own frame, so it succeeds and takes the rest of the user's
// `.bashrc` with it, silently and with status 0. Non-interactive bash does
// read `.bashrc` — `BASH_ENV`, and a shell started by a remote shell daemon —
// so both interactivity states are driven.
func TestGenerate_BashHookNeverAbortsTheRcFileThatSourcesIt(t *testing.T) {
	requirePOSIXHarness(t)
	requireInterpreter(t, "bash")
	script, err := Generate("bash")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// The hook text goes through `eval` from a *file* the shell sources, as in
	// real usage; `$(cat ...)` stands in for the real `envoke shell-init bash`.
	scriptPath := writeDriver(t, "shell-init.bash", script)
	rcPath := writeDriver(t, "bashrc", `eval "$(cat `+shellQuote(scriptPath)+`)"`+"\n"+
		"echo "+rcContinuedSentinel+"\n")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "non-interactive", args: []string{"--noprofile", "--norc", "-c", ". " + shellQuote(rcPath)}},
		{name: "interactive", args: []string{"--noprofile", "--norc", "-i", "-c", ". " + shellQuote(rcPath)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubDir := t.TempDir()
			writeEnvokeStub(t, stubDir, filepath.Join(stubDir, "calls.log"))

			out, err := runDriver(t, stubDir, exec.Command("bash", tc.args...))
			if err != nil {
				t.Fatalf("driver script failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, rcContinuedSentinel) {
				t.Errorf("sourcing the hook cut the rc file short: %q never printed, output:\n%s", rcContinuedSentinel, out)
			}
		})
	}
}

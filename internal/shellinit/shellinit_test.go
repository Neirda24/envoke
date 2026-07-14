package shellinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate_UnsupportedShellIsError(t *testing.T) {
	_, err := Generate("fish")
	if err == nil {
		t.Fatalf("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "fish") {
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

	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", driver)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+":"+os.Getenv("PATH"))
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

func writeEnvokeStub(t *testing.T, dir, logPath string) {
	t.Helper()
	stub := "#!/bin/sh\n" +
		`if [ "$1" = "shell-hook" ]; then echo "$2 $3" >> ` + shellQuote(logPath) + "; fi\n"
	path := filepath.Join(dir, "envoke")
	if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
		t.Fatalf("WriteFile stub: %v", err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

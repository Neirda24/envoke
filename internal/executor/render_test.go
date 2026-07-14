package executor

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/matcher"
)

func TestRender_NoMatchesIsEmpty(t *testing.T) {
	if got := Render("bash", nil, nil); got != "" {
		t.Errorf(`Render("bash", nil, nil) = %q, want empty`, got)
	}
}

func TestRender_OrdersLeavesBeforeEnters(t *testing.T) {
	leave := matcher.Match{Dir: "/a", Block: config.Block{Type: config.Leave, Pattern: regexp.MustCompile(`^/a$`), Script: "echo leave"}}
	enter := matcher.Match{Dir: "/b", Block: config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/b$`), Script: "echo enter"}}

	got := Render("bash", []matcher.Match{leave}, []matcher.Match{enter})
	leaveIdx := strings.Index(got, "echo leave")
	enterIdx := strings.Index(got, "echo enter")
	if leaveIdx == -1 || enterIdx == -1 || leaveIdx > enterIdx {
		t.Errorf("expected leave script before enter script, got:\n%s", got)
	}
}

func TestRender_ExportsMatchVarsBeforeScript(t *testing.T) {
	m := matcher.Match{
		Dir: "/Projects/foo",
		Block: config.Block{
			Type:    config.Enter,
			Pattern: regexp.MustCompile(`^/Projects/([^/]+)$`),
			Script:  "echo hi",
		},
	}
	got := Render("bash", nil, []matcher.Match{m})

	for _, want := range []string{
		"export ENVOKE_DIR='/Projects/foo'",
		"export ENVOKE_TYPE='enter'",
		"export ENVOKE_MATCH='/Projects/foo'",
		"export ENVOKE_MATCH_1='foo'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Index(got, "export ENVOKE_MATCH_1") > strings.Index(got, "echo hi") {
		t.Errorf("exports must come before the script, got:\n%s", got)
	}
}

func TestRender_UnrecognizedShellFallsBackToPosix(t *testing.T) {
	m := matcher.Match{Dir: "/a", Block: config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"}}
	for _, shell := range []string{"", "zsh", "ksh", "bogus"} {
		got := Render(shell, nil, []matcher.Match{m})
		if !strings.Contains(got, "export ENVOKE_DIR='/a'") {
			t.Errorf("shell %q: expected POSIX export syntax, got:\n%s", shell, got)
		}
	}
}

func TestRender_FishUsesSetGx(t *testing.T) {
	m := matcher.Match{Dir: "/a", Block: config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"}}
	got := Render("fish", nil, []matcher.Match{m})
	if !strings.Contains(got, "set -gx ENVOKE_DIR '/a'") {
		t.Errorf("expected fish `set -gx` syntax, got:\n%s", got)
	}
	if strings.Contains(got, "export ") {
		t.Errorf("fish output must not contain POSIX `export`, got:\n%s", got)
	}
}

func TestRender_TcshUsesSetenv(t *testing.T) {
	m := matcher.Match{Dir: "/a", Block: config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"}}
	got := Render("tcsh", nil, []matcher.Match{m})
	if !strings.Contains(got, "setenv ENVOKE_DIR '/a'") {
		t.Errorf("expected tcsh `setenv` syntax, got:\n%s", got)
	}
	if strings.Contains(got, "export ") {
		t.Errorf("tcsh output must not contain POSIX `export`, got:\n%s", got)
	}
}

func TestRender_PowershellUsesEnvDrive(t *testing.T) {
	m := matcher.Match{Dir: "/a", Block: config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"}}
	got := Render("powershell", nil, []matcher.Match{m})
	if !strings.Contains(got, "$env:ENVOKE_DIR = '/a'") {
		t.Errorf("expected powershell `$env:` syntax, got:\n%s", got)
	}
	if strings.Contains(got, "export ") {
		t.Errorf("powershell output must not contain POSIX `export`, got:\n%s", got)
	}
}

func TestRender_EndToEndThroughRealShell(t *testing.T) {
	// Exercises the actual payoff of the trust/eval architecture: a value
	// exported by a rendered block must be visible to shell code appended
	// after it, exactly as it would be in the caller's interactive shell.
	// The directory deliberately contains a space and a single quote to
	// regression-test posixQuote against real shell metacharacters.
	dir := `/has space/and'quote`
	m := matcher.Match{
		Dir: dir,
		Block: config.Block{
			Type:    config.Enter,
			Pattern: regexp.MustCompile(`^/has space/(.+)$`),
			Script:  `echo "$ENVOKE_DIR|$ENVOKE_TYPE|$ENVOKE_MATCH_1"`,
		},
	}

	script := Render("bash", nil, []matcher.Match{m})

	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c rendered script: %v\n%s", err, out)
	}
	want := dir + "|enter|and'quote\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestRender_TcshEndToEndThroughRealShell(t *testing.T) {
	if _, err := exec.LookPath("tcsh"); err != nil {
		t.Skip("tcsh not available on this system, skipping")
	}
	// Same regression as TestRender_EndToEndThroughRealShell, but for the
	// tcsh profile's setenv + posixQuote's close/escape/reopen quoting,
	// which tcsh happens to accept too.
	dir := `/has space/and'quote`
	m := matcher.Match{
		Dir: dir,
		Block: config.Block{
			Type:    config.Enter,
			Pattern: regexp.MustCompile(`^/has space/(.+)$`),
			Script:  `echo "$ENVOKE_DIR|$ENVOKE_TYPE|$ENVOKE_MATCH_1"`,
		},
	}

	script := Render("tcsh", nil, []matcher.Match{m})

	out, err := exec.Command("tcsh", "-f", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("tcsh -c rendered script: %v\n%s", err, out)
	}
	want := dir + "|enter|and'quote\n"
	if string(out) != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

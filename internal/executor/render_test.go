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
	leave := mustMatch(t, "/a", config.Block{Type: config.Leave, Pattern: regexp.MustCompile(`^/a$`), Script: "echo leave"})
	enter := mustMatch(t, "/b", config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/b$`), Script: "echo enter"})

	got := Render("bash", []matcher.Match{leave}, []matcher.Match{enter})
	leaveIdx := strings.Index(got, "echo leave")
	enterIdx := strings.Index(got, "echo enter")
	if leaveIdx == -1 || enterIdx == -1 || leaveIdx > enterIdx {
		t.Errorf("expected leave script before enter script, got:\n%s", got)
	}
}

func TestRender_ExportsMatchVarsBeforeScript(t *testing.T) {
	m := mustMatch(t, "/Projects/foo", config.Block{
		Type:    config.Enter,
		Pattern: regexp.MustCompile(`^/Projects/([^/]+)$`),
		Script:  "echo hi",
	})
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
	m := mustMatch(t, "/a", config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"})
	for _, shell := range []string{"", "zsh", "ksh", "bogus"} {
		got := Render(shell, nil, []matcher.Match{m})
		if !strings.Contains(got, "export ENVOKE_DIR='/a'") {
			t.Errorf("shell %q: expected POSIX export syntax, got:\n%s", shell, got)
		}
	}
}

func TestRender_FishUsesSetGx(t *testing.T) {
	m := mustMatch(t, "/a", config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"})
	got := Render("fish", nil, []matcher.Match{m})
	if !strings.Contains(got, "set -gx ENVOKE_DIR '/a'") {
		t.Errorf("expected fish `set -gx` syntax, got:\n%s", got)
	}
	if strings.Contains(got, "export ") {
		t.Errorf("fish output must not contain POSIX `export`, got:\n%s", got)
	}
}

func TestRender_TcshUsesSetenv(t *testing.T) {
	m := mustMatch(t, "/a", config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"})
	got := Render("tcsh", nil, []matcher.Match{m})
	if !strings.Contains(got, "setenv ENVOKE_DIR '/a'") {
		t.Errorf("expected tcsh `setenv` syntax, got:\n%s", got)
	}
	if strings.Contains(got, "export ") {
		t.Errorf("tcsh output must not contain POSIX `export`, got:\n%s", got)
	}
}

func TestRender_PowershellUsesEnvDrive(t *testing.T) {
	m := mustMatch(t, "/a", config.Block{Type: config.Enter, Pattern: regexp.MustCompile(`^/a$`), Script: "echo hi"})
	got := Render("powershell", nil, []matcher.Match{m})
	if !strings.Contains(got, "$env:ENVOKE_DIR = '/a'") {
		t.Errorf("expected powershell `$env:` syntax, got:\n%s", got)
	}
	if strings.Contains(got, "export ") {
		t.Errorf("powershell output must not contain POSIX `export`, got:\n%s", got)
	}
}

// nastyBasenames are directory basenames covering every metacharacter that
// is special to at least one supported shell. Render quotes a matched
// directory (and its capture groups) into shell source that the caller's
// own shell evaluates, so each of these has to survive that round trip
// byte-for-byte in every dialect.
//
// A literal newline is deliberately absent: csh cannot represent one inside
// a single-quoted string at all (not even escaped), so it is a documented
// unsupported case for tcsh rather than something these tests should claim
// works. See tcshQuote.
var nastyBasenames = []string{
	"plain",
	"has space",
	"and'quote",
	`double"quote`,
	"bang!bang",
	"dollar$var",
	"back`tick`",
	"semi;colon",
	"pipe|pipe",
	`back\slash`,
	"star*glob",
	"brace{a,b}",
	"paren(x)",
	"amp&amp",
	"hash#hash",
	"tilde~tilde",
	"percent%percent",
	"newline-free\ttab",
}

// renderShell is one shell profile plus how to actually execute Render's
// output in it, and how that dialect spells "echo three variables".
type renderShell struct {
	profile     string
	interpreter string
	echo        string
	command     func(script string) *exec.Cmd
}

func renderShells() []renderShell {
	const posixEcho = `echo "$ENVOKE_DIR|$ENVOKE_TYPE|$ENVOKE_MATCH_1"`
	return []renderShell{
		{profile: "bash", interpreter: "sh", echo: posixEcho, command: func(s string) *exec.Cmd {
			return exec.Command("sh", "-c", s)
		}},
		{profile: "fish", interpreter: "fish", echo: posixEcho, command: func(s string) *exec.Cmd {
			return exec.Command("fish", "--no-config", "-c", s)
		}},
		{profile: "tcsh", interpreter: "tcsh", echo: posixEcho, command: func(s string) *exec.Cmd {
			return exec.Command("tcsh", "-f", "-c", s)
		}},
		{profile: "powershell", interpreter: "pwsh",
			echo: `Write-Output "$env:ENVOKE_DIR|$env:ENVOKE_TYPE|$env:ENVOKE_MATCH_1"`,
			command: func(s string) *exec.Cmd {
				return exec.Command("pwsh", "-NoProfile", "-Command", s)
			}},
	}
}

// TestRender_QuotingRoundTripsThroughRealShells is the cross-dialect
// quoting contract: whatever a directory is called, the ENVOKE_* variables
// a block sees must hold exactly that name, in every shell.
//
// It regression-tests a real bug found by review: the tcsh profile reused
// posixQuote, but csh expands `!` at the lexer even inside single quotes,
// so a directory named `bang!bang` aborted the whole sourced block with
// "bang: Event not found." — no variables set, matched script never run.
// Asserting the property per dialect rather than per known-bad character is
// what makes this catch the next such quirk instead of only this one.
func TestRender_QuotingRoundTripsThroughRealShells(t *testing.T) {
	for _, rs := range renderShells() {
		t.Run(rs.profile, func(t *testing.T) {
			if _, err := exec.LookPath(rs.interpreter); err != nil {
				t.Skipf("%s not available on this system, skipping", rs.interpreter)
			}
			for _, base := range nastyBasenames {
				t.Run(base, func(t *testing.T) {
					const parent = "/has space"
					dir := parent + "/" + base
					m := mustMatch(t, dir, config.Block{
						Type:    config.Enter,
						Pattern: regexp.MustCompile("^" + regexp.QuoteMeta(parent) + "/(.+)$"),
						Script:  rs.echo,
					})

					script := Render(rs.profile, nil, []matcher.Match{m})
					out, err := rs.command(script).CombinedOutput()
					if err != nil {
						t.Fatalf("running rendered script: %v\nscript:\n%s\noutput:\n%s", err, script, out)
					}
					want := dir + "|enter|" + base + "\n"
					if strings.ReplaceAll(string(out), "\r\n", "\n") != want {
						t.Errorf("round trip = %q, want %q\nscript:\n%s", out, want, script)
					}
				})
			}
		})
	}
}

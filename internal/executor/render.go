package executor

import (
	"fmt"
	"strings"

	"github.com/Neirda24/envoke/internal/matcher"
)

// Render builds shell code that, when eval'd/sourced in the *calling*
// shell, runs every matched block in order (leaves, then enters — matching
// matcher.Resolve's ordering). This is what makes side effects like venv
// activation or exported env vars visible in the user's interactive shell:
// unlike Run, which execs a block in a subprocess, Render's output is meant
// to be eval'd/sourced directly by the shell hook, so scripts run in the
// parent shell's own process.
//
// shell selects the ENVOKE_* export syntax and value quoting for the
// calling shell — "bash", "zsh", "fish", "tcsh", and "powershell" all spell
// "export a variable" and "quote a string" differently, even though the
// matched block's own script text is emitted verbatim regardless: Render
// can't translate the user's script body, only the ENVOKE_* plumbing around
// it, so a script written in POSIX sh still needs to be POSIX sh (or at
// least portable enough) to behave when eval'd by fish/tcsh/powershell. An
// unrecognized shell name falls back to the POSIX profile (bash/zsh).
//
// Callers are responsible for checking trust before calling Render — it
// performs no trust check itself, matching CLAUDE.md's non-negotiable
// trust-before-execution principle.
func Render(shell string, leaves, enters []matcher.Match) string {
	p := profileFor(shell)
	var b strings.Builder
	for _, m := range leaves {
		renderMatch(&b, m, p)
	}
	for _, m := range enters {
		renderMatch(&b, m, p)
	}
	return b.String()
}

func renderMatch(b *strings.Builder, m matcher.Match, p shellProfile) {
	for _, v := range matchVars(m) {
		b.WriteString(p.export(v[0], v[1]))
		b.WriteString("\n")
	}
	b.WriteString(m.Block.Script)
	b.WriteString("\n")
}

// shellProfile is how a calling shell wants an ENVOKE_* variable assigned
// and exported into its environment.
type shellProfile struct {
	export func(name, value string) string
}

var posixProfile = shellProfile{export: posixExport}

// profiles holds every non-POSIX shell; profileFor falls back to
// posixProfile (bash/zsh's own syntax) for "bash", "zsh", "", and anything
// unrecognized.
var profiles = map[string]shellProfile{
	"fish":       {export: fishExport},
	"tcsh":       {export: tcshExport},
	"powershell": {export: powershellExport},
}

func profileFor(shell string) shellProfile {
	if p, ok := profiles[shell]; ok {
		return p
	}
	return posixProfile
}

func posixExport(name, value string) string {
	return fmt.Sprintf("export %s=%s", name, posixQuote(value))
}

func fishExport(name, value string) string {
	return fmt.Sprintf("set -gx %s %s", name, fishQuote(value))
}

// tcshExport uses setenv, csh's spelling of "export a variable" — tcsh has
// no `export`/`VAR=value` syntax at all, so eval'ing posixExport's output in
// tcsh is a syntax error on the very first line.
func tcshExport(name, value string) string {
	return fmt.Sprintf("setenv %s %s", name, posixQuote(value))
}

func powershellExport(name, value string) string {
	return fmt.Sprintf("$env:%s = %s", name, powershellQuote(value))
}

// posixQuote wraps s in single quotes so it's always emitted as a literal
// POSIX shell value, regardless of its contents — including embedded single
// quotes, spaces, or `$`/backtick metacharacters that would otherwise be
// interpreted by the shell evaluating Render's output. tcsh's csh-family
// quoting accepts the same close-quote/escape/reopen-quote trick for an
// embedded `'` (verified against a real tcsh), so tcshExport reuses this
// instead of duplicating it.
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// fishQuote wraps s in single quotes using fish's own escaping rule: inside
// single quotes, only `\` and `'` are special, each escaped by a leading
// backslash — unlike POSIX, which has no in-quote escape and instead
// concatenates adjacent quoted/escaped segments.
func fishQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// powershellQuote wraps s in single quotes using PowerShell's escaping
// rule: a literal single quote is written as two single quotes back to
// back; nothing else is special inside a single-quoted string.
func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

package executor

import (
	"fmt"
	"strings"

	"github.com/Neirda24/envoke/internal/matcher"
)

// Render builds shell code that, when eval'd/sourced in the *calling* shell,
// runs every matched block in order (leaves, then enters). Unlike Run, which
// execs a block in a subprocess, this output is meant to be evaluated by the
// user's own shell, so `export`/`source` inside a block affect that shell.
//
// Each block is wrapped: its ENVOKE_* variables are set before its script and
// unset after, so a block only ever sees its own values and nothing survives
// into the session. Without the trailing unset, ENVOKE_MATCH_2 set by one
// block would still be visible to the next block (whose own pattern may have
// fewer capture groups), and every variable would leak into every process
// started afterwards.
//
// shell selects the assignment and quoting dialect ("fish", "tcsh",
// "powershell"; anything else, including "" and "bash"/"zsh", uses POSIX).
// Only the ENVOKE_* plumbing is translated — a block's own script body is
// emitted verbatim and still has to be written in the calling shell's syntax.
//
// Callers must check trust before calling Render; it performs no check
// itself.
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
	vars := matchVars(m)
	names := make([]string, len(vars))
	for i, v := range vars {
		b.WriteString(p.export(v[0], v[1]))
		b.WriteString("\n")
		names[i] = v[0]
	}

	b.WriteString(m.Block.Script)
	b.WriteString("\n")

	// Emitted unconditionally rather than only on success: a failing script
	// must not leave its variables behind either.
	b.WriteString(p.unset(names))
}

// shellProfile is how a calling shell spells setting and clearing an
// exported variable.
type shellProfile struct {
	export func(name, value string) string
	unset  func(names []string) string
}

var posixProfile = shellProfile{export: posixExport, unset: posixUnset}

// profiles holds every non-POSIX shell; profileFor falls back to
// posixProfile for bash, zsh, "", and anything unrecognized.
var profiles = map[string]shellProfile{
	"fish":       {export: fishExport, unset: fishUnset},
	"tcsh":       {export: tcshExport, unset: tcshUnset},
	"powershell": {export: powershellExport, unset: powershellUnset},
}

func profileFor(shell string) shellProfile {
	if p, ok := profiles[shell]; ok {
		return p
	}
	return posixProfile
}

// posixShells are the names the POSIX profile legitimately serves. "" is one
// of them because bash's and zsh's generated hooks omit --shell entirely.
var posixShells = map[string]bool{"": true, "bash": true, "zsh": true}

// IsKnownShell reports whether shell is a dialect Render can actually speak.
// Render itself still falls back to POSIX for anything else, which is right
// for a library but wrong for a CLI flag: a typo would otherwise emit
// `export` into a fish or tcsh session, where it is a syntax error on every
// directory change.
func IsKnownShell(shell string) bool {
	return posixShells[shell] || profiles[shell].export != nil
}

func posixExport(name, value string) string {
	return fmt.Sprintf("export %s=%s", name, posixQuote(value))
}

func posixUnset(names []string) string {
	return "unset " + strings.Join(names, " ") + "\n"
}

func fishExport(name, value string) string {
	return fmt.Sprintf("set -gx %s %s", name, fishQuote(value))
}

func fishUnset(names []string) string {
	return "set -e " + strings.Join(names, " ") + "\n"
}

// tcshExport uses setenv: csh has no `export` or `VAR=value` syntax, so
// posixExport's output is a syntax error on its very first line there.
func tcshExport(name, value string) string {
	return fmt.Sprintf("setenv %s %s", name, tcshQuote(value))
}

// tcshUnset emits one unsetenv per name: csh's unsetenv takes a single
// pattern, not a list.
func tcshUnset(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString("unsetenv " + n + "\n")
	}
	return b.String()
}

func powershellExport(name, value string) string {
	return fmt.Sprintf("$env:%s = %s", name, powershellQuote(value))
}

// powershellUnset tolerates an already-absent variable, so a block whose
// script removed one itself doesn't turn the teardown into an error.
func powershellUnset(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString("Remove-Item -LiteralPath Env:" + n + " -ErrorAction SilentlyContinue\n")
	}
	return b.String()
}

// posixQuote wraps s in single quotes so it is always a literal value,
// whatever it contains.
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tcshQuote is posixQuote plus csh's extra hazard: history expansion happens
// at the lexer, before quote processing, so `!` is expanded even inside
// single quotes and has to be backslash-escaped. (A backslash is otherwise
// not an escape inside csh single quotes, so a value already containing one
// still round-trips.)
//
// A literal newline cannot be represented in a csh single-quoted string at
// all, escaped or not, so a directory name containing one is unsupported
// under tcsh. It fails loudly with "Unmatched '".
func tcshQuote(s string) string {
	return strings.ReplaceAll(posixQuote(s), "!", `\!`)
}

// fishQuote uses fish's own rule: inside single quotes only `\` and `'` are
// special, each escaped with a backslash. POSIX has no in-quote escape and
// concatenates segments instead, so posixQuote's output is wrong here.
func fishQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// powershellQuote uses PowerShell's rule: a literal single quote is doubled,
// nothing else is special inside single quotes.
func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

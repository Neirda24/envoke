package executor

import (
	"fmt"
	"strings"

	"github.com/Neirda24/envoke/internal/matcher"
)

// Render builds shell code that, when eval'd in the *calling* shell, runs
// every matched block in order (leaves, then enters — matching
// matcher.Resolve's ordering). This is what makes side effects like venv
// activation or exported env vars visible in the user's interactive shell:
// unlike Run, which execs a block in a subprocess, Render's output is meant
// to be `eval`'d directly by the shell hook, so scripts run in the parent
// shell's own process.
//
// Callers are responsible for checking trust before calling Render — it
// performs no trust check itself, matching CLAUDE.md's non-negotiable
// trust-before-execution principle.
func Render(leaves, enters []matcher.Match) string {
	var b strings.Builder
	for _, m := range leaves {
		renderMatch(&b, m)
	}
	for _, m := range enters {
		renderMatch(&b, m)
	}
	return b.String()
}

func renderMatch(b *strings.Builder, m matcher.Match) {
	for _, v := range matchVars(m) {
		fmt.Fprintf(b, "export %s=%s\n", v[0], shellQuote(v[1]))
	}
	b.WriteString(m.Block.Script)
	b.WriteString("\n")
}

// shellQuote wraps s in single quotes so it's always emitted as a literal
// POSIX shell value, regardless of its contents — including embedded single
// quotes, spaces, or `$`/backtick metacharacters that would otherwise be
// interpreted by the shell evaluating Render's output.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

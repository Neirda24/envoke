// Package executor runs a matched enter/leave block's script.
package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Neirda24/envoke/internal/matcher"
)

// killGrace is how long a script gets to exit on its own after the context
// is cancelled, before it is killed outright.
const killGrace = 5 * time.Second

// Run executes m's script through the shell, with the matched directory as
// the script's working directory and ENVOKE_* env vars set. Stdio is
// inherited from the caller so interactive scripts behave normally.
//
// Cancelling ctx interrupts the script rather than killing it, so a `trap`
// in the block gets to run; killGrace later escalates to a kill. Overriding
// Cancel is what makes that possible — CommandContext's default is an
// immediate kill, which no script can clean up after.
//
// Callers are responsible for having verified trust before getting here —
// internal/envoke.Transition is the only caller and does exactly that.
func Run(ctx context.Context, m matcher.Match) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Block.Script)
	cmd.Dir = m.Dir
	cmd.Env = append(os.Environ(), matchEnv(m)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = killGrace

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s (%s:%d): %w", m.Block.Type, m.Block.RawPattern, m.Dir, m.Block.Line, err)
	}
	return nil
}

// matchEnv builds the ENVOKE_* environment variables for Run's subprocess.
func matchEnv(m matcher.Match) []string {
	vars := matchVars(m)
	env := make([]string, len(vars))
	for i, v := range vars {
		env[i] = v[0] + "=" + v[1]
	}
	return env
}

// matchVars lists the ENVOKE_* variables exposed to a matched block's
// script: the directory that matched, the block type, and any regex capture
// groups from the pattern (ENVOKE_MATCH for the full match, ENVOKE_MATCH_1..
// for capture groups). Shared by Run (as subprocess env) and Render (as
// shell `export` statements) so the two execution paths can't drift apart.
//
// The groups come from matcher.NewMatch rather than being recomputed here:
// re-running the pattern would double the regex work on the hot path that
// every `cd` goes through, and would have to duplicate the
// slash-normalization rule to get the same answer on Windows.
func matchVars(m matcher.Match) [][2]string {
	vars := [][2]string{
		{"ENVOKE_DIR", m.Dir},
		{"ENVOKE_TYPE", m.Block.Type.String()},
	}

	if len(m.Groups) == 0 {
		return vars
	}
	vars = append(vars, [2]string{"ENVOKE_MATCH", m.Groups[0]})
	for i, g := range m.Groups[1:] {
		vars = append(vars, [2]string{fmt.Sprintf("ENVOKE_MATCH_%d", i+1), g})
	}
	return vars
}

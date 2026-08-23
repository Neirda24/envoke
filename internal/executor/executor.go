// Package executor runs a matched enter/leave block's script.
package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Neirda24/envoke/internal/matcher"
)

// killGrace is how long a script gets to exit on its own after the context
// is cancelled, before it is killed outright.
const killGrace = 5 * time.Second

// ErrNoShell reports that there is no POSIX shell on PATH to run a block
// with. Its own error because "sh: executable file not found" names a program
// the user never asked for, which is a bad first message on Windows — where
// there is no `sh` unless Git for Windows, MSYS2 or WSL put one there, and
// where the shell hook works fine.
var ErrNoShell = errors.New(`no POSIX shell ("sh") on PATH`)

// Run executes m's script through the shell, with the matched directory as
// the working directory and ENVOKE_* set. Stdio is inherited so interactive
// scripts behave normally.
//
// Cancelling ctx interrupts the script rather than killing it, so a `trap` in
// the block gets to run; killGrace escalates to a kill. Overriding Cancel is
// what makes that possible — CommandContext's default is an immediate kill.
//
// Trust must be verified before getting here; internal/envoke.Transition is
// the only caller and does.
func Run(ctx context.Context, m matcher.Match) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Block.Script)
	cmd.Dir = m.Dir
	cmd.Env = blockEnv(m)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = killGrace

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			err = ErrNoShell
		}
		return fmt.Errorf("%s %s (%s:%d): %w", m.Block.Type, m.Block.RawPattern, m.Dir, m.Block.Line, err)
	}
	return nil
}

// blockEnv is the environment Run's subprocess gets: the caller's, minus
// every variable a matched block is given, plus the ones this block has.
//
// The subtraction is the point. These variables are numbered per block, so a
// block that captured nothing must not see an ENVOKE_MATCH_2 inherited from
// the caller's environment — an `envoke exec` invoked from inside a block, or
// a shell where a script exported one. Render can only clear what it set
// itself; Run builds the environment outright, so here it is absolute.
func blockEnv(m matcher.Match) []string {
	vars := matchVars(m)

	env := make([]string, 0, len(vars)+len(os.Environ()))
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && isBlockVar(name) {
			continue
		}
		env = append(env, kv)
	}
	for _, v := range vars {
		env = append(env, v[0]+"="+v[1])
	}
	return env
}

// isBlockVar reports whether name is one of the variables envoke hands a
// matched block, and therefore one no block may inherit from outside. Must
// agree with matchVars; the numbered form is matched by shape because there
// is one per capture group and no bound on how many.
func isBlockVar(name string) bool {
	switch name {
	case "ENVOKE_DIR", "ENVOKE_TYPE", "ENVOKE_MATCH":
		return true
	}
	digits, ok := strings.CutPrefix(name, "ENVOKE_MATCH_")
	if !ok || digits == "" {
		return false
	}
	return strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// matchVars lists the ENVOKE_* variables exposed to a matched block's script.
// Shared by Run (as subprocess env) and Render (as shell `export` statements)
// so the two execution paths can't drift apart.
//
// The groups come from matcher.NewMatch rather than being recomputed:
// re-running the pattern would double the regex work on the hot path of every
// `cd`, and would have to duplicate the slash-normalization rule to get the
// same answer on Windows.
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

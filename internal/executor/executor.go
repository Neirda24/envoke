// Package executor runs a matched enter/leave block's script.
package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/Neirda24/envoke/internal/matcher"
)

// Run executes m's script through the shell, with the matched directory as
// the script's working directory and ENVOKE_* env vars set. Stdio is
// inherited from the caller so interactive scripts behave normally.
func Run(ctx context.Context, m matcher.Match) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Block.Script)
	cmd.Dir = m.Dir
	cmd.Env = append(os.Environ(), matchEnv(m)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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
func matchVars(m matcher.Match) [][2]string {
	vars := [][2]string{
		{"ENVOKE_DIR", m.Dir},
		{"ENVOKE_TYPE", m.Block.Type.String()},
	}

	groups := m.Block.Pattern.FindStringSubmatch(m.Dir)
	if len(groups) == 0 {
		return vars
	}
	vars = append(vars, [2]string{"ENVOKE_MATCH", groups[0]})
	for i, g := range groups[1:] {
		vars = append(vars, [2]string{fmt.Sprintf("ENVOKE_MATCH_%d", i+1), g})
	}
	return vars
}

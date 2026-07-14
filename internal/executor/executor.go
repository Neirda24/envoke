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

// matchEnv builds the ENVOKE_* environment variables exposed to a matched
// block's script: the directory that matched, the block type, and any regex
// capture groups from the pattern (ENVOKE_MATCH for the full match,
// ENVOKE_MATCH_1.. for capture groups).
func matchEnv(m matcher.Match) []string {
	env := []string{
		"ENVOKE_DIR=" + m.Dir,
		"ENVOKE_TYPE=" + m.Block.Type.String(),
	}

	groups := m.Block.Pattern.FindStringSubmatch(m.Dir)
	if len(groups) == 0 {
		return env
	}
	env = append(env, "ENVOKE_MATCH="+groups[0])
	for i, g := range groups[1:] {
		env = append(env, fmt.Sprintf("ENVOKE_MATCH_%d=%s", i+1, g))
	}
	return env
}

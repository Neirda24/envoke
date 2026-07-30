// Package envoke wires the config, trust, matcher and executor packages
// together into the core enter/leave loop used for non-interactive
// execution (`envoke exec`).
package envoke

import (
	"context"
	"errors"
	"fmt"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/executor"
	"github.com/Neirda24/envoke/internal/matcher"
	"github.com/Neirda24/envoke/internal/trust"
)

// ErrUntrusted reports that the config has not been approved with
// `envoke allow` since its last edit, so none of its blocks were run.
// Callers can test for it with errors.Is.
var ErrUntrusted = errors.New("config is not trusted")

// Transition runs every enter/leave block that fires for a directory change
// from -> to, in a subprocess per block: leave blocks first (deepest
// directory first), then enter blocks (shallowest directory first) — see
// matcher.Resolve for the ordering rules, including traverse behavior for
// intermediate directories.
//
// It takes a config *path* and does the loading and the trust check itself
// rather than accepting a parsed *config.Config. This is the only thing in
// the codebase that spawns a shell from config, so the trust gate lives
// here where no caller can skip it; an untrusted config returns
// ErrUntrusted having run nothing. One read feeds both the parse and the
// hash (see config.LoadFile).
//
// Execution stops at the first failing block, and a partially-applied
// transition is not unwound — enter and leave are independent.
func Transition(ctx context.Context, configPath, from, to string) error {
	cfg, content, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}

	trusted, err := trust.IsTrusted(configPath, content)
	if err != nil {
		return err
	}
	if !trusted {
		return fmt.Errorf("%s: %w", configPath, ErrUntrusted)
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		return fmt.Errorf("resolve %s -> %s: %w", from, to, err)
	}

	for _, m := range leaves {
		if err := executor.Run(ctx, m); err != nil {
			return err
		}
	}
	for _, m := range enters {
		if err := executor.Run(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

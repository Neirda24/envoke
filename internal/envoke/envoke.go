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
// It deliberately takes a config *path* and does the loading and the trust
// check itself, rather than accepting an already-parsed *config.Config.
// Accepting a parsed config would make the trust check the caller's
// responsibility, and CLAUDE.md's trust-before-execution principle is not
// something to leave to a caller's diligence: this function is the only
// thing in the codebase that spawns a shell from config, so the gate lives
// here where it cannot be skipped. An untrusted config returns ErrUntrusted
// having run nothing at all. The single read that feeds both the parse and
// the hash is the same read-once discipline cmd/envoke uses (see
// config.LoadFile).
//
// Execution stops at the first failing block; scripts after it (whether
// remaining leave blocks or any enter blocks) do not run. envoke does not
// snapshot or auto-unwind a partially-applied transition — enter and leave
// are independent, explicit blocks (see CLAUDE.md).
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

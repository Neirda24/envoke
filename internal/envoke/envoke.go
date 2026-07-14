// Package envoke wires the config, matcher and executor packages together
// into the core enter/leave loop.
package envoke

import (
	"context"
	"fmt"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/executor"
	"github.com/Neirda24/envoke/internal/matcher"
)

// Transition runs every enter/leave block that fires for a directory change
// from -> to: leave blocks first (deepest directory first), then enter
// blocks (shallowest directory first) — see matcher.Resolve for the
// ordering rules, including traverse behavior for intermediate directories.
//
// Execution stops at the first failing block; scripts after it (whether
// remaining leave blocks or any enter blocks) do not run. envoke does not
// snapshot or auto-unwind a partially-applied transition — enter and leave
// are independent, explicit blocks (see CLAUDE.md).
func Transition(ctx context.Context, cfg *config.Config, from, to string) error {
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

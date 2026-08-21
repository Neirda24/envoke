// Package envoke wires the config, trust, matcher and executor packages
// together into the core enter/leave loop used for non-interactive
// execution (`envoke exec`).
package envoke

import (
	"context"
	"errors"
	"fmt"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/configset"
	"github.com/Neirda24/envoke/internal/executor"
	"github.com/Neirda24/envoke/internal/matcher"
)

// ErrUntrusted reports that a config has not been approved with `envoke
// allow` since its last edit, so none of *its* blocks were run. Callers can
// test for it with errors.Is.
var ErrUntrusted = errors.New("config is not trusted")

// ErrNoConfig reports that there is no config at all — neither a central one
// nor any envokerc.d fragment. exec is called deliberately, usually from a
// script, so having nothing to run is a setup mistake worth failing on rather
// than a quiet success.
var ErrNoConfig = errors.New("no config found")

// Transition runs every enter/leave block that fires for a directory change
// from -> to, in a subprocess per block: leave blocks first (deepest
// directory first), then enter blocks (shallowest directory first) — see
// matcher.Resolve for the ordering rules, including traverse behavior for
// intermediate directories.
//
// The config set is passed in already loaded — cmd/envoke locates it, since
// that is where "no config at all" has to be reported — but every trust
// decision is made here: this is the only thing in the codebase that spawns a
// shell from config, so the gate lives where no caller can skip it. One read
// feeds both the parse and the hash (see config.LoadFile).
//
// A config that is untrusted or unreadable does not stop the others: with
// several fragments in play, one that a `git pull` just rewrote would
// otherwise disable the whole set. Every such config is reported in the
// returned error — joined, so errors.Is still finds ErrUntrusted — after the
// trusted blocks have run.
//
// Execution stops at the first failing block, and a partially-applied
// transition is not unwound — enter and leave are independent.
func Transition(ctx context.Context, entries []configset.Entry, from, to string) error {
	if len(entries) == 0 {
		return ErrNoConfig
	}

	leaves, enters, err := matcher.Resolve(configset.Configs(entries), from, to)
	if err != nil {
		return fmt.Errorf("resolve %s -> %s: %w", from, to, err)
	}

	runnable, problems, err := decide(entries, leaves, enters)
	if err != nil {
		return err
	}

	for _, matches := range [][]matcher.Match{leaves, enters} {
		for _, m := range matches {
			if !runnable[m.Config] {
				continue
			}
			if err := executor.Run(ctx, m); err != nil {
				return errors.Join(append(problems, err)...)
			}
		}
	}
	return errors.Join(problems...)
}

// decide resolves each matched config's trust state once, whatever number of
// blocks it contributed, and turns everything that isn't runnable into a
// reportable problem.
//
// Only configs that actually matched are consulted. A fragment with nothing
// to say about this transition is not something to warn about — it isn't
// being skipped, it simply doesn't apply.
func decide(entries []configset.Entry, matched ...[]matcher.Match) (map[*config.Config]bool, []error, error) {
	var problems []error
	for _, e := range entries {
		if e.Err != nil {
			problems = append(problems, e.Err)
		}
	}

	byConfig := configset.ByConfig(entries)
	runnable := make(map[*config.Config]bool, len(byConfig))
	seen := make(map[*config.Config]bool, len(byConfig))

	for _, matches := range matched {
		for _, m := range matches {
			if seen[m.Config] {
				continue
			}
			seen[m.Config] = true

			entry := byConfig[m.Config]
			decision, err := configset.Decide(entry)
			if err != nil {
				return nil, nil, err
			}
			if decision == configset.Run {
				runnable[m.Config] = true
			} else {
				problems = append(problems, fmt.Errorf("%s: %w", entry.Path, ErrUntrusted))
			}
		}
	}
	return runnable, problems, nil
}

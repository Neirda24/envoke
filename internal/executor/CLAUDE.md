# internal/executor

Two execution models sharing `matchVars`.

- **`Run`** — `sh -c` subprocess, for `internal/envoke.Transition`. Side
  effects stay in the subprocess, so this is not what the shell hook uses.
- **`Render`** — text for the caller's own shell to `eval`/`source`, for
  `shell-hook` and `reload`. The only path where a block's `export`/`source`
  reaches the user's interactive shell.

## Don't

- **Don't drop the per-block teardown** (`shellProfile.unset`). Capture groups
  are numbered per block, so without it a two-group block followed by a
  zero-group one leaves `ENVOKE_MATCH_2` visible to a script that captured
  nothing, and every exported var outlives the `cd`.
- **Don't make `tcshQuote` call through to `posixQuote` alone.** csh does
  history expansion at the lexer, before quote processing, so `!` expands
  inside single quotes: a directory named `foo!bar` aborted the whole sourced
  block with "Event not found", setting no variables and running no script. A
  literal newline is unrepresentable in a csh single-quoted string and is a
  documented unsupported case.
- **Don't let `Render`'s POSIX fallback reach a CLI flag.** Falling back is
  right for a library and wrong for `--shell fsh`, which would feed `export`
  to a fish session — hence `IsKnownShell`.

The two models do not guarantee the same thing, and the difference is
structural: `Run` builds the child's environment outright, `Render` can only
clear what it set itself.

## Tests

- `TestRender_QuotingRoundTripsThroughRealShells` round-trips hostile
  basenames through each real interpreter. **Extend that table** rather than
  writing a one-off test for the next quirk.
- The suite `t.Skip`s without an interpreter, so `.dagger`'s `test-shell-*`
  checks run it in a container with exactly one shell. A local `SKIP` means
  run it through Dagger, not that the case is covered.

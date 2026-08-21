# internal/executor

Two execution models sharing `matchVars` (the
`ENVOKE_DIR`/`ENVOKE_TYPE`/`ENVOKE_MATCH`/`ENVOKE_MATCH_N` a matched block
sees).

## `Run(ctx, match)` — subprocess

Execs the script via `sh -c` as a **subprocess** (`cmd.Dir` set to the matched
directory, inherited stdio). Used by `internal/envoke.Transition` for
non-interactive/one-off execution — side effects like `export`/`source` don't
escape this subprocess, so this is not what the shell hook uses.

The environment comes from `blockEnv`, which **strips every `ENVOKE_*` block
variable out of `os.Environ()`** before adding this block's: they are numbered
per block, so inheriting an `ENVOKE_MATCH_2` from the caller would show it to
a script that captured nothing.

Windows has no `sh` unless Git for Windows/MSYS2/WSL put one there, and
`exec: "sh": executable file not found` names a program the user never
mentioned — hence `ErrNoShell`, which `cmd/envoke` follows with what to do
instead.

## `Render(shell, leaves, enters)` — text for the caller's own shell

Builds ENVOKE_*-var-assignment-prefixed shell text for every match, meant to
be `eval`'d/`source`d by the *caller's own shell*. This is what
`cmd/envoke shell-hook` and `reload` use once a config is trusted — the only
path where `export`/`source` in a script actually affects the user's
interactive shell.

**Each block's vars are unset again right after its script**
(`shellProfile.unset`, one spelling per dialect): capture groups are numbered
per block, so without the teardown a two-group block followed by a zero-group
one left `ENVOKE_MATCH_2` visible to a script that never captured anything,
and every var — all exported — outlived the `cd` and was inherited by every
process started later. Don't drop it for a "cleaner" output.

The two paths do **not** guarantee the same thing here, and the difference is
structural: `Run` builds the child's environment outright (`blockEnv` strips
every `ENVOKE_DIR`/`TYPE`/`MATCH`/`MATCH_<n>` out of `os.Environ()` before
adding this block's), so a block provably sees only its own; `Render` writes
text for a shell it doesn't own and can only clear what it set itself, so a
value exported by the user's own script before the first block still reaches
it.

`shell` selects a `shellProfile` (posix, fish, tcsh, powershell — each with
its own quoting function to prevent injection via directory names or capture
groups); an unrecognized name falls back to POSIX, which is right for a
library and wrong for a CLI flag — hence `IsKnownShell`, which `cmd/envoke`
uses to reject `--shell fsh` before it feeds `export` to a fish session.

**`tcshQuote` is not `posixQuote`**: csh does history expansion at the lexer,
before quote processing, so `!` is expanded even inside single quotes and must
be backslash-escaped — a directory named `foo!bar` otherwise aborted the whole
sourced block with "bar: Event not found.", setting no variables and running no
script. A literal newline can't be represented in a csh single-quoted string
at all, and is a documented unsupported case for tcsh.

All four profiles are covered by
`TestRender_QuotingRoundTripsThroughRealShells`, which round-trips a table of
hostile basenames through each real interpreter — extend that table rather than
writing a one-off test for the next quirk.

Render can only translate the ENVOKE_* plumbing — a script body still has to
be written in the calling shell's own syntax.

## Testing

This package emits shell code and its suite `t.Skip`s without the interpreter,
so `.dagger`'s `test-shell-*` checks run it alongside `internal/shellinit`'s in
a container with exactly one shell installed. A local `SKIP` here means run it
through Dagger, not that the case is covered.

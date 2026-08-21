# internal/shellinit

`Generate(shell)` returns the literal hook script for `"bash"`, `"zsh"`,
`"fish"`, `"tcsh"`, or `"powershell"` (static strings, no templating — one
binary generates all shell integration, never hand-maintained script files).

fish, tcsh and powershell pass `--shell <name>` so `executor.Render` picks the
right dialect; bash and zsh omit it and take the POSIX default (`posixShells`
in `executor/render.go`), which is a documented behaviour rather than an
oversight. None of the five hooks redefine `cd` (`assertNeverRedefinesCd`).

## The interactive guard

**Four of the five refuse to install in a non-interactive shell**
(`case $- in *i*`, `[[ -o interactive ]]`, `status is-interactive`,
`$?prompt`) — `.cshrc` and `.zshenv` are read by every shell, so a hook
installed there otherwise fires for every `tcsh -c` or `zsh -c` that changes
directory. PowerShell has no guard on purpose: its hook point is the `prompt`
function, which only an interactive host calls.

The guard is why every driver in `shellinit_test.go` now makes its shell
interactive — `-i` for bash/zsh/fish, `set prompt` for tcsh, which is what
`$?prompt` actually tests. Two exceptions worth knowing: the *completion*
drivers stay non-interactive (no guard to satisfy, and `bash -i` without a tty
prints job-control warnings containing the word "bash", which one case asserts
is absent), and `TestGenerate_HooksDoNotInstallInNonInteractiveShells` is the
same drivers *without* the flag.

That test asserts **two** things, and one alone would pass a broken guard:
that nothing reached the stub, *and* that the driver ran to its end
(`rcContinuedSentinel`). "The stub was never reached" is equally true of a
guard that takes the shell installing it down with it, which is why the guards
wrap the installation rather than returning early — `return` inside `eval`
inside a sourced file pops the *sourced file's* frame, skipping the rest of the
user's rc file with status 0, and in an executed script `exit` ends it outright.
`TestGenerate_BashHookNeverAbortsTheRcFileThatSourcesIt` drives that specific
shape, `eval "$(envoke shell-init bash)"` from a sourced file, in both
interactivity states.

## Completion

`Completion(shell)` does the same for tab completion, for bash/zsh/fish only —
tcsh and PowerShell return an explicit error rather than a half-working
script.

**No generated script reads `subcommands`.** Each of the three hardcodes its
own list, in its own syntax: bash's `-W "..."` word list, zsh's `_envoke_cmds`
array of `name:description`, and fish's one `complete ... -a <name>` line per
command. `subcommands` is the checklist that keeps those three honest —
`TestCompletion_ListsEverySubcommand` asserts every name in it appears in all
three scripts. The other half of the net is `cmd/envoke`'s
`TestRun_CompletionCoversEverySubcommand`, which compares `envoke help` against
the generated **bash** script only, so a name missing from zsh's or fish's list
is caught by the first test and not the second. Adding a name to `subcommands`
alone completes the command in no shell.

**The generated bash scripts must work on bash 3.2**, which is still
`/bin/bash` on every Mac. `mapfile` is 4.0+ and silently produces nothing
there, so the completion uses a `while IFS= read -r` loop instead. The macOS
CI runner is what actually exercises this.

## Two cross-shell invariants

Each has a test that drives real interpreters:

- A hook must never let a **directory name** reach a shell parser as code
  (`TestGenerate_HooksNeverExecuteDirectoryNames` — this is what the tcsh
  `eval` rework below is about).
- A hook must be **transparent to the shell's last-command status**
  (`TestGenerate_HooksAreTransparentToLastCommandStatus`): bash/zsh/fish save
  `$?`/`$status` on entry and `return` it, PowerShell saves and restores
  `$LASTEXITCODE`. Skipping that turns every exit-code-aware prompt into a
  liar.

## Per-shell gotchas

- **bash** has no native "on cd" hook, so it polls via `PROMPT_COMMAND`
  comparing `$PWD` against a var — that var **must be seeded at hook-install
  time**, or the first `cd` after install compares the new directory against
  itself and is silently missed.
- **zsh** uses the native `chpwd_functions` array; `$OLDPWD` is already set by
  the shell, no seeding needed.
- **fish** uses `--on-variable PWD` and seeds its own baseline var (like
  bash — fish's `$OLDPWD` isn't reliable enough to depend on). A bare command
  substitution splits multi-line output into one list element per line, so
  `string collect` (fish 3.4+) joins `envoke shell-hook`'s stdout into one
  string before `eval`.
- **tcsh** claims the `cwdcmd` alias outright (not chained with a pre-existing
  user `cwdcmd`, e.g. xterm title-setting — documented limitation). tcsh has no
  `export`/`VAR=value` syntax and both plain and quoted backquote substitution
  split on newlines, so the hook pipes `envoke shell-hook`'s stdout straight
  into `source /dev/stdin`. `cwdcmd`'s body runs through a restricted execution
  path that doesn't honor `|`/`>` directly, so the whole thing is wrapped in
  `eval "..."` to force a full re-parse — and **that eval string is a
  compile-time constant with nothing user-controlled in it**: the directories
  travel through `$ENVOKE_FROM`/`$ENVOKE_TO`, `setenv`'d outside the eval, which
  csh does not re-tokenize inside double quotes. Interpolating `$owd`/`$cwd`
  into the eval string is the exploited injection this replaced — one single
  quote in a directory name closed the quoting and ran the rest as code, with no
  config file and no `envoke allow`. Don't put a directory name back in there
  under any quoting. tcsh also has no `command` builtin (unlike bash/zsh/fish) —
  use `\envoke` to bypass a same-named user alias, not `command envoke`.
- **powershell** wraps the `prompt` function (saving and always calling through
  to any previous definition, guarded against double-wrapping), joining output
  via `Out-String` before `Invoke-Expression` — same multi-line concern as
  fish's `string collect`. It fires only where `(Get-Location).Provider.Name`
  is `FileSystem`, because a PowerShell location need not be a path at all
  (`HKLM:`, `Cert:`, `Env:`) and `shell-hook` would reject it on stderr from
  inside `prompt`, twice per round trip. What it then sends is
  **`.ProviderPath`, not `.Path`**: under the FileSystem provider `.Path` is
  still PowerShell's spelling — drive-qualified for a user-created PSDrive,
  provider-qualified for a UNC location — and a one-letter PSDrive name is
  worse than an error, being absolute and pointing somewhere that doesn't
  exist. Two separate decisions: the provider gates *whether*, `ProviderPath`
  decides *what*.

## Testing

Generated shell code is tested against real interpreters, not just
string-matched: syntax checks (`<shell> -n -c`) plus behavioral tests that
drive a real subprocess with a stub `envoke` binary on `PATH` and assert the
side effect (`setenv`/`export`/`set`) actually persists in the calling shell.
`t.Skip`s locally if an interpreter isn't installed; `.dagger`'s
`test-shell-*` checks run all five for real in CI. A local `SKIP` is a signal
to run it through Dagger, not something to wave off.

The stub is installed by `installStub`, which is build-tagged: a `#!/bin/sh`
script off Windows, and on Windows a link (or copy) of the **running test
binary** named `envoke.exe`, re-entered through a windows-only `TestMain`. That
is not over-engineering — a `.cmd` shim gets its argv re-parsed by cmd.exe and
its `echo` writes CRLF, which the log assertions compare against `"\n"`; and a
`.ps1` or a function named `envoke` is not a *native* command, so PowerShell
never touches `$LASTEXITCODE` and the transparency test would pass while
asserting nothing. `requirePOSIXHarness` means "needs a POSIX interpreter";
`requireHarness(t, shell)` is what a cross-shell table calls, so the four POSIX
shells skip per case instead of taking PowerShell down with them on Windows.

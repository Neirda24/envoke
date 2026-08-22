# internal/shellinit

`Generate(shell)` and `Completion(shell)` — static strings per shell, no
templating. One binary generates all shell integration; never hand-maintained
script files.

- fish/tcsh/powershell pass `--shell <name>`; bash and zsh omit it and take
  `executor`'s POSIX default. Documented behaviour, not an oversight.
- No hook redefines `cd` (`assertNeverRedefinesCd`).

## The interactive guard

- Four of the five refuse to install in a non-interactive shell. PowerShell
  has none on purpose: `prompt` is only called by an interactive host.
- **The guards wrap the installation rather than returning early.** `return`
  inside `eval` inside a sourced file pops the *sourced file's* frame,
  skipping the rest of the user's rc with status 0.
  `TestGenerate_BashHookNeverAbortsTheRcFileThatSourcesIt` drives that exact
  shape in both interactivity states.
- `TestGenerate_HooksDoNotInstallInNonInteractiveShells` asserts **two**
  things, and either alone would pass a broken guard: nothing reached the
  stub, *and* the driver ran to its end (`rcContinuedSentinel`).
- Every hook driver therefore makes its shell interactive — `-i`, or `set
  prompt` for tcsh, which is what `$?prompt` tests. The *completion* drivers
  stay non-interactive: no guard to satisfy, and `bash -i` without a tty
  prints job-control warnings containing "bash", which one case asserts is
  absent.

## Completion

- **No generated script reads `subcommands`.** Each hardcodes its own list in
  its own syntax; `subcommands` is the checklist and
  `TestCompletion_ListsEverySubcommand` asserts every name appears in all
  three. `cmd/envoke`'s `TestRun_CompletionCoversEverySubcommand` compares
  `envoke help` against the **bash** script only, so a name missing from zsh
  or fish is caught by the first test and not the second.
- The descriptions in the zsh and fish scripts are duplicated between them and
  covered by neither test.
- **Generated bash must run on bash 3.2**, still `/bin/bash` on every Mac:
  `mapfile` is 4.0+ and silently produces nothing, hence the `while IFS= read
  -r` loop. The macOS runner is what exercises this.

## Two cross-shell invariants, each with a real-interpreter test

- A directory name must never reach a shell parser as code
  (`TestGenerate_HooksNeverExecuteDirectoryNames`).
- A hook must be transparent to the shell's last-command status
  (`TestGenerate_HooksAreTransparentToLastCommandStatus`). Skipping it turns
  every exit-code-aware prompt into a liar.

## Per-shell gotchas

- **bash** polls via `PROMPT_COMMAND`; its baseline var **must be seeded at
  install time**, or the first `cd` after install compares the new directory
  against itself.
- **zsh** uses native `chpwd_functions`; `$OLDPWD` needs no seeding.
- **fish** seeds its own baseline (its `$OLDPWD` isn't reliable) and needs
  `string collect` (3.4+): a bare command substitution splits multi-line
  output into one list element per line.
- **tcsh** claims `cwdcmd` outright — a pre-existing user `cwdcmd` is lost, a
  documented limitation. **Never put a directory name back into the eval
  string under any quoting**: interpolating `$owd`/`$cwd` was an exploited
  injection, one single quote in a directory name closing the quoting and
  running the rest as code with no config and no `envoke allow`. The
  directories travel through `$ENVOKE_FROM`/`$ENVOKE_TO`, `setenv`'d outside
  the eval, which csh does not re-tokenize inside double quotes. tcsh also has
  no `command` builtin — use `\envoke`.
- **powershell** joins with `Out-String` before `Invoke-Expression` (fish's
  concern), and fires only under the FileSystem provider. Provider gates
  *whether*; `ProviderPath` decides *what*. Two separate decisions.

## Testing

- Generated code is driven through real interpreters with a stub `envoke` on
  `PATH`, not string-matched. `t.Skip`s without one; `.dagger`'s
  `test-shell-*` checks run all five. A local `SKIP` means run it through
  Dagger.
- `installStub` is build-tagged, and the Windows half is not
  over-engineering: a `.cmd` shim gets its argv re-parsed by cmd.exe and its
  `echo` writes CRLF, and a `.ps1` or a function is not a *native* command, so
  PowerShell never touches `$LASTEXITCODE` and the transparency test would
  pass while asserting nothing. Hence a link to the running test binary named
  `envoke.exe`, re-entered through a windows-only `TestMain`.
- `requireHarness(t, shell)` is what a cross-shell table calls, so the four
  POSIX shells skip per case instead of taking PowerShell down with them on
  Windows.

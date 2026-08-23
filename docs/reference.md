# Reference

Every subcommand, flag, environment variable, file and exit code, in one
place. The task-oriented pages explain *why* — this page is the checklist.

## Commands

`envoke` with no arguments prints usage on stderr and exits 2.

| Command | What it does |
|---|---|
| `envoke version` | Version, commit and build date, plus the Go toolchain and OS/arch the binary was built with. Also `--version`, `-V`. |
| `envoke help` | Usage summary on stdout. Also `-h`, `--help`. |
| `envoke shell-init [<shell>]` | Prints the hook script for your rc file. See [Getting Started](getting-started.md#shell-integration). |
| `envoke completion [<shell>]` | Prints a tab-completion script. bash, zsh and fish only. |
| `envoke allow [--yes\|-y] [<path>]` | Reviews and trusts a config. With no path, covers every config envoke would load. See [Trust Model](trust.md). |
| `envoke revoke [<path>]` | Withdraws trust, removing the record and its content copy. With no path, covers every config envoke would load — the mirror of `allow`. |
| `envoke list` | Lists the configs envoke would load with the status each would get, then any trust records outside that set. See [Trust Model](trust.md#seeing-and-withdrawing-trust). |
| `envoke prune` | Drops records whose config no longer exists. |
| `envoke disable` | Stops running blocks, in every shell, until `enable`. |
| `envoke enable` | Undoes `disable`. |
| `envoke reload [--shell <name>]` | Prints the enter blocks for the current directory, to `eval`. See [Debugging](debugging.md#applying-a-config-without-leaving-the-directory). |
| `envoke exec [<from> [<to>]]` | Runs matching blocks in subprocesses. See [Non-interactive Use](non-interactive.md). |
| `envoke debug [<from> [<to>]]` | Prints what would fire, without running it. See [Debugging](debugging.md). |
| `envoke shell-hook [--shell <name>] [--] <from> <to>` | Internal; called by the generated hook on every directory change. |

`shell-init` and `completion` guess the shell from `$SHELL` when it is
omitted, and error rather than defaulting to bash for one they don't
recognise.

`allow` and `revoke` are exact opposites, including their default: with no
path each covers **the whole set** — your central config plus every
`envokerc.d` fragment — and with a path, just that file. So `envoke revoke`
undoes `envoke allow`, and neither leaves half a set in a state you didn't
ask for. Trust records for configs *outside* the current set are not touched
by either; `envoke list` shows those separately and `envoke prune` clears the
ones whose file is gone.

**The generated hook installs itself only in an interactive shell.** Sourcing
it from a non-interactive one — a `zsh -c`, a `tcsh -c` in a Makefile — is a
no-op, so a script that `cd`s never runs your blocks. `envoke exec` is the
deliberate non-interactive entry point.

`exec` and `debug` accept relative paths, and fill in what they can. `<to>`
defaults to the directory you are in, which envoke works out for itself.
`<from>` cannot be inferred at all: it defaults to `$OLDPWD`, so the
no-argument form needs a shell that exports one. PowerShell has no `$OLDPWD`,
and there the no-argument form is an error naming the form to type instead.

**One argument is `<from>`** — the half envoke cannot work out — and is
therefore the form that works in every shell. Because it can be misread as
naming the directory to act *on*, which is the opposite direction, `exec`
echoes the pair it resolved to stderr when given exactly one argument; `debug`
already leads with it. The no-argument and two-argument forms print nothing
extra.

`shell-hook` infers nothing: it only ever receives generated arguments.

## Flags

| Flag | Commands | Meaning |
|---|---|---|
| `--yes`, `-y` | `allow` | Skip the y/N confirmation. May appear before or after the path. |
| `--shell <name>` | `reload`, `shell-hook` | Which dialect to render: `bash`, `zsh`, `fish`, `tcsh`, `powershell`. Omitted means POSIX, which is what bash's and zsh's hooks rely on. An unrecognised name is an error, not a silent fallback. |
| `--` | any | Ends flag parsing, so an argument that looks like a flag isn't parsed as one. It matters for `shell-hook`, whose arguments are directory names it never chose: every hook that passes directories as arguments passes `--` first, and the tcsh hook passes them through the environment instead, so it needs none. |

## Environment variables envoke reads

| Variable | Effect |
|---|---|
| `ENVOKERC` | Central config path, used verbatim — first in the [lookup order](configuration.md#locating-the-central-config). |
| `ENVOKERC_D` | Fragment directory, used verbatim — first in [its lookup order](configuration.md#the-envokercd-directory). Independent of `$ENVOKERC`; both are loaded. |
| `XDG_CONFIG_HOME` | Third in that order: `$XDG_CONFIG_HOME/envoke/config`. Defaults to `~/.config`. |
| `XDG_DATA_HOME` | Where trust records and the disable flag live. Defaults to `~/.local/share`. |
| `HOME` / `USERPROFILE` | Expands a leading `~` in a pattern, and anchors the default config and data paths. `USERPROFILE` on Windows. |
| `SHELL` | Guessed shell for `shell-init`/`completion` with no argument. |
| `ENVOKE_DISABLE` | Per-session override of the persistent switch. `0`/`false`/`no`/`off` force envoke on; any other non-empty value forces it off; unset or empty defers to the flag. See [Debugging](debugging.md#turning-envoke-off). |
| `OLDPWD` | `<from>` for `exec` and `debug` when given no arguments. Unset — as in PowerShell, which has no counterpart — means there is nothing to infer, and both error rather than guess. tcsh maintains `$owd` instead, so an `$OLDPWD` seen there was inherited from the shell that started it and never updates: pass `<from>` yourself rather than trust it. Nothing can detect that case; it is an ordinary environment variable either way. |
| `PWD` | The current directory, used by `reload` and for `<to>` whenever `exec` or `debug` is not given one. Preferred over the process's own working directory, which is the fallback when `$PWD` is unset or relative: through a symlinked directory the two disagree, and the patterns you write describe the path you `cd`'d through. |
| `ENVOKE_FROM`, `ENVOKE_TO` | Read by `shell-hook` when it gets no positional arguments. Exists for the tcsh hook — see [Trust Model](trust.md#directory-names-are-never-executed). |

## Environment variables a matched block sees

`ENVOKE_DIR`, `ENVOKE_TYPE`, `ENVOKE_MATCH` and `ENVOKE_MATCH_N` — set
before the block's script and cleared again after it. See
[Configuration](configuration.md#what-a-matched-script-sees) for what each
holds, and for the working directory a block actually runs in.

`ENVOKE_DIR` is always the directory as your shell spelled it. For a
[confined](configuration.md#bringing-a-projects-own-config-in) fragment —
one symlinked in from a project — `ENVOKE_MATCH` and the `ENVOKE_MATCH_N`
captures come from the **symlink-resolved** path instead, because that is what
such a config's patterns are matched against. For every other config the two
forms are the same.

## Files

| Path | Contents |
|---|---|
| `$ENVOKERC`, `~/.envokerc`, or `$XDG_CONFIG_HOME/envoke/config` | Your central config, in lookup order. The first that exists is used and the others are ignored. |
| `$ENVOKERC_D`, `~/.envokerc.d`, or `$XDG_CONFIG_HOME/envoke/envokerc.d` | The fragment directory, in lookup order — again, only the first that exists. Every file in it is a config, applied in order of its path relative to the directory; at most 512 files, nested at most 8 levels deep. See [Configuration](configuration.md#the-envokercd-directory). |
| `<data home>/envoke/allow/<sha256 of the config's absolute path>` | The approved content's hash — the trust token. |
| `<same>.content` | A plaintext copy of the approved config, for the diff on re-approval. |
| `<same>.path` | The config's absolute path, so `list` and `prune` can resolve the record. |
| `<data home>/envoke/disabled` | Present when `envoke disable` is in effect. Its content is never read. |

`<data home>` is `$XDG_DATA_HOME`, or `~/.local/share` when that isn't set.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success — including the states that are not failures: nothing matched, nothing to prune, a config that was already untrusted, envoke being disabled, and `shell-hook` finding an untrusted config (it reports on stderr and renders nothing, so evaluating its empty output stays a safe no-op). |
| 1 | An error: a config that can't be read or doesn't parse, no config found where one is required, an unsupported shell for `shell-init`/`completion`, an aborted `allow`, or a config that wasn't applied for `exec` and `reload` — both are typed deliberately, so doing nothing has to be loud. With several configs in play, one that is untrusted or unparseable never stops the others from running; it is reported and the exit code says something was skipped. |
| 2 | A usage error: unknown command, wrong number of arguments, unknown flag, unrecognised `--shell`, or a `$SHELL` that can't be identified. |
| 130 | `envoke exec` was interrupted — see [Non-interactive Use](non-interactive.md#interruption). |

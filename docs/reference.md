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
| `envoke allow [--yes\|-y] [<path>]` | Reviews and trusts a config. See [Trust Model](trust.md). |
| `envoke revoke [<path>]` | Withdraws trust, removing the record and its content copy. |
| `envoke list` | Lists every trust record and the status of its config. |
| `envoke prune` | Drops records whose config no longer exists. |
| `envoke disable` | Stops running blocks, in every shell, until `enable`. |
| `envoke enable` | Undoes `disable`. |
| `envoke reload [--shell <name>]` | Prints the enter blocks for the current directory, to `eval`. See [Debugging](debugging.md#applying-a-config-without-leaving-the-directory). |
| `envoke exec [<from> <to>]` | Runs matching blocks in subprocesses. See [Non-interactive Use](non-interactive.md). |
| `envoke debug [<from> <to>]` | Prints what would fire, without running it. See [Debugging](debugging.md). |
| `envoke shell-hook [--shell <name>] [--] <from> <to>` | Internal; called by the generated hook on every directory change. |

`shell-init` and `completion` guess the shell from `$SHELL` when it is
omitted, and error rather than defaulting to bash for one they don't
recognise.

`exec` and `debug` accept relative paths and default to `$OLDPWD -> $PWD`
when given no arguments. `shell-hook` does neither: it only ever receives
generated arguments.

## Flags

| Flag | Commands | Meaning |
|---|---|---|
| `--yes`, `-y` | `allow` | Skip the y/N confirmation. May appear before or after the path. |
| `--shell <name>` | `reload`, `shell-hook` | Which dialect to render: `bash`, `zsh`, `fish`, `tcsh`, `powershell`. Omitted means POSIX, which is what bash's and zsh's hooks rely on. An unrecognised name is an error, not a silent fallback. |
| `--` | any | Ends flag parsing, so an argument that looks like a flag isn't parsed as one. It matters for `shell-hook`, whose arguments are directory names it never chose, and every generated hook passes it. |

## Environment variables envoke reads

| Variable | Effect |
|---|---|
| `ENVOKERC` | Config path, used verbatim — first in the [lookup order](configuration.md#locating-the-config-file). |
| `XDG_CONFIG_HOME` | Third in that order: `$XDG_CONFIG_HOME/envoke/config`. Defaults to `~/.config`. |
| `XDG_DATA_HOME` | Where trust records and the disable flag live. Defaults to `~/.local/share`. |
| `HOME` / `USERPROFILE` | Expands a leading `~` in a pattern, and anchors the default config and data paths. `USERPROFILE` on Windows. |
| `SHELL` | Guessed shell for `shell-init`/`completion` with no argument. |
| `ENVOKE_DISABLE` | Per-session override of the persistent switch. `0`/`false`/`no`/`off` force envoke on; any other non-empty value forces it off; unset or empty defers to the flag. See [Debugging](debugging.md#turning-envoke-off). |
| `OLDPWD`, `PWD` | The transition `exec` and `debug` assume when given no arguments. `reload` uses `PWD` alone. |
| `ENVOKE_FROM`, `ENVOKE_TO` | Read by `shell-hook` when it gets no positional arguments. Exists for the tcsh hook — see [Trust Model](trust.md#directory-names-are-never-executed). |

## Environment variables a matched block sees

`ENVOKE_DIR`, `ENVOKE_TYPE`, `ENVOKE_MATCH` and `ENVOKE_MATCH_N` — set
before the block's script and cleared again after it. See
[Configuration](configuration.md#what-a-matched-script-sees) for what each
holds, and for the working directory a block actually runs in.

## Files

| Path | Contents |
|---|---|
| `$ENVOKERC`, `~/.envokerc`, or `$XDG_CONFIG_HOME/envoke/config` | Your config, in lookup order. |
| `<data home>/envoke/allow/<sha256 of the config's absolute path>` | The approved content's hash — the trust token. |
| `<same>.content` | A plaintext copy of the approved config, for the diff on re-approval. |
| `<same>.path` | The config's absolute path, so `list` and `prune` can resolve the record. |
| `<data home>/envoke/disabled` | Present when `envoke disable` is in effect. Its content is never read. |

`<data home>` is `$XDG_DATA_HOME`, or `~/.local/share` when that isn't set.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success — including the states that are not failures: nothing matched, nothing to prune, a config that was already untrusted, envoke being disabled, and `shell-hook` finding an untrusted config (it reports on stderr and renders nothing, so evaluating its empty output stays a safe no-op). |
| 1 | An error: a config that can't be read or doesn't parse, no config found where one is required, an unsupported shell for `shell-init`/`completion`, an aborted `allow`, or an untrusted config for `exec` and `reload` — both are typed deliberately, so doing nothing has to be loud. |
| 2 | A usage error: unknown command, wrong number of arguments, unknown flag, unrecognised `--shell`, or a `$SHELL` that can't be identified. |
| 130 | `envoke exec` was interrupted — see [Non-interactive Use](non-interactive.md#interruption). |

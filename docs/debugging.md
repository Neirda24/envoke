---
title: Inspecting and switching off
description: >-
  envoke debug prints what a directory change would fire without running it;
  disable and enable switch envoke off; reload applies a config where you stand.
---

# Inspecting and switching off

Three tools that never run a block, or stop blocks running:

| | |
|---|---|
| [`envoke debug`](#envoke-debug) | what a directory change *would* fire, without running any of it |
| [`envoke disable` / `enable`](#turning-envoke-off) | stop running blocks — every shell, or just this one |
| [`eval "$(envoke reload)"`](#applying-a-config-without-leaving-the-directory) | apply a freshly approved config where you are standing |

If you are here because something didn't happen and you want the answer rather
than the tour, start with [Troubleshooting](troubleshooting.md) — it is ordered
by which cause actually comes up most.

## envoke debug

`envoke debug [<from> [<to>]]` prints which `enter`/`leave` blocks would fire for a given directory transition, without ever running them.

```sh
envoke debug ~/Projects ~/Projects/envoke   # both directories named
envoke debug ~/Projects                     # <to> is where you are standing
envoke debug                                # <from> is $OLDPWD as well
```

Both arguments are optional and may be relative. **One argument is `<from>`** —
envoke can always work out where you are and can only be told where you came
from — and that is the form to reach for whenever a page here says "run
`envoke debug`", because it is typeable in every shell.

The no-argument form needs `$OLDPWD`, which only POSIX shells maintain.
PowerShell has none and fails loudly, naming the one-argument form. tcsh keeps
`$owd` instead, so any `$OLDPWD` a tcsh has was inherited when it started and
has been wrong ever since — a plausible, silent, wrong answer from a
diagnostic, which is the worse of the two failures. Name `<from>` yourself on
both. (The generated tcsh hook is unaffected: it passes `$owd` itself, so this
is only about commands you type.) See
[`OLDPWD`](reference.md#environment-variables-envoke-reads).

This runs the same resolution the live `shell-hook` does, and additionally reports the status of every config in play — but it never calls the code path that executes or renders a script, regardless of trust status. That's the point: `envoke debug` is safe to run against a config you haven't approved yet, or one you're actively editing and don't want to accidentally trigger. It also never asks you anything.

Its first lines list every config in play — your central one plus each `envokerc.d` fragment — and what would happen with it:

```
$ envoke debug ~/work/api ~/work/api/src
envoke debug: /home/you/work/api -> /home/you/work/api/src
  config /home/you/.envokerc (trusted)
  config /home/you/.config/envoke/envokerc.d/api (NOT trusted -- run `envoke allow /home/you/.config/envoke/envokerc.d/api` before these would actually run)
    symlink to /home/you/work/api/envoke.conf
    confined to /home/you/work/api -- its blocks cannot match outside that directory, whatever their patterns say
  enter /home/you/work/api/src (line 4 of /home/you/.config/envoke/envokerc.d/api: ./src)
    export SRC=1
```

Each block names the file it was declared in, since that is what says whose approval gates it. A config can also read as `failed to load`.

The indented lines under a `config` line carry what a status on its own can't
say: the file a symlinked fragment actually leads to — the one envoke parses —
and, for a [confined](configuration.md#bringing-a-projects-own-config-in) one,
the directory outside which none of its blocks can match. Together they answer
the combination that otherwise has no explanation: a config listed `trusted`,
loaded, and firing nothing. The pattern is not broken; it points out of the
tree the fragment is bounded to. A config that really lives in your config
directory has no bound to state, so it gets no `confined to` line — though if
you reached it through a link, the target is still reported, since that names
the file that was read.

A third line, `its symlink could not be followed, so that bound is the link's
own directory`, is the fail-closed case: envoke could not resolve the link, so
it bounds the fragment to where the link itself sits.

Use it to:

- Develop a new config without surprises — see exactly which blocks a transition would match before you `envoke allow` it.
- Confirm a pattern change matches (or stops matching) the directories you expect.
- Find out which configs are being picked up at all — the central one and every `envokerc.d` fragment — and check their trust status without inspecting the trust store directly.

It also points out when a matched block will run somewhere other than the directory it matched — see [Where the script runs](configuration.md#where-the-script-runs).

## Turning envoke off

When the block you're debugging is the one breaking your shell, you don't want to comment the hook out of your rc file and open a new terminal.

```sh
envoke disable   # every shell, from now on
envoke enable    # undo it
```

`envoke disable` sets a flag under your data home, so it survives new shells and reboots until you run `envoke enable`. Trust records are untouched: switching envoke off is not withdrawing approval, and coming back doesn't mean re-approving anything.

For a single terminal, `ENVOKE_DISABLE` overrides that flag in both directions:

```sh
export ENVOKE_DISABLE=1   # off in this shell only
export ENVOKE_DISABLE=0   # on in this shell, even if `envoke disable` is set
unset ENVOKE_DISABLE      # back to whatever the persistent flag says
```

While envoke is off:

- `cd` does nothing at all and says nothing — the hook runs on every directory change, so a warning there would be a permanent nuisance.
- `envoke exec` and `envoke reload` say why they did nothing, on stderr, and still exit 0. Being switched off is what was asked for, not a failure.
- `envoke debug` keeps working and reports the switch alongside the trust status, since it never executes anything anyway.
- `envoke allow`, `revoke`, `list` and `prune` are unaffected. Managing trust is a separate question from whether blocks run.

## Applying a config without leaving the directory

`envoke allow` runs as a child of your shell and cannot export anything into it, so a config you just approved takes effect on your next `cd`. To apply it where you're standing:

```sh
eval "$(envoke reload)"
```

That re-runs the `enter` blocks matching your current directory and everything above it, exactly as if you had arrived from outside. `envoke allow` prints this line for you when it succeeds.

=== "bash"

    ```sh
    eval "$(envoke reload)"
    ```

=== "zsh"

    ```sh
    eval "$(envoke reload)"
    ```

=== "fish"

    ```fish
    envoke reload --shell fish | source
    ```

=== "tcsh"

    ```tcsh
    envoke reload --shell tcsh | source /dev/stdin
    ```

=== "PowerShell"

    ```powershell
    envoke reload --shell powershell | Out-String | Invoke-Expression
    ```

`reload` runs `enter` blocks only. Nothing has been left, and envoke never snapshots state to unwind later — if the previous version of your config exported something the new one doesn't, clear it yourself or open a new shell.

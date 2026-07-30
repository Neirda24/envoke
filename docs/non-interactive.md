# Non-interactive use

The shell hook exists to change *your interactive shell* — that's why
`envoke shell-hook` prints shell code for your shell to `eval`/`source`
rather than running anything itself. Scripts, Makefiles and CI jobs have no
interactive shell to hook into, so they get a separate entry point:

```sh
envoke exec <from> <to>
```

`envoke exec` resolves the same blocks the shell hook would for that
directory change and runs each one in its own `sh -c` subprocess, with the
matched directory as the working directory and the same `ENVOKE_*`
variables set.

```sh
envoke exec "$PWD" ~/Projects/my-app
```

Both arguments are optional and may be relative. With none, `envoke exec`
uses the shell's own last transition (`$OLDPWD` to `$PWD`), the same default
as `envoke debug`.

!!! warning "Unix only"

    Blocks run through `sh -c`, so `envoke exec` needs a POSIX shell on
    `PATH`. On Windows it will not find one. The shell hook is unaffected —
    the PowerShell hook renders into PowerShell itself.

## What it does *not* do

**Side effects stay in the subprocess.** `export`, `source` and `cd` inside
a block affect that block's own subprocess and nothing else — they cannot
reach the shell that invoked `envoke exec`, or any later command in your
script. That is not a limitation to work around; it is what "subprocess"
means. If you want a block's `export` to be visible afterwards, you need the
shell hook and an interactive shell, or you need the block to write
something your script then reads.

So `envoke exec` is for blocks whose value is their *effect* — writing a
file, warming a cache, starting a service, running a code generator — not
for blocks whose value is the environment they leave behind.

## Trust applies exactly as it does everywhere else

`envoke exec` refuses to run anything from a config that has not been
through [`envoke allow`](trust.md) since its last edit:

```
envoke: /home/you/.envokerc: config is not trusted: run `envoke allow /home/you/.envokerc`
```

It exits 1 in that case, and 1 as soon as any block exits non-zero —
remaining blocks are not run, and nothing is unwound (see the
enter/leave independence rule in [Configuration](configuration.md)).

That stop-on-failure behaviour is specific to `envoke exec`. The shell hook
does the opposite: it hands your shell every matched block at once, so a
failing one doesn't stop the rest. See [When a block
fails](configuration.md#when-a-block-fails).

`envoke disable` applies here too, and `envoke exec` says so rather than
silently doing nothing:

```
envoke: disabled by the persistent switch -- no blocks were run
```

It still exits 0 — being switched off is what was asked for, not a failure.
Set `ENVOKE_DISABLE=0` for a job that must run its blocks regardless.

## Interruption

A SIGINT or SIGTERM interrupts the running block rather than killing
`envoke` out from under it, so a `trap` in the block gets a chance to clean
up; it is killed five seconds later if it hasn't exited. `envoke exec` then
exits 130.

For CI, approve the config as part of provisioning, where `--yes` skips the
interactive prompt:

```sh
envoke allow --yes ./ci/envokerc
```

## Seeing what would run first

[`envoke debug <from> <to>`](debugging.md) prints the same resolution
without executing anything, trusted or not. It is the right thing to reach
for when a job's blocks did not do what you expected.

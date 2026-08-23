# Non-interactive use

The shell hook exists to change *your interactive shell* — that's why
`envoke shell-hook` prints shell code for your shell to `eval`/`source`
rather than running anything itself. Scripts, Makefiles and CI jobs have no
interactive shell to hook into, so they get a separate entry point:

```sh
envoke exec [<from> [<to>]]
```

`envoke exec` resolves the same blocks the shell hook would for that
directory change and runs each one in its own `sh -c` subprocess, with the
matched directory as the working directory and the same `ENVOKE_*`
variables set.

```sh
envoke exec "$PWD" ~/Projects/my-app
```

Both arguments are optional and may be relative, on the same terms as
`envoke debug`. `<to>` defaults to the directory you are in. `<from>` cannot
be inferred, so it defaults to `$OLDPWD` and the no-argument form needs a shell
that exports a live one. PowerShell has no `$OLDPWD` and errors there; tcsh
maintains `$owd` instead, so an `$OLDPWD` inherited from the shell that started
it resolves a stale `<from>` with nothing to flag it. Pass `<from>` in either.

A single argument is `<from>`, with `<to>` inferred, which is the form every
shell can type:

```sh
cd ~/Projects/my-app
envoke exec ~/Projects
```

For that form alone, `exec` writes the pair it resolved to stderr:

```
envoke exec: /home/you/Projects -> /home/you/Projects/my-app
```

`envoke exec <dir>` can be read as "run the blocks for this directory", which
would be the opposite direction — that directory's `leave` blocks, not its
`enter` ones — so the line makes the misreading visible. The no-argument and
two-argument forms print nothing extra, so no invocation that already worked
changes what it writes. It goes to stderr because stdout belongs to the blocks,
and a caller capturing them must not collect a diagnostic.

!!! warning "Unix only"

    Blocks run through `sh -c`, so `envoke exec` needs a POSIX shell on
    `PATH`. Windows does not provide one by default, and `envoke exec` says
    so rather than reporting a missing program you never asked for:

    ```
    envoke: enter ./api (C:\src\api:3): no POSIX shell ("sh") on PATH
    envoke: exec runs each block through `sh`; install a POSIX shell (Git for Windows, MSYS2 or WSL each provide one) or use the shell hook, which runs blocks in the shell you already have
    ```

    The shell hook is unaffected — the PowerShell hook renders into
    PowerShell itself, and needs no `sh`.

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
envoke: /home/you/.envokerc: config is not trusted
envoke: approve a config with `envoke allow` before it will run here
```

It exits 1 in that case, and 1 as soon as any block exits non-zero —
remaining blocks are not run, and nothing is unwound (see the
enter/leave independence rule in [Configuration](configuration.md)).

Nothing prompts, here or anywhere else — approve configs beforehand, in your
provisioning step, where `--yes` skips the interactive prompt:

```sh
envoke allow --yes ./ci/envokerc
```

`envoke exec` is also the *only* way to run blocks non-interactively: the
generated shell hooks refuse to install themselves in a non-interactive shell,
so a script that `cd`s does not run your enter/leave blocks by accident.

With several configs in play, one that is untrusted or unparseable does not
stop the others: it is reported on stderr, the trusted ones still run, and the
exit code is 1 to say something was skipped. One fragment a `git pull` just
rewrote must not silently disable the config you did approve.

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

## Seeing what would run first

[`envoke debug [<from> [<to>]]`](debugging.md) prints the same resolution
without executing anything, trusted or not. It is the right thing to reach
for when a job's blocks did not do what you expected.

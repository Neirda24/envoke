# Debugging

`envoke debug <from> <to>` prints which `enter`/`leave` blocks would fire for a given directory transition, without ever running them.

```sh
envoke debug ~/Projects ~/Projects/envoke
```

Both arguments are optional and may be relative. With none, envoke uses your
shell's own last transition (`$OLDPWD` to `$PWD`), which answers "why didn't
anything fire when I just cd'd here?" without retyping two absolute paths:

```sh
cd ~/Projects/envoke
envoke debug
```

This runs the same resolution logic (`matcher.Resolve`) as the live `shell-hook`, and additionally reports whether the config is currently trusted — but it never calls the code path that executes or renders a script, regardless of trust status. That's the point: `envoke debug` is safe to run against a config you haven't approved yet, or one you're actively editing and don't want to accidentally trigger.

Use it to:

- Develop a new config without surprises — see exactly which blocks a transition would match before you `envoke allow` it.
- Confirm a pattern change matches (or stops matching) the directories you expect.
- Check trust status without needing to inspect the trust store directly.

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

While envoke is off, `cd` does nothing at all and says nothing — it runs on every directory change, so a warning there would be a permanent nuisance. `envoke exec` says why it did nothing and still exits 0. `envoke debug` keeps working and reports the switch alongside the trust status, since it never executes anything anyway.

## Applying a config without leaving the directory

`envoke allow` runs as a child of your shell and cannot export anything into it, so a config you just approved takes effect on your next `cd`. To apply it where you're standing:

```sh
eval "$(envoke reload)"
```

That re-runs the `enter` blocks matching your current directory and everything above it, exactly as if you had arrived from outside. `envoke allow` prints this line for you when it succeeds.

Per shell:

```fish
envoke reload --shell fish | source
```

```tcsh
envoke reload --shell tcsh | source /dev/stdin
```

```powershell
envoke reload --shell powershell | Out-String | Invoke-Expression
```

`reload` runs `enter` blocks only. Nothing has been left, and envoke never snapshots state to unwind later — if the previous version of your config exported something the new one doesn't, clear it yourself or open a new shell.

# internal/state

envoke's on-disk runtime state.

`DataHome()` is the `$XDG_DATA_HOME`/`~/.local/share` resolution, owned here
because `internal/trust` needs it too (it used to duplicate the lookup).

**Runtime state goes under `$XDG_DATA_HOME`, not `$XDG_CONFIG_HOME`.** The
trust store and the disabled marker are state, not user config, so they follow
XDG's separate data-home convention. Anything new that envoke writes for
itself belongs here too.

`Disabled() (bool, Source, error)` / `Disable()` / `Enable()` back the off
switch: a marker file at `<data home>/envoke/disabled` for the persistent
half, `$ENVOKE_DISABLE` for the per-session half. **The env var decides on its
own, in both directions** — `=0` has to be able to turn envoke back on in a
shell where the flag is set, or the persistent switch would be inescapable
without deleting a file. Unset or empty defers to the flag; any other value
counts as disabled, so a typo errs toward *not* executing scripts.

`shell-hook` checks it first and stays completely silent (it runs on every
`cd`); `exec` and `reload` report it on stderr and still exit 0; `debug` keeps
working and reports it next to the trust status.

Disabling never touches trust records — being off is not withdrawing approval.

# internal/state

Data home, and the disable/enable off switch.

- **Anything envoke writes for itself goes under `$XDG_DATA_HOME`**, never
  `$XDG_CONFIG_HOME`: it is state, not user config. `DataHome()` is owned here
  because `internal/trust` needs it too and used to duplicate the lookup.
- `$ENVOKE_DISABLE` decides on its own **in both directions** — `=0` must be
  able to turn envoke back on where the persistent flag is set, or the flag is
  inescapable without deleting a file.
- Disabling never touches trust records: being off is not withdrawing
  approval.

How each command reacts, since no single file states it:

| Command | On disabled |
|---|---|
| `shell-hook` | silent, exit 0 — it runs on every `cd` |
| `exec`, `reload` | report on stderr, exit 0 |
| `debug` | keeps working, reports it beside the trust status |

# cmd/envoke

`run(args, stdout, stderr, stdin) int` is the testable dispatcher `main()`
wraps.

**The subcommand catalogue is `usage()` at the bottom of `main.go`** — what
`envoke help` prints, so it is the version users see. Nothing here repeats it.

## Why these commands behave the way they do

- **`shell-init` errors instead of defaulting to bash** for an unrecognised
  `$SHELL`: a wrong guess writes a broken rc file whose breakage surfaces far
  from its cause.
- **`allow` and `revoke` with no path both cover the whole set.** A set one
  command approves has to be a set the other can withdraw. `configTargets` is
  the shared resolver; don't reintroduce a per-command version.
- **`reload` exits non-zero when anything was refused** — it was typed, so
  doing nothing has to be loud. The hook must stay silent.
- **`debug`'s `printWorkingDirNote`** flags that a matched block runs where
  the shell landed, not where it matched: true of the hook, not of `exec`.

## Argument handling

- `exec` and `debug` share `transitionArgs`. **The two halves of a transition
  are not symmetric — don't "simplify" them into symmetry.** `<to>` is always
  inferable; `<from>` is inferable from nothing, and PowerShell exports
  neither `$PWD` nor `OLDPWD`, so the no-argument form is a POSIX-only
  convenience and one positional meaning `<from>` is what works in any shell.
- One positional also reads as "act on this directory", which for `exec`
  would run that directory's *leave* blocks. `cmdExec` echoes the resolved
  pair to stderr **for that form alone**, so nothing that already worked
  starts emitting a line it didn't.
- **`shell-hook` infers nothing.** Every hook passing directories as arguments
  passes `--` first; tcsh's passes no positionals at all (see
  [`internal/shellinit`](../../internal/shellinit/CLAUDE.md)).
- Flag sets are stdlib `flag`, one per subcommand via `newFlagSet`. No
  `cobra`/`urfave-cli`: nothing justified it; revisit only if subcommand
  parsing outgrows `flag`.
- **Don't hand-roll argument scanning.** The previous version recognised
  `--shell` only in position 0, couldn't accept a path named `-y`, and had no
  `--`. The one exception is `cmdAllow` picking `--yes`/`-y` back out of the
  positionals, because `envoke allow <path> --yes` shipped as documented
  behaviour and stdlib `flag` stops at the first positional.

## Locating, loading, gating

- `locateConfigs` is the enforcement point for "envoke never discovers a
  config": the only caller of `config.Locate`/`LocateDir`, and
  `configset.Load` is handed those two paths and can reach nothing else.
- `loadConfigSet` reads them, `configPaths` deliberately doesn't. That is the
  only difference there should ever be between the two.
- `runnable`/`mayRun` decide each config **once** however many of its blocks
  matched. `internal/envoke.decide` is the same shape for `exec`; changing one
  means checking the other.

## Terminal output is escaped by default

- `fprintf`/`fprintln`/`fprint` escape every `string` and `error` argument.
  Config text and directory names are both attacker-controllable and both
  reach a terminal.
- **Check the lines *around* a change, not only the one printing untrusted
  text.** Round after round of fixes escaped the obvious line and missed the
  neighbour that merely described it — a path beside an escaped body, an error
  quoting an escaped path.
- The exceptions are the `raw` type: generated shell code, which must reach
  the caller's `eval` byte for byte, and envoke's own usage text. `grep raw(`
  is the complete list, and `TestRun_GeneratedShellCodeIsNeverEscaped` stops
  the escaping being widened onto the stream that carries code.

## Testing

Tests run on all three platforms. Windows works because of three helpers, and
a fixture that skips them breaks only there:

- `tp`/`np` build a volume-prefixed path, `tp` with forward slashes and `np`
  native. **A pattern always wants `tp`** (regexes matched against
  `matcher.MatchPath`, so `C:\a` reads `\a` as an escape); **a path envoke
  printed always wants `np`**. Swapping the two is how this port breaks. A
  pattern built from a real directory goes through `filepath.ToSlash`, not
  `tp`.
- `configBody` prefixes the volume onto every `/`-leading pattern, so config
  bodies stay written the readable way.
- `fragmentDir` returns a **resolved** directory: the fragment walk resolves
  before walking, and `%TMP%` on a Windows runner is normally the 8.3 short
  form.

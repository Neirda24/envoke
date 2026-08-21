# cmd/envoke

`run(args, stdout, stderr, stdin) int` is the testable dispatcher `main()`
wraps.

**The subcommand catalogue is `usage()` at the bottom of `main.go`** — every
command, its flags, its defaults, and where blocks come from. That text is what
`envoke help` prints, so it is the version users see and the one to keep
correct. Nothing below repeats it; what follows is only what a synopsis can't
carry.

## Why these commands behave the way they do

- **`shell-init` errors instead of defaulting to bash** for an unrecognised
  `$SHELL`. A wrong guess writes a broken rc file whose breakage surfaces far
  from its cause.
- **`allow` and `revoke` with no path both cover the whole set**, central
  config and every fragment. Splitting rules across files is an organisational
  choice, not a decision to approve them one at a time — and a set one command
  approves has to be a set the other can withdraw, or the whole-set default has
  no inverse. `configTargets` is the shared resolver that keeps them from
  drifting on what "no path" means; don't reintroduce a per-command version of
  it. (`allow` additionally reports and skips one broken fragment rather than
  blocking the rest.)
- **`allow` prints the `eval "$(envoke reload)"` line on success** because it
  is a child process and cannot export into the shell that ran it.
- **`reload` exits non-zero when anything was refused.** It was typed, so doing
  nothing has to be loud — unlike the hook, which must stay silent.
- **`exec` runs under `signal.NotifyContext`** so SIGINT/SIGTERM interrupts the
  block rather than orphaning its `sh`.
- **`debug`'s `printWorkingDirNote`** flags that a matched block runs where the
  shell landed, not in the directory it matched — true of the hook, not of
  `exec`.
- **`disable`/`enable` share `cmdSwitch`**, which warns when `$ENVOKE_DISABLE`
  overrides what was just asked for, so neither can appear to do nothing.

## Argument handling

`exec` and `debug` share `transitionArgs`: both are human-typed, so both accept
relative paths and infer what they can. **The two halves of a transition are not
symmetric — don't "simplify" them into symmetry.** `<to>` is always inferable
(`currentDir`: `$PWD`, else `os.Getwd`); `<from>` is inferable from nothing, and
PowerShell exports neither `$PWD` nor `OLDPWD`, so the no-argument form is a
POSIX-only convenience while one positional meaning `<from>` is the form that
can be typed in any shell. One positional also reads as "act on this
directory", which for `exec` would run that directory's *leave* blocks instead:
`debug` opens with the pair it resolved, and `cmdExec` echoes the same line to
stderr **for that form alone**, so no invocation that already worked starts
emitting a line it didn't before.

**`shell-hook` infers nothing** — it only ever receives generated arguments,
and every hook that passes directories as arguments passes `--` first, so a
directory named like a flag can't be parsed as one. tcsh's passes no
positionals at all, sending the directories through
`$ENVOKE_FROM`/`$ENVOKE_TO` instead, which is the stronger form of the same
guarantee (see `internal/shellinit`'s notes for why that eval string has to stay
a constant).

Flag sets are stdlib `flag`, one per subcommand via `newFlagSet`
(`ContinueOnError`, output discarded, no default usage dump) so `run` stays a
plain function returning an exit code and the usage text stays in envoke's own
voice. No `cobra`/`urfave-cli`: nothing justified it when this was decided;
revisit only if subcommand parsing genuinely outgrows `flag`.

Don't hand-roll argument scanning. The previous version only recognised
`--shell` in position 0, couldn't accept a path named `-y`, and had no `--`.
The one deliberate exception is `cmdAllow` picking `--yes`/`-y` back out of the
positionals, because `envoke allow <path> --yes` shipped as documented
behaviour and stdlib `flag` stops at the first positional.

## Locating, loading, gating

`locateConfigs` is the one place that resolves *where* configs live, which makes
it the enforcement point for "envoke never discovers a config": it is the only
caller of `config.Locate`/`config.LocateDir`, and `configset.Load` is handed
those two paths and can reach nothing else. `loadConfigSet` reads them and
`configPaths` deliberately doesn't, and that difference is the only thing that
should ever differ between the two.

`runnable` filters matched blocks down to the ones envoke may act on, deciding
each config **once** however many of its blocks matched, and `mayRun` reports
the ones it held back. `shell-hook` checks every matched config's decision
before ever calling `executor.Render`.

## Terminal output is escaped by default

Terminal output goes through the local `fprintf`/`fprintln`/`fprint` helpers
rather than `_, _ = fmt.Fprintf(...)` at every call site. Two reasons, and the
second is the load-bearing one: a failed write to a terminal is not something a
CLI can act on (and errcheck is in golangci-lint's default set), and **these
helpers escape every `string` and `error` argument through `sanitize`**.

Config text and directory names are both attacker-controllable in ordinary
situations and both reach a terminal. Round after round of fixes each escaped
the obvious untrusted text and missed a neighbouring line that merely described
it — a path next to an escaped body, an error message quoting an escaped path.
So check the lines *around* a change, not only the one that prints untrusted
text. The split that holds is format string (a Go literal here) versus
argument, so the argument is escaped by default and the exceptions are the
`raw` type — generated shell code, which must reach the caller's `eval` byte
for byte, and envoke's own usage text. `grep raw(` is the complete list, and
`TestRun_GeneratedShellCodeIsNeverEscaped` is what stops the escaping being
widened onto the stream that carries code.

## Testing

CI runs this package's tests on all three platforms. Windows works because of
three helpers, and a fixture that skips them breaks only there:

- `tp`/`np` (copied from `internal/matcher`, same names deliberately) build a
  volume-prefixed path — `tp` with forward slashes, `np` native. **A pattern
  always wants `tp`** (patterns are regexes matched against
  `matcher.MatchPath`, so `C:\a` would read `\a` as an escape); **a path envoke
  printed always wants `np`**, since it went through `filepath.Clean`. Swapping
  those two is the way this port breaks.
- `configBody` prefixes the volume onto every `/`-leading pattern in a fixture,
  so config bodies stay written the readable way.
- `fragmentDir` returns a *resolved* directory, because the fragment walk
  resolves before walking and `%TMP%` on a Windows runner is normally the 8.3
  short form — without it, every assertion comparing its own path against
  envoke's output fails there and nowhere else.

A pattern built from a real directory goes through `filepath.ToSlash(dir)`, not
`tp`.

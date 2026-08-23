---
name: security-invariants
description: Review an envoke change against the project's own security invariants — the trust gate, read-once/TOCTOU, terminal escaping, per-dialect shell quoting, confinement, and the no-discovery rule. Use before merging anything that touches config loading, trust, execution, or terminal/shell output.
---

# envoke security invariants

envoke decides whether to run shell code found in a file, then writes shell
code into the user's interactive shell. The generic review lenses miss most of
what matters here.

This is the index: eight invariants, and for each the **one place that enforces
it** and the **test that would fail**. If a change weakens one, that is the
finding. The reasoning behind each lives in the package's own `CLAUDE.md` — read
that before deciding a change is safe, and the source before deciding a doc page
is still accurate. Project-wide rules: [root CLAUDE.md](../../../CLAUDE.md); the
user-facing rationale is `docs/trust.md` and `docs/design-notes.md`.

## 1. The trust gate is single

`configset.Decide`, called from `internal/envoke.Transition` and from
`cmd/envoke`'s `mayRun`. **Any new path reaching `executor.Run` or
`executor.Render` must go through it.** `Transition` takes the loaded set rather
than `[]*config.Config` so no caller can pre-empt the decision — a refactor
"simplifying" that signature removes the gate.

## 2. A config feeding a trust decision is read exactly once

`config.LoadFile`/`LoadFragmentResolved` return the source bytes
alongside the parsed config; `trust.Allow`/`IsTrusted` take those bytes, never a
path. **A path-taking convenience wrapper on the trust API is the hole, not a
refactor.** Check that no new path reads a config twice — including between
showing a user a config and recording its hash.

## 3. Terminal output is escaped by default

`cmd/envoke`'s `fprintf`/`fprintln`/`fprint` escape every `string` and `error`
argument through `sanitize`; the format string is a Go literal and is not. The
only exceptions are the `raw` type — `grep 'raw('` is the complete list.

Findings to look for: a new `fmt.Fprintf(w, ...)` at a call site; a new `raw`
without a reason; and an *unescaped neighbour* of an escaped line — the line
that names the path, or the error that quotes it. That last one is what every
previous round of fixes missed, so read around the change.
`TestRun_GeneratedShellCodeIsNeverEscaped` stops the escaping being widened onto
the stream that carries code.

## 4. Quoting is per shell dialect, and they are not interchangeable

Anything generating shell text from a directory name or capture group uses the
`shellProfile`'s own quoter. `tcshQuote` is **not** `posixQuote`. Extend the
hostile-basename table in `TestRender_QuotingRoundTripsThroughRealShells` rather
than writing a one-off test — it round-trips through real interpreters.

The hook side is the same property one layer out: no directory name may reach a
shell parser as code (`TestGenerate_HooksNeverExecuteDirectoryNames`). The tcsh
hook is the sharp edge — its `eval` string must stay a compile-time constant,
with the directories arriving via `ENVOKE_FROM`/`ENVOKE_TO`. Interpolating them
into that string, under any quoting, reopens an exploited injection that needed
no config file and no `envoke allow`.

## 5. Block variables do not leak between blocks

`blockEnv` strips every `ENVOKE_*` out of `os.Environ()` before `Run` adds this
block's; `Render` can only clear what it set, so it unsets each block's vars
right after its script (`shellProfile.unset`). Dropping that teardown for
"cleaner" output is a finding — and so is assuming the two paths guarantee the
same thing.

## 6. Confinement of configs that point outside the config directory

A symlinked project config is content someone else's commit can rewrite.
`newMatch` refuses any match outside `Config.Dir` for a `Local` config, whatever
its patterns say, and it lives there because every `Match` in the codebase is
built there (`NewMatch` is a one-line wrapper over it). The bound is not the
first gate: the **pattern runs first**, as the cheaper refusal, and only what it
admits is put to the bound — so a change to pattern compilation changes what the
bound is ever asked about
(`TestNewMatch_ConfinedConfigRefusesOnThePatternBeforeTheBound` pins the order
by watching `candidate.bounds`).

The check itself is `candidate.withinBound`: `Within` lexically, then
`sameDirOrAncestor` walking up by `os.SameFile`. Four ways to break it without
touching the refusal in `newMatch`:

- **Walking a path that isn't symlink-resolved.** This is the one that has no
  signature to defend it. `withinBound` takes only `base` and calls
  `c.resolve()` itself; hand the walk the spelled `c.dir` and `/proj/link ->
  /etc` has `base` among its ancestors, so a directory that is really under
  `/etc` is admitted. Nothing but `candidate` being unexported, `resolve` being
  the sole writer of `physical`, and `NewMatch` being the only way in keeps that
  unexpressible. Skipping the resolve reads as an optimisation and opens
  containment (`TestNewMatch_ConfinedConfigMatchesTheResolvedDirectory` has the
  escaping-link case).
- **Re-keying the memo.** The answer is cached per `base`, not per candidate:
  two confined fragments in one set have different bounds, so a per-candidate
  memo answers the second fragment with the first's answer.
- **Comparing spellings instead of resolved paths, or the reverse.**
- **`configset.confine` no longer treating `cfg.DirUnresolved` as confined** —
  an unfollowable symlink then reads as a file that really lives in
  `envokerc.d`, the one shape that is *not* confined.

## 7. No filesystem discovery, and a bounded walk

`cmd/envoke`'s `locateConfigs` is the only caller of
`config.Locate`/`config.LocateDir`, so it is the enforcement point;
`configset.Load` is handed those two paths and can reach nothing else. A new
caller of either, or a `Load` that computes a path itself, is the finding.
`docs/design-notes.md` records the `/tmp` case that killed the discovery model.

The `envokerc.d` walk is bounded by `maxFragments`/`maxFragmentDepth` and errors
rather than truncating: every file it finds is opened, parsed and hashed on
**every `cd`**, and `$ENVOKERC_D` is honoured verbatim.

## 8. Trust records fail closed

The hash file is written **last** and every write is atomic, so a torn write
leaves an untrusted config rather than a trusted one; both sibling files are
optional on read so upgrading never revokes anyone's trust.
`UnsafeStorePermissions` walks the store *and* its ancestors up to the data home
(`storeChain`) — a `0700` store inside a `0777` parent is a `0777` store.

## Verifying

`dagger call -m .dagger test`, plus `test-shell-<shell>` for anything touching
generated shell code and `fuzz` for the parser or pattern compilation. Never
`go test` on the host.

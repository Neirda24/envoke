# internal/config

Parsing, locating, patterns, permissions.

## Read-once

- **`LoadFile` whenever the same file also feeds a trust decision.** It reads
  once and returns the bytes with the parsed config. Two reads validate one
  version and execute another. Adding a read back is a security regression,
  not a refactor.
- What makes that hold end to end is in `cmd/envoke`: `reviewForApproval` uses
  the bytes `configset.Load` already read and selects the entry by **file
  identity** (`entryFor`), not by path text. Selecting on the spelling
  reintroduces the second read wherever the set's spelling differs — a
  resolved `envokerc.d`, the ordinary dotfiles layout — and keys the trust
  record on a name the hook never looks up.
- `LoadFragmentResolved(path, resolved)` is the fragment loader that runs;
  `configset` is its only production caller. `resolved` must be
  `EvalSymlinks`' answer or `""`. **`""` is not "not a link"** — it sets
  `DirUnresolved`, which is what lets `configset.confine` fail closed. Passing
  a plausible substitute (the absolute path) turns that closed failure open.
- `loadFragment` is unexported and has no production caller, by design: every
  caller outside this package already holds the resolution its dedup is keyed
  on. It stays because deleting it puts `EvalSymlinks` into four test bodies.

## Locating (`locate.go`)

- `Locate`/`LocateDir` mirror each other over `$ENVOKERC`/`$ENVOKERC_D`. Both
  honour the env var verbatim, existing or not.
- `Fragments` **resolves the directory before walking and returns that root**:
  `WalkDir` does not follow a symlink even as the root it was handed, and
  `configset.confine` has to compare against the same resolution or every
  fragment in a symlinked config directory gets confined.
- It sorts on the path **relative to the root** — a plain walk orders `a/b`
  before `a.txt`, and `10-`/`20-` prefixes deciding order is the point of the
  directory.
- It decides an entry's type on **what a stat reports, never `d.Type()`'s
  label**, and `walkableRoot` does the same for the directory itself. Go sets
  `fs.ModeSymlink` for `IO_REPARSE_TAG_SYMLINK` alone, so a Windows junction
  (`mklink /J`, which needs no elevation) arrives as `fs.ModeIrregular`.
  Deciding on the label fails the whole set over a junction inside
  `envokerc.d`, and yields zero fragments and no error for a junctioned
  `envokerc.d` itself.
- **No junction is constructible off Windows**, so
  `TestFragments_FollowsAJunction` covers that end to end on one platform
  only; the portable tests reach the same branch through a symlinked
  directory.

## Bounds

Three, and *where* each sits is the load-bearing part:

| Bound | Where | Why there |
|---|---|---|
| `maxFragments`/`maxFragmentDepth` | the walk | `$ENVOKERC_D` is verbatim, so a stray `/` would walk the filesystem per prompt |
| file type | the walk, **before any open** | `open` on a FIFO with no writer never returns |
| `maxConfigBytes` | `readSource`, on the read | a stat reports 0 for a device and can go stale; in `load` so the central config is covered too |

Residual, stated rather than papered over: `$ENVOKERC` naming a FIFO still
blocks on open. That path the user names themselves, unlike a fragment.

## Patterns (`pattern.go`)

- **The standalone compile before the wrap is a validator, not a warm-up.**
  `^(?:...)$` only anchors a pattern whose groups balance: `)|(` wraps into
  `^(?:)|()$`, a top-level alternation outside the anchors that matches the
  empty string at the start of every path. Found by `FuzzCompilePattern`;
  `)|(` is a seed.
- Compiling twice to get that check is *not* load-bearing, and every pattern
  in the set is compiled on every `cd`. `regexp/syntax.Parse(expanded,
  syntax.Perl)` answers the same question without building a program to throw
  away. Revisit in this order: benchmark one `compilePattern` first, then
  confirm `syntax.Parse` rejects everything `regexp.Compile` does by running
  `FuzzCompilePattern` for real time. A cheaper validator on the path that
  decides which shell code runs has to be provably not a weaker one.
- **Base prepended after `expandEnv`, not before**: a directory can be named
  `$HOME`, and `QuoteMeta` leaves `\$HOME` — still a `$` plus an identifier.

## Fuzzing

`FuzzParse`, `FuzzParseBytesMatchesParse`, `FuzzCompilePattern`. Seeds run
under `dagger call -m .dagger test`; `... fuzz [--fuzz-time=5m]` actually
fuzzes. A new target must also be added to `fuzzTargets` in `.dagger/main.go`
— see the `add-fuzz-target` skill.

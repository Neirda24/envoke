# internal/config

## Parsing, and reading a file exactly once

The parser is hand-rolled and line-oriented; the entry points and the `Block`
shape they produce are right there in the package.

**`LoadFile` is the one to use whenever the same file also feeds a trust
decision** — it reads the file exactly once and returns the source bytes
alongside the parsed config, so `trust.IsTrusted`/`trust.Allow` hash the same
bytes that get parsed and executed. Two separate reads would let the file change
in between, validating one version while executing another.

What guarantees that end to end is that `cmd/envoke` reads a config it reviews
once per command: `reviewForApproval` uses the bytes `configset.Load` already
read, selecting the entry by **file identity** (`entryFor`) rather than by the
path text, and reaches `LoadFile` only for a target the set does not hold at all.
Selecting on the spelling reintroduces the second read wherever the set's own
spelling differs — a resolved `envokerc.d`, which is the ordinary dotfiles
layout — and with it a trust record keyed on a name the hook never looks up.
Adding a read back is a security regression, not a refactor.

**`LoadFragmentResolved(path, resolved)` is the fragment loader that runs**;
`configset` is its only production caller. It is `LoadFile` for an `envokerc.d`
file with one difference: the base a `./` pattern resolves against comes from
the symlink-*resolved* path, because a fragment is often a link to a config
committed inside a project and `./` there has to mean the project rather than
the config directory the link sits in. `Config.Path` stays the link — that is
what the user controls and what the trust record is keyed on.

The resolution is handed in rather than computed here because the caller needs
`filepath.EvalSymlinks`' answer for its own dedup anyway, and that is an
lstat/readlink loop per path component on every `cd`. `resolved` must be that
answer or `""` for "EvalSymlinks refused" — which is *not* the same as "not a
link": `""` makes the base the link's own directory and sets
`Config.DirUnresolved`, which is what lets `configset.confine` fail closed.
Passing a plausible substitute (the absolute path, say) instead of `""` silently
converts that closed failure into an open one.

`loadFragment(path)` is the same call with the resolution done for it, and it is
**unexported on purpose**: it had no production caller and never should have one,
since every caller outside this package holds the resolution already and must
pass *that* answer — the one its dedup is keyed on. Keep it, though. It is the
only place the resolve-then-delegate contract is stated in one call, and deleting
it would put `filepath.EvalSymlinks` into four test bodies.

The parser is hand-rolled, **not a grammar library**: the grammar is a simple
line-oriented format, and a hand-rolled scanner keeps positioned error
messages simple. Revisit only if the grammar grows real nesting/expressions.

Malformed config fails with a positioned `*ParseError{Line, Msg}`, never
silently — **including an undefined `$VAR`**, which used to expand to `""` and
produce a valid pattern that could never match.

## Locating configs and fragments

`Locate()` resolves the *central* config path: `$ENVOKERC` (used verbatim,
even if missing) → `~/.envokerc` if present → `$XDG_CONFIG_HOME/envoke/config`
(or `~/.config/envoke/config`) if present → not found (normal state, not an
error).

`LocateDir`/`Fragments` (`locate.go`) find the fragment directory (mirroring
`Locate`'s order via `$ENVOKERC_D`) and list what is in it. Four things in
`Fragments` are load-bearing:

- It **resolves the directory before walking**, because `filepath.WalkDir`
  does not follow a symlink even when that symlink is the root it was handed —
  walking the link directly finds *nothing* when the config directory is
  itself a link into a dotfiles repo, which is the normal dotfiles layout.
- It **returns that resolved `root`** alongside the paths, because the paths it
  reports are resolved too and a caller comparing them against the directory
  that holds them — `configset`, deciding whether a fragment merely *points*
  into it — has to compare against the same resolution or every fragment in a
  symlinked config directory reads as pointing out of it and gets confined.
  `root` is never empty: it falls back to `dir` as given.
- It **sorts on the path relative to the root**, because a plain walk orders
  `a/b` before `a.txt` and the whole point of the directory is that
  `10-`/`20-` prefixes decide order.
- It decides an entry's type on **what a stat reports, never on the label the
  walk gave it** (`d.Type()`), and `walkableRoot` does the same for the
  directory itself. `fs.ModeSymlink` is not the only way a directory arrives:
  Go sets it for `IO_REPARSE_TAG_SYMLINK` alone, so a Windows **junction** —
  `mklink /J`, the form that needs no elevation, therefore the likely one — is
  `fs.ModeIrregular`. Go back to deciding on the label and a junction *inside*
  `envokerc.d` fails the entire set, where a symlinked directory in the same
  position is silently skipped (`configset.Entry.Err` is per file precisely so
  one entry cannot do that), while a junctioned `envokerc.d` yields **zero
  fragments and no error**, since neither `filepath.EvalSymlinks` nor
  `filepath.WalkDir` follows one and the dotted default name is then skipped as
  an editor dropping. So: a thing that turns out to be a *directory* is skipped
  whatever bit described it, a thing that is neither directory nor regular file
  is still an error naming it, and a root that is not a directory is an error
  about the *directory*. `walkableRoot`'s one `os.Lstat` is the only syscall
  this costs a `cd`; `os.Stat` (which follows a name surrogate) and
  `os.Readlink` (which reads a junction's target) are reached only by a root
  the walk could not have entered by itself. **No junction is constructible off
  Windows**, so `TestFragments_FollowsAJunction` is the one test that covers
  either half end to end and it runs on one platform; the portable tests reach
  the same branch through a symlinked directory.

The walk is **bounded** (`maxFragments`/`maxFragmentDepth`) and errors rather
than truncating: every file it finds is opened, parsed and hashed on every
`cd`, and `$ENVOKERC_D` is honoured verbatim, so an accidental `/` there would
otherwise walk the whole filesystem in front of every shell prompt.
`BenchmarkLoad` in `internal/configset` is where the per-fragment cost is
measurable.

Counting the files says nothing about what is in them: one can be a FIFO or a
device, or a megabyte of text. Two further checks answer that — the file's
**type** and `maxConfigBytes` — and which of them sits where is the load-bearing
part. The **type** check is in the walk because *opening* is
the harmful act: `open` on a FIFO with no writer never returns, so a check after
the open is too late. It costs an ordinary fragment no syscall at all —
`WalkDir`'s `Type()` settles a regular file — and the stat is spent only on the
entries that are not one, where it is also what rules out a directory.
The **size** cap is in `load`/`readSource`, and on the read (`io.LimitReader` of
`maxConfigBytes+1`) rather than on a stat's reported size, because a stat answers
neither case: a character device reports zero bytes and would still read until
memory ran out, and a regular file can grow between being measured and being
read. Reading one byte past the bound is also what keeps the one read one read,
which is what lets the trust decision hash exactly what was parsed. Sitting in
`load` is what covers the *central* config too, since `Locate` returns
`$ENVOKERC` verbatim and never stats it. One residual, stated rather than papered
over: `$ENVOKERC` naming a FIFO still blocks on open — a path the user names
themselves, unlike a fragment, whose content is whatever a project's last commit
says.

## Pattern compilation (`pattern.go`)

Expands a leading `~` and `$VAR`/`${VAR}` as literal (`regexp.QuoteMeta`'d)
substitutions, then anchors the result as `^(?:...)$` — that anchoring is what
makes matching segment-based rather than prefix-based.

**The pattern is compiled once on its own before being wrapped**, and that
throwaway compile is the whole point: `^(?:...)$` only anchors a pattern whose
groups balance, and `)|(` wraps into `^(?:)|()$` — a top-level alternation
outside the anchors, matching the empty string at the start of every path, so
the block fires for every directory on the machine. Requiring the pattern to
compile standalone makes that unexpressible rather than patching the one
spelling (found by `FuzzCompilePattern`; `)|(` is a seed).

The *check* is load-bearing; compiling twice to get it is not. Every pattern in
the set is compiled on every `cd`, so this is the largest single allocator on
that path — `regexp/syntax.Parse(expanded, syntax.Perl)` answers the same
question (does this parse standalone, so that `^(?:…)$` really anchors it?)
without building a program to throw away. Revisit condition, in this order:
benchmark one `compilePattern` first, since every estimate of the win rests on
what a compile actually costs; then confirm `syntax.Parse` rejects everything
`regexp.Compile` does by running `FuzzCompilePattern` for real time rather than
its seed corpus. It is a validator on the path that decides which shell code
runs, so a cheaper one has to be provably not a weaker one.

A leading `./` or `../` (`splitRelative`) resolves against the config file's
own directory — the base `Parse` is given — walking up one level per `../`.
Only a *leading* one counts, exactly as with `~`, so `(/opt|/srv)/x` and `...`
keep their regex meaning. **The base is prepended after `expandEnv`, not
before**: a directory can be named `$HOME`, and `QuoteMeta` leaves that as
`\$HOME` — still a `$` followed by an identifier, which a later env expansion
would substitute, silently retargeting the pattern.

Joining base and remainder is **plain concatenation**, which is why the
remainder keeps its leading `/` (`./src` → `/src`) and why `splitRelative`
trims a trailing separator off the base when the remainder supplies one. A
filesystem root is the case that has one: `filepath.Dir` stops at `/` (at `C:\`
for a Windows volume), which a long enough `../` chain reaches and which a
config living in a root has from the start. Two separators compile fine and
then match nothing — use `os.IsPathSeparator` rather than a `"/\\"` cutset,
since a Unix directory may legitimately end in a backslash.

`expandEnv` is hand-rolled rather than `os.Expand` because patterns are
regexes: `os.Expand` would eat `$?`, `$*`, `$#` and `$0`-`$9` as shell special
variables, so only `$` followed by a real identifier is treated as a reference
and every other `$` stays the regex anchor it almost certainly is.

## Permissions (`permissions.go`)

`UnsafePermissions(path)`/`UnsafeDirPermissions(path)` report whether the
config file, or the directory holding it, is writable by anyone else, which
`cmd/envoke` surfaces as a non-fatal warning — content-hash trust revocation
only protects against *silent* modification, not a multi-user machine where
another local user could edit the file directly. Both delegate to
`internal/fsperm`, and must keep doing so.

## Fuzzing

This package has fuzz targets (`fuzz_test.go`): `FuzzParse`,
`FuzzParseBytesMatchesParse` and `FuzzCompilePattern`. The parser is
hand-rolled by choice and consumes a whole file of unstructured text that
decides what shell code runs, which is what native fuzzing is for. Their seed
corpora run as ordinary tests under `dagger call -m .dagger test`;
`dagger call -m .dagger fuzz [--fuzz-time=5m]` actually fuzzes (not a
`+check` — a fixed-duration run per target would tax every CI run for a low
hit rate). Adding a target means adding it to `fuzzTargets` in
`.dagger/main.go` too; it is listed explicitly so the omission is visible.

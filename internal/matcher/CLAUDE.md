# internal/matcher

`Transitions(from, to)` walks both paths' ancestor chains via `filepath.Dir`
to their common ancestor, returning directories left (deepest-first) and
entered (shallowest-first) — jumping straight from `/a` to `/a/x/y/z` still
fires `/a/x` and `/a/x/y`'s rules.

`Resolve(cfgs, from, to)` runs every block's pattern against the relevant
directories and returns ordered `[]Match`. It takes a **slice** of configs,
ordered outermost-first (what `configset.Configs` produces), and applies
them in that order for enters and reversed for leaves, so a transition unwinds
in the order it was applied. Every `Match` carries its `*config.Config`: with
several files in play, that is the only thing that says whose trust decision
gates it.

`Enters(cfgs, dir)` answers a different question for `envoke reload` — every
enter block matching `dir` or any ancestor, as if arriving from outside the
filesystem. `Resolve` cannot express it: it reports what *changed*, so
`from == to` yields nothing and passing the root as `from` still skips the
root itself.

`candidate.withinBound` is the confinement test for `Local` configs, and
`newMatch` is the only place it is applied. Every `Match` in the codebase is
built there — `NewMatch` is a one-line wrapper — so no caller can assemble one
that skipped the refusal. Keep it that way.

`withinBound` is `Within` first, then `sameDirOrAncestor` on a miss. `Within`
(via `filepath.Rel`, which already knows that two Windows volumes have no
relative path) is lexical, and **deliberately stays that way**: it is the only
form usable on a path that need not exist, which `configset.confine` and
`newMatch`'s own textual fallback both need. But `filepath.Rel` folds component
case on Windows and nowhere else, so lexically the bound would answer
differently per platform for two spellings of one directory — and where the
filesystem is case-insensitive, which is macOS by default, those two spellings
*are* one directory, so a `$PWD` the user typed as `PROJ` would take a confined
fragment's blocks out of service. `sameDirOrAncestor` walks up from the resolved
directory comparing `os.SameFile`, which is why it can only ever admit something
physically inside the bound: an admitted ancestor **is** `base`, whatever either
is called. The answer is memoised per `base` on the candidate — two confined
fragments in one set have different bounds, so the key is `base` and not the
candidate alone.

That soundness rests on the walk starting from the **resolved** path, and the
signature cannot state it. Hand `sameDirOrAncestor` the spelled `c.dir` instead
and `/proj/link -> /etc` has `base` among its ancestors and is admitted —
containment opened to save syscalls. What makes that unexpressible from outside
is only that `candidate` is unexported, `resolve` is the sole writer of
`physical`, and `NewMatch` is the one way in.

**`sameDirOrAncestor` refuses outright on Windows**, and the guard sits in that
function rather than in `withinBound` because what is unsound is the identity
comparison, not one caller's use of it — guarding the caller would leave a
helper that still looks sound for the next caller to reuse, and an identity test
is a thing this codebase has already wanted in more than one place. `os.SameFile`
there compares `(volume serial, file index)`; the file index is unsupported on
ReFS, whose file IDs are 128-bit, is a directory-entry offset on FAT/exFAT, and
is absent from some SMB redirectors, so two distinct directories can compare
equal. The walk is reached
**only** for a directory `Within` has already placed outside `base`, so that
false positive is not a missed match but a **confinement bypass**. Nothing the
walk was written for is lost: on Windows `EvalSymlinks` normalises every
component to its on-disk spelling — long form, so 8.3 names expand — and
`filepath.Rel` folds case, so `Within` answers the two-spellings question by
itself. What *is* given up is a directory the kernel reaches through a device
mapping no path spells (a `subst` drive, a drive mapped to a share), which now
stays outside the bound — fail-closed, the right way round for a bound.
`runtime.GOOS` and not a build-tagged file because the guard's reason is a claim
about `Within`, three functions down the same file, and splitting it out would
put the claim in one file and its subject in another. The tagged files elsewhere
are tagged for the other kind of reason — `internal/fsperm`'s `unsafePerm` has no
single definition to write, and
`matchpath_unix_test.go`/`matchpath_windows_test.go` need fixtures with a `\` in
a filename, legal on Unix and unrepresentable on Windows. Here the walk is
written once and one platform declines to consult it.

`withinBound` still memoises on Windows, so `bounds` goes on meaning "the
identity half was reached and refused" and the pattern-before-bound test reads
the same on every platform.
`TestSameDirOrAncestor_RefusesWithoutComparingIdentityOnWindows` pins the guard
itself and runs everywhere — off Windows its rows are what says the walk still
compares — while `TestNewMatch_ConfinedConfigBoundIsLexicalOnWindows` pins the
answers through the bound and needs a Windows runner, so Dagger never sees it and
the `native` job is the only thing that does.

`newMatch` runs the **pattern before the bound**, and the two orders admit the
same set: both predicates must hold for a `Match`, and confinement is reported
per config (`cmd/envoke`'s `printConfigBound`) and never per refused match, so
nothing observes which of the two refused. What the order buys is cost. `Within`
says yes only *inside* `base`, so it is no fast path for the directories a `cd`
mostly visits, and every one of them would otherwise stat its way to the
filesystem root to learn what the regex already knew; a confined fragment's `./`
patterns carry `cfg.Dir` as a literal prefix (`config.compilePattern`), so
outside the project the pattern refuses with no syscall. What survives to the
bound is a pattern reaching out of its own project — the case the bound exists
for and the one worth the walk.
`TestNewMatch_ConfinedConfigRefusesOnThePatternBeforeTheBound` pins both cases —
a pattern rooted in the config's own directory never reaching the bound, one
reaching outside it doing so — by watching `candidate.bounds`, the only trace the
walk leaves; the memo is therefore load-bearing for the test as much as for the
cost.

Which bound is still owed is carried explicitly (`boundByIdentity`) and **cannot**
be recovered from `against != c.dir`: a project with no symlink above it resolves
to its own spelling, so that inference would silently skip the identity bound for
the commonest confined case. That line is what a refactor must not fold away.

The walk is not extended to the `DirUnresolved`/textual branch: its premise is
that resolution failed, so statting ancestors would be partial resolution
smuggled into a branch whose whole statement is that only the spelling is left.

The **pattern** is not covered by any of this. It runs against the resolved
spelling, so on a case-insensitive filesystem a confined fragment whose bound
would now pass can still fail to match — and since the pattern goes first, for a
`./` pattern the bound is never reached and the block still does not fire.
Canonical casing per component has no portable API off Windows, and
`TestNewMatch_ConfinedConfigPatternKeepsTheSpellingItResolvedTo` pins that
residual rather than leaving it to be rediscovered. The bound still earns its
place for the patterns that do match a differently-cased directory — wildcard and
absolute ones, where `Within` refuses on case and only the walk admits them.

**For a `Local` config the bound *and* the pattern apply to the
symlink-resolved directory**, not the one the shell reported.
`config.LoadFragmentResolved` bases it on the followed link, so such
a config's `Dir` and its patterns' base are both physical while `$PWD` need not
be; comparing or matching one spelling against the other made every block in
the fragment stop firing wherever an ancestor of the project is a symlink — on
macOS, any project under `/var`. Where the directory will not resolve at all,
`newMatch` falls back to comparing it as spelled: the same question of the same
string the bound was always applied to, where refusing outright would cost the
leave blocks of a directory removed underfoot and nothing else unwinds those.

The resolution is per **directory**, not per (directory, config, block):
`collect` builds one `candidate` in its outer loop and every `newMatch` call
shares it, because `filepath.EvalSymlinks` is an lstat/readlink loop per path
component and this runs on every `cd`. A refactor that resolves inside
`newMatch` instead is correct and multiplies that cost by the size of the set.

`Match.Dir` stays **logical** — what the shell reported — because it is used as
a working directory and exported as `ENVOKE_DIR`, and that is the directory the
user is looking at. `Match.Groups` holds the submatches of whichever path
actually matched, so for a confined config they are resolved segments. The
pattern runs **once** and the groups are stored, since `executor.matchVars`
reads them from there rather than re-running the regex on the hot path of every
`cd`.

Patterns are matched against `MatchPath(dir)` (`filepath.ToSlash`), not `dir`:
patterns are regexes and are therefore written with `/`, so on Windows
`filepath.Dir`'s backslashes could never match anything a user would write. It
must stay `filepath.ToSlash` and never a plain `ReplaceAll` — `\` is legal in
a Unix filename, and rewriting it there would corrupt real directory names
(guarded by `matchpath_unix_test.go`).

Test convention: `tp`/`np` build volume-prefixed paths so the same table runs
on Windows.

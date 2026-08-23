# internal/matcher

What a `cd` left and entered, and which blocks match.

- `Transitions(from, to)` splits a change into directories left and entered.
  `Resolve(cfgs, from, to)` matches blocks against them; `Enters(cfgs, dir)`
  answers what `envoke reload` needs and `Resolve` cannot express.
- `cfgs` is a **slice, ordered outermost-first** (what `configset.Configs`
  produces), applied in order for enters and reversed for leaves.

## Confinement — the part to be careful with

- `newMatch` builds **every** `Match` in the codebase; `NewMatch` is a
  one-line wrapper. That is what makes the bound unskippable. Keep it that
  way.
- The soundness precondition — the identity walk starts from the *resolved*
  path — is stated on `candidate.withinBound` and cannot be expressed in a
  signature. What holds it is that `candidate` is unexported, `resolve` is the
  only writer of `physical`, and `NewMatch` is the one way in from outside.
- `sameDirOrAncestor` refuses on Windows, and the guard is on the primitive
  rather than the caller: the identity comparison is what is unsound there, so
  guarding the caller would leave a helper that still looks reusable.
  `runtime.GOOS` rather than a build-tagged file because the guard's reason is
  a claim about `Within`, three functions down the same file.
- `boundByIdentity` cannot be recovered from `against != c.dir` — a project
  with no symlink above it resolves to its own spelling. That line is what a
  refactor must not fold away.
- `bounds` is memoised per `base`, not per candidate: two confined fragments
  in one set have different bounds. It is also **load-bearing for a test** —
  it is the only trace the walk leaves.
- Resolution is per **directory**: `collect` builds one `candidate` in its
  outer loop. Resolving inside `newMatch` instead is correct and multiplies an
  lstat/readlink loop per path component by the size of the set.

## Which test pins what

| Test | Runs |
|---|---|
| `TestSameDirOrAncestor_RefusesWithoutComparingIdentityOnWindows` | everywhere — off Windows its rows are what says the walk still compares |
| `TestNewMatch_ConfinedConfigBoundIsLexicalOnWindows` | Windows runner only; Dagger never sees it |
| `TestNewMatch_ConfinedConfigRefusesOnThePatternBeforeTheBound` | everywhere, by watching `candidate.bounds` |
| `TestNewMatch_ConfinedConfigPatternKeepsTheSpellingItResolvedTo` | pins the known residual: a `./` pattern is refused on case before the bound is consulted |

Test convention: `tp`/`np` build volume-prefixed paths so one table runs on
Windows.

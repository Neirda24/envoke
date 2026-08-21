# internal/configset

The one place that decides *which* configs envoke acts on.
`Load(globalPath, fragmentDir)` returns the central config (when there is one)
followed by every fragment, in `config.Fragments` order. The set does **not**
depend on the transition: every file in it lives in a directory the user owns,
so there is nothing to discover per `cd`.

Each `Entry` carries its content bytes alongside the parsed config, for the
same read-once reason `config.LoadFile` does.

**`Entry.Err` is per file**: one unparseable fragment — possibly one a
`git pull` just rewrote — must not disable the whole set, so it is reported
and skipped rather than returned wholesale.

Three things live here and nowhere else:

- The **confinement decision** (`confine`), set here rather than in `config`
  because only the assembler knows the root — and the root it compares against
  is the one `config.Fragments` returns, the resolved one the walk used, not
  `fragmentDir` as given: a config directory symlinked into a dotfiles
  repository would otherwise make every fragment inside it look like a file
  pointing *out* of it, confining the whole set. It also treats
  `cfg.DirUnresolved` as confined, because a fragment whose symlink could not
  be followed reports the link's own directory and would otherwise read as a
  file that really lives in `envokerc.d`, the one shape that is *not*
  confined. It compares with `matcher.Within`, the lexical form, **on purpose**:
  both sides are paths `Fragments` and `LoadFragmentResolved` already resolved,
  so the identity walk `matcher` uses when *applying* the bound would be
  syscalls spent on an answer it cannot change.
- The **one symlink resolution per path** (`identify`), shared by the two
  things that need it: the dedup key, so a fragment linking back at
  `~/.envokerc` counts once instead of firing every block twice per `cd`, and
  the `resolved` argument to `config.LoadFragmentResolved`. Sharing it is the
  point — `filepath.EvalSymlinks` is an lstat/readlink loop per path component
  and this runs for every fragment on every `cd`. The two answers are not
  interchangeable: the key falls back to the absolute path so a dedup miss is
  never a wrong dedup, while `resolved` must stay `""` when the link would not
  follow, because that is what `config` turns into `DirUnresolved` and
  `confine` reads as confined.
- `Decide(entry)`, which states the trust rule once.

`BenchmarkLoad` is where the per-fragment cost of the `envokerc.d` walk is
measurable.

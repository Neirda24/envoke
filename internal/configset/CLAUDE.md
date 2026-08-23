# internal/configset

Decides *which* configs envoke acts on, and loads each exactly once. The set
does not depend on the transition — every file in it lives in a directory the
user owns, so there is nothing to discover per `cd`.

Three things live here and nowhere else:

- **`confine`** — the confinement decision. Here rather than in `config`
  because only the assembler knows the root, and it must be the *resolved*
  root `config.Fragments` returns: a config directory symlinked into a
  dotfiles repository would otherwise confine every fragment in it. It
  compares with `matcher.Within` (lexical) on purpose — both sides are already
  resolved, so `matcher`'s identity walk would be syscalls spent on an answer
  it cannot change.
- **`identify`** — one `EvalSymlinks` per path, shared by the dedup key and by
  `LoadFragmentResolved`'s `resolved`. The two answers are **not**
  interchangeable: the key falls back to the absolute path so a dedup miss is
  never a wrong dedup, while `resolved` must stay `""` when the link won't
  follow, because that is what becomes `DirUnresolved` and reads as confined.
- **`Decide`** — the trust rule, stated once.

`BenchmarkLoad` is where the per-fragment cost of the `envokerc.d` walk is
measurable.

# internal/envoke

`Transition(ctx, entries, from, to)` — the subprocess core loop behind
`envoke exec`. Not used by the shell hook or `debug`.

- **Takes the loaded set, not `[]*config.Config`, and makes every trust
  decision itself.** It is the only thing in the codebase that spawns a shell
  from config, so the gate lives where no caller can skip it. Don't "simplify"
  the signature.
- **Resolve before decide.** `decide` only consults configs some block of
  which matched, so a fragment with nothing to say about this transition
  produces no warning. Deciding first would report every untrusted config in
  the set on every `cd`.
- One untrusted or unreadable config does not stop the others. All-or-nothing
  was right with a single config and is wrong with a set — a fragment a `git
  pull` just rewrote would disable everything in CI.
- The same "decide once per config, report once" shape exists in
  `cmd/envoke`'s `runnable`/`mayRun`, reporting to stderr instead of joining
  errors. Changing one means checking the other.

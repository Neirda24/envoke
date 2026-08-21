# internal/envoke

`Transition(ctx, entries, from, to)` is the subprocess-based core loop behind
`envoke exec`: resolve, trust-check **the configs that matched**, run all
leaves, then all enters via `executor.Run`, stopping at the first failing block
(no partial-transition unwind — see the enter/leave independence principle).

That order is the interesting part and not an accident: `decide` only consults
configs some block of which actually matched, so a fragment with nothing to say
about this transition produces no warning — it isn't being skipped, it doesn't
apply. Deciding before resolving would report every untrusted config in the set
on every `cd`.

It takes the **loaded set** rather than parsed configs, and makes every trust
decision itself: it is the only thing in the codebase that spawns a shell from
config, so the gate lives where no caller can skip it. Don't "simplify" it
into accepting `[]*config.Config`.

A config that is untrusted or unreadable **does not stop the others** — it is
collected and returned via `errors.Join` (so `errors.Is` still finds
`ErrUntrusted`) after the trusted blocks have run. All-or-nothing was right
when there was one config and is wrong now: one fragment a `git pull` just
rewrote would otherwise disable the whole set in CI.

Used for non-interactive automation (scripts/CI), not the shell hook or
`envoke debug` — side effects stay in the subprocess, which is what
[docs/non-interactive.md](../../docs/non-interactive.md) exists to spell out.

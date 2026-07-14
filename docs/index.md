# envoke

`envoke` runs shell scripts when you `cd` into or out of a directory.

It's a spiritual rewrite of [ondir](https://github.com/alecthomas/ondir) in Go: same idea (per-directory `enter`/`leave` hooks matched by path), rewritten to fix a decade of unresolved bugs and to reach every major shell and OS with a single static binary.

!!! warning "Status: early development"
    The core matching engine, shell hooks for bash/zsh/fish/tcsh/PowerShell, the `envoke allow` trust mechanism, and `envoke debug` dry-run diagnostics all exist and are tested — a `cd` into a trusted, matching directory really does run its `enter` block in your shell today. bash, zsh, and tcsh have been verified end to end against real interpreters; fish and PowerShell are implemented against documented behavior but not yet run against real `fish`/`pwsh` interpreters. Still missing: packaging/releases. See the [project status on GitHub](https://github.com/Neirda24/envoke/blob/main/CLAUDE.md#status) for the current MVP scope order.

## Why not just use ondir / direnv?

- **ondir** nails the enter/leave-with-regex-matching model but is effectively unmaintained (last push 2023, maintainer considers it feature-complete), uses POSIX regex vulnerable to catastrophic backtracking (hangs), and has several long-standing correctness bugs (see [Design Notes](design-notes.md)).
- **direnv** is mature and trusted, but it's centered on a single `.envrc` per directory with load/unload semantics — it doesn't do regex-based matching across a directory tree, and isn't aimed at the "run an arbitrary script on enter/leave of *any* matching path" use case.

`envoke` targets the ondir model — path-pattern-driven hooks, not one-file-per-directory — with direnv-grade reliability and trust.

## Where to go next

- [Getting Started](getting-started.md) — install, hook into your shell, write your first config.
- [Configuration](configuration.md) — the config file syntax, path patterns, and what a matched script sees.
- [Trust Model](trust.md) — why `envoke allow` exists and how it works.
- [Debugging](debugging.md) — inspect what would fire before trusting a config.
- [Design Notes](design-notes.md) — the ondir bugs this project exists to fix.

## License

[MIT](https://github.com/Neirda24/envoke/blob/main/LICENSE)

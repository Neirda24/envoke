# envoke

`envoke` runs a shell script automatically when you `cd` into — or out of —
a directory, matched by path pattern. One static binary, every major shell,
nothing runs until you approve it.

It's a spiritual rewrite of [ondir](https://github.com/alecthomas/ondir) in
Go: same idea (per-directory `enter`/`leave` hooks matched by path). ondir
is feature-complete by its own maintainer's account and still works, but
it's had no release in years; envoke picks up the same model, addresses a
few specific long-standing rough edges, and reaches every major shell and
OS with a single static binary.

!!! warning "Status: early development"
    The matching engine, shell hooks for bash/zsh/fish/tcsh/PowerShell, the `envoke allow` trust mechanism, and `envoke debug` dry-run diagnostics all exist and are tested end-to-end against real interpreters — a `cd` into a trusted, matching directory really does run its `enter` block in your shell today. GitHub Releases, a Homebrew tap, a Scoop bucket, and `.deb`/`.rpm` packages are all live. See the [README's Status section](https://github.com/Neirda24/envoke/blob/main/README.md#status) for exact scope.

## Why not just use ondir / direnv?

`envoke` targets the same model as [ondir](https://github.com/alecthomas/ondir) —
path-pattern-driven `enter`/`leave` hooks, not one-file-per-directory like
[direnv](https://direnv.net/). ondir is feature-complete by its own
maintainer's account and still does the job, but it's had no release in
years; envoke picks up the same model on a few specific points (regex
engine choice, path matching semantics, a trust/approval step) and extends
shell support beyond bash/zsh.

See [Design Notes](design-notes.md) for the full, point-by-point comparison.

## Where to go next

- [Getting Started](getting-started.md) — install, hook into your shell, write your first config.
- [Configuration](configuration.md) — the config file syntax, path patterns, and what a matched script sees.
- [Trust Model](trust.md) — why `envoke allow` exists and how it works.
- [Debugging](debugging.md) — inspect what would fire before trusting a config.
- [Uninstalling](uninstall.md) — remove the shell hook and the binary, per install method.
- [Design Notes](design-notes.md) — the specific points where envoke departs from ondir.

## License

[MIT](https://github.com/Neirda24/envoke/blob/main/LICENSE)

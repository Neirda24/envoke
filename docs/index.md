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
    Everything documented here exists and is tested end-to-end against real interpreters — a `cd` into a trusted, matching directory really does run its `enter` block in your shell today. That covers the matching engine, shell hooks for bash/zsh/fish/tcsh/PowerShell, the `envokerc.d` fragment directory with relative patterns and symlinked project configs, the trust mechanism (`allow`, `revoke`, `list`, `prune`), the `disable`/`enable` off switch, `reload`, non-interactive `exec`, and `debug` dry-run diagnostics. GitHub Releases, a Homebrew tap, a Scoop bucket, and `.deb`/`.rpm` packages are all live, each release carrying a per-archive SBOM alongside cosign-signed checksums. The [Reference](reference.md) is the complete list of commands, flags, variables and exit codes.

## Why not just use ondir / direnv?

`envoke` targets the same model as [ondir](https://github.com/alecthomas/ondir) —
path-pattern-driven `enter`/`leave` hooks, not one file per directory like
[direnv](https://direnv.net/). ondir is feature-complete by its own maintainer's
account and still does the job, but it's had no release in years; envoke picks
up the same model on a few specific points (regex engine choice, path matching
semantics, a trust/approval step) and extends shell support beyond bash/zsh.

- [envoke vs. direnv](vs-direnv.md) — path patterns covering whole trees
  instead of an `.envrc` per directory, arbitrary shell scripts instead of
  environment variables only, and when to use which (or both).
- [Design Notes](design-notes.md) — the full, point-by-point comparison with
  ondir.

## Where to go next

- [envoke vs. direnv](vs-direnv.md) — how the two differ, and which one fits your case.
- [Getting Started](getting-started.md) — install, hook into your shell, write your first config.
- [Configuration](configuration.md) — the config file syntax, path patterns, and what a matched script sees.
- [Recipes](recipes.md) — worked enter/leave pairs for virtualenvs, kubectl contexts, cloud profiles, generated shell functions and `.env` files, unwinding included.
- [Trust Model](trust.md) — why `envoke allow` exists and how it works.
- [Non-interactive Use](non-interactive.md) — running blocks from scripts, Makefiles and CI.
- [Debugging](debugging.md) — inspect what would fire, switch envoke off, reload a config in place.
- [Troubleshooting](troubleshooting.md) — why a block didn't fire, in the order the causes actually come up.
- [Reference](reference.md) — every command, flag, environment variable, file and exit code.
- [Uninstalling](uninstall.md) — remove the shell hook and the binary, per install method.
- [Design Notes](design-notes.md) — the specific points where envoke departs from ondir.

## License

[MIT](https://github.com/Neirda24/envoke/blob/main/LICENSE)

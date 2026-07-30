# Getting Started

## Installation

```sh
brew install neirda24/tap/envoke                          # Homebrew (macOS/Linux)
scoop bucket add neirda24 https://github.com/Neirda24/scoop-bucket && scoop install envoke  # Scoop (Windows)
go install github.com/Neirda24/envoke/cmd/envoke@latest   # Go toolchain
```

Or download a prebuilt binary, `.deb`, or `.rpm` from
[GitHub Releases](https://github.com/Neirda24/envoke/releases)
for macOS/Linux/Windows (amd64/arm64), built with [goreleaser](https://goreleaser.com/).
Each release's `checksums.txt` is signed keylessly with
[cosign](https://docs.sigstore.dev/cosign/) — see the release notes for the
exact verification command.

## Shell integration

Add one of these to your shell's rc file, matching your shell:

```sh
# bash/zsh
eval "$(envoke shell-init bash)"   # or zsh
```

`envoke shell-init` with no argument guesses the shell from `$SHELL`, which
is usually what you want when adding the line to your own rc file. It never
falls back to a default it isn't sure about: an unrecognised `$SHELL` is an
error telling you to name the shell, rather than a bash hook quietly written
into a fish config.

```fish
# fish
envoke shell-init fish | source
```

```tcsh
# tcsh — `eval "$(...)"`-style substitution splits multi-line output on
# newlines in tcsh, so this pipes into source instead.
envoke shell-init tcsh | source /dev/stdin
```

```powershell
# PowerShell — Out-String joins the (possibly multi-line) output into one
# string before Invoke-Expression evaluates it.
& envoke shell-init powershell | Out-String | Invoke-Expression
```

Restart your shell (or re-source your rc file) after adding the hook.

### Tab completion

Optional, and separate from the hook above:

```sh
envoke completion bash >> ~/.bashrc          # or: source <(envoke completion bash)
envoke completion zsh  > "${fpath[1]}/_envoke"   # zsh, with compinit already set up
envoke completion fish > ~/.config/fish/completions/envoke.fish
```

With no argument it guesses from `$SHELL`, like `shell-init`. Only bash, zsh
and fish are covered — tcsh and PowerShell would need a half-working script
to be worth shipping, so they get an explicit error instead.

### A note on Windows

Windows binaries are published (and a Scoop manifest with them), and the
PowerShell hook is generated the same way as the others. Two things are
worth knowing before you rely on it:

- **Write patterns with `/`, not `\`.** Patterns are regexes, where `\` is
  the escape character, so `C:\Users\you` would not mean what it looks like.
  envoke normalizes the directories it tests to forward slashes, so
  `C:/Users/you/Projects/([^/]+)` is the form that works. `ENVOKE_DIR` is
  still handed to your script in native `C:\...` form; the `ENVOKE_MATCH*`
  capture variables come from the normalized path.
- **Matching is case-sensitive**, while Windows paths generally are not. If
  that matters for a rule, write the pattern case-insensitively with
  `(?i)`.
- [`envoke exec`](non-interactive.md) runs blocks through `sh -c` and so
  needs a POSIX shell on `PATH` (Git Bash, WSL, MSYS2). The shell hook
  itself has no such requirement.

Windows is tested on a real `windows-latest` CI runner, not just
cross-compiled: the matching engine, config parsing and the trust store all
run their full suites there. The end-to-end tests that drive bash/zsh/fish/
tcsh still run on Linux and macOS only, since those shells aren't what a
Windows user is running anyway. Please
[open an issue](https://github.com/Neirda24/envoke/issues) if something
behaves differently than documented here.

## Checking your version

```sh
$ envoke version
envoke 0.1.1-SNAPSHOT-e4b0eb5 (commit e4b0eb554c40b05a566ae1e01a427dc08e12ac47, built 2026-07-16T13:28:55Z)
go1.26.4 darwin/arm64
```

Useful to have on hand for bug reports — it prints the real version, commit, and build date for a released binary, plus the Go toolchain and OS/arch it was built with. A binary built locally without goreleaser's release `ldflags` (e.g. a plain `go build`) prints `envoke dev (commit unknown, built unknown)` instead, since those values are only injected at release build time.

## Your first config

Create `~/.envokerc`:

```
enter ~/Projects/([^/]+)
    source "$ENVOKE_DIR/venv/bin/activate"

leave ~/Projects/([^/]+)
    deactivate
```

Then approve it — envoke never runs a new or edited config unconditionally. `envoke allow` shows you the blocks it's about to trust and asks for confirmation before recording anything:

```sh
$ envoke allow
envoke: about to trust /home/you/.envokerc -- review each block below before confirming:

  enter ~/Projects/([^/]+) (line 1)
    source "$ENVOKE_DIR/venv/bin/activate"

  leave ~/Projects/([^/]+) (line 4)
    deactivate

envoke: trust and run these blocks on every matching cd? [y/N] y
envoke: trusted /home/you/.envokerc
```

`cd` into a matching directory and the `enter` block runs in your current shell; `cd` back out and `leave` runs. For non-interactive setups (dotfiles bootstrap scripts, provisioning), pass `--yes`/`-y` to skip the confirmation prompt: `envoke allow --yes`. See [Trust Model](trust.md) for the full confirm/diff/`--yes` behavior, and [Debugging](debugging.md) for inspecting matches before trusting a config.

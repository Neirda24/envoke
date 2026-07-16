# Getting Started

## Installation

```sh
brew install neirda24/tap/envoke                          # Homebrew (macOS/Linux)
go install github.com/Neirda24/envoke/cmd/envoke@latest   # Go toolchain
```

Or download a prebuilt binary from [GitHub Releases](https://github.com/Neirda24/envoke/releases)
for macOS/Linux/Windows (amd64/arm64), built with [goreleaser](https://goreleaser.com/).
Each release's `checksums.txt` is signed keylessly with
[cosign](https://docs.sigstore.dev/cosign/) — see the release notes for the
exact verification command.

Not yet published: a **Scoop** bucket (Windows) and `.deb`/`.rpm` **Linux
packages** via [nfpm](https://nfpm.goreleaser.com/).

## Shell integration

Add one of these to your shell's rc file, matching your shell:

```sh
# bash/zsh
eval "$(envoke shell-init bash)"   # or zsh
```

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
    source venv/bin/activate

leave ~/Projects/([^/]+)
    deactivate
```

Then approve it — envoke never runs a new or edited config unconditionally. `envoke allow` shows you the blocks it's about to trust and asks for confirmation before recording anything:

```sh
$ envoke allow
envoke: about to trust /home/you/.envokerc -- review each block below before confirming:

  enter ~/Projects/([^/]+) (line 1)
    source venv/bin/activate

  leave ~/Projects/([^/]+) (line 4)
    deactivate

envoke: trust and run these blocks on every matching cd? [y/N] y
envoke: trusted /home/you/.envokerc
```

`cd` into a matching directory and the `enter` block runs in your current shell; `cd` back out and `leave` runs. For non-interactive setups (dotfiles bootstrap scripts, provisioning), pass `--yes`/`-y` to skip the confirmation prompt: `envoke allow --yes`. See [Trust Model](trust.md) for the full confirm/diff/`--yes` behavior, and [Debugging](debugging.md) for inspecting matches before trusting a config.

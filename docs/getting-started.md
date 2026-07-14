# Getting Started

## Installation

Not yet published. Planned distribution, once v1 is buildable:

- **GitHub Releases** — prebuilt static binaries for macOS/Linux/Windows (amd64 + arm64), built with [goreleaser](https://goreleaser.com/).
- **Homebrew** (macOS/Linux) — `brew install neirda24/tap/envoke`.
- **Scoop** (Windows) — `scoop bucket add neirda24 https://github.com/Neirda24/scoop-bucket && scoop install envoke`.
- **Linux packages** — `.deb`/`.rpm` built via [nfpm](https://nfpm.goreleaser.com/) as part of the goreleaser pipeline.
- **Go toolchain** — `go install github.com/Neirda24/envoke@latest` for Go users who prefer building from source.

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

## Your first config

Create `~/.envokerc`:

```
enter ~/Projects/([^/]+)
    source venv/bin/activate

leave ~/Projects/([^/]+)
    deactivate
```

Then approve it — envoke never runs a new or edited config unconditionally:

```sh
envoke allow
```

`cd` into a matching directory and the `enter` block runs in your current shell; `cd` back out and `leave` runs. See [Trust Model](trust.md) for why the `allow` step exists, and [Debugging](debugging.md) for inspecting matches before trusting a config.

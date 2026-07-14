# envoke

`envoke` runs shell scripts when you `cd` into or out of a directory.

It's a spiritual rewrite of [ondir](https://github.com/alecthomas/ondir) in Go: same idea (per-directory `enter`/`leave` hooks matched by path), rewritten to fix a decade of unresolved bugs and to reach every major shell and OS with a single static binary.

> **Status**: early design phase. No code yet — this README describes the target shape of v1.

## Why not just use ondir / direnv?

- **ondir** nails the enter/leave-with-regex-matching model but is effectively unmaintained (last push 2023, maintainer considers it feature-complete), uses POSIX regex vulnerable to catastrophic backtracking (hangs), and has several long-standing correctness bugs (see [Design notes](#design-notes)).
- **direnv** is mature and trusted, but it's centered on a single `.envrc` per directory with load/unload semantics — it doesn't do regex-based matching across a directory tree, and isn't aimed at the "run an arbitrary script on enter/leave of *any* matching path" use case.

`envoke` targets the ondir model — path-pattern-driven hooks, not one-file-per-directory — with direnv-grade reliability and trust.

## Core concept

A config file (`~/.envokerc` by default) declares blocks:

```
enter ~/Projects/([^/]+)
    source venv/bin/activate

leave ~/Projects/([^/]+)
    deactivate
```

- `enter <path-pattern>`: runs when you `cd` into a directory matching the pattern (matched with Go's `regexp`/RE2 — linear time, no catastrophic backtracking).
- `leave <path-pattern>`: runs when you `cd` out of a directory matching the pattern.
- Moving through intermediate directories still triggers their rules, even if you don't stop there.
- Enter and leave are **independent, explicit blocks** — envoke does not snapshot state on enter and auto-restore it on leave. If entering exports a variable or activates a venv, the matching `leave` block is responsible for explicitly unwinding it. This keeps behavior predictable and scriptable instead of relying on magic state-diffing.

## Use cases

- **Python (or any) virtualenv activation** — `enter` sources `venv/bin/activate`, `leave` runs `deactivate`.
- **Per-directory umask** — `enter` tightens `umask` for a sensitive tree, `leave` restores the default explicitly.
- **Project-scoped environment variables** — `enter` exports API keys/endpoints/feature flags for a project, `leave` unsets them.
- **Cloud/k8s context switching** — `enter` runs `kubectl config use-context ...` / sets `AWS_PROFILE` / activates a `gcloud` configuration for a directory tree; `leave` switches back explicitly. Reliable because path matching is segment-based, not raw-prefix (ondir's bug #5: `/home/foo` no longer falsely matches `/home/foobar`).
- **Capture groups exposed to scripts** — `enter ~/Projects/([^/]+)` can expose the matched project name to the script (e.g. via `ENVOKE_MATCH_1`), so one generic block handles many project directories instead of duplicating config per project.
- **On-the-fly shell function generation** — `enter` generates/sources a `functions.sh` tailored to the current project (e.g. from Docker Compose labels), `leave` cleans it up. Per-rule enter/leave state tracking avoids regenerating on every deeper `cd` within an already-active tree.
- **Dev toolchain version switching** — `enter` swaps Node/Ruby/Go versions for a project tree without depending on each tool's own per-directory convention; one mechanism for all of them.

## Trust model

Any config that runs arbitrary shell code on `cd` needs an opt-in step before it executes for the first time, direnv-style (`direnv allow`). ondir has no such mechanism — any `~/.ondirrc` runs unconditionally. `envoke` requires an explicit `envoke allow` (or equivalent) before a new or changed config block is executed.

## Diagnostics

`envoke debug <from> <to>` prints which `enter`/`leave` blocks would fire for a given directory transition, without running them — for developing a config without surprises.

## Design notes

Bugs in ondir that motivate specific design choices here:

| ondir issue | envoke fix |
|---|---|
| Catastrophic backtracking hangs (glibc POSIX regex) | Go's `regexp` (RE2), linear-time matching guaranteed |
| Basename-prefix false positives (`/home/foo` matches `/home/foobar`) | Path-segment matching, not raw string prefix |
| No `~` expansion in config paths | Explicit `~`/env-var expansion |
| Capture groups not exposed to scripts | Matched path and capture groups exposed as env vars |
| No `ONDIRRC` var / no XDG support | `ENVOKERC` env var + `$XDG_CONFIG_HOME/envoke/config` |
| Hand-maintained shell scripts per shell (bash/zsh/tcsh/fish) | Single binary generates hooks: `envoke shell-init bash\|zsh\|fish\|tcsh\|powershell` |
| zsh integration overrides `cd` directly | Proper `chpwd_functions` (zsh), `--on-variable PWD` (fish) |
| No config trust/opt-in | `envoke allow` before executing new/changed config |

## Installation

Not yet published. Planned distribution, once v1 is buildable:

- **GitHub Releases** — prebuilt static binaries for macOS/Linux/Windows (amd64 + arm64), built with [goreleaser](https://goreleaser.com/).
- **Homebrew** (macOS/Linux) — `brew install neirda24/tap/envoke`.
- **Scoop** (Windows) — `scoop bucket add neirda24 https://github.com/Neirda24/scoop-bucket && scoop install envoke`.
- **Linux packages** — `.deb`/`.rpm` built via [nfpm](https://nfpm.goreleaser.com/) as part of the goreleaser pipeline.
- **Universal install script** — `curl -sSL https://.../install.sh | sh`, detects OS/arch and pulls the matching release binary.
- **Go toolchain** — `go install github.com/Neirda24/envoke@latest` for Go users who prefer building from source.

`winget` (Windows) is a candidate follow-up once the project has enough release history to meet its submission requirements.

## Shell integration

Add to your shell rc file:

```sh
# bash/zsh
eval "$(envoke shell-init bash)"   # or zsh
```

```fish
# fish
envoke shell-init fish | source
```

## License

TBD.

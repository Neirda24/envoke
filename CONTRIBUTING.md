# Contributing to envoke

Thanks for your interest in contributing. `envoke` is early-stage — see
[CLAUDE.md](https://github.com/Neirda24/envoke/blob/main/CLAUDE.md) for the
current scope and architecture.

## Before you start

- Read the [README](https://github.com/Neirda24/envoke/blob/main/README.md)
  for the pitch and [docs/design-notes.md](https://github.com/Neirda24/envoke/blob/main/docs/design-notes.md)
  for the specific points this project departs from ondir on.
- Read [CLAUDE.md](https://github.com/Neirda24/envoke/blob/main/CLAUDE.md)
  for the non-negotiable design principles (RE2-only matching, path-segment
  matching, no implicit enter/leave undo, trust-before-execution) and the
  current status of what's implemented.
- Don't start work on something that depends on a capability that isn't
  built and tested yet — check CLAUDE.md's Status section first, and open
  an issue if you want to work on something out of that order.

## How to help

- **Bug reports and small fixes** are always welcome without prior
  discussion — see [Reporting bugs](#reporting-bugs) below.
- **Anything bigger** (a new capability, a new shell, a new packaging
  target) — open an issue first so scope and approach can be agreed before
  you write code. Check CLAUDE.md's Status section to see what's already
  in progress.
- **Docs** are a good place to start if you're new to the codebase: `docs/`
  is a normal MkDocs site, no Go toolchain required to preview it.

## Development setup

Requires Go 1.23+. No other tooling — the project has zero non-stdlib
dependencies by design; keep it that way unless there's a strong reason to
add one.

## Verifying your change

**Never run `gofmt`, `go vet`, `go build`, `go test`, or `golangci-lint`
directly on your machine** — this project verifies everything through
[Dagger](https://dagger.io) instead, so every check runs on the same pinned
container images CI uses, not whatever happens to be installed locally
(this is what caught real shell-hook bugs that only reproduced on Linux, not
macOS — see CLAUDE.md's `internal/shellinit` notes).

Requires the [`dagger` CLI](https://docs.dagger.io/getting-started/installation)
and a container runtime (Docker or similar).

Pick the command that matches what you changed:

| You changed... | Run this |
|---|---|
| Go code anywhere in `internal/`/`cmd/` | `dagger check -m .dagger` (runs everything below) |
| Just want a quick loop while iterating | `dagger call -m .dagger fmt`, `vet`, `build`, or `test` individually |
| `internal/shellinit` (shell hook generation) | `dagger call -m .dagger test-shell-bash` (swap in `zsh`/`fish`/`tcsh`/`powershell`) — each spins up a container with only that one interpreter installed, so nothing silently skips for lack of a binary |
| `.github/workflows/*.yml` | `dagger -m .dagger call zizmor` and `dagger -m .dagger call actions-up` (see below) |
| `docs/` or `mkdocs.yml` | `mkdocs serve` or `dagger -m ./.dagger call docs up --ports 8000:8000` (see [Previewing documentation](#previewing-documentation-changes)) |
| `.dagger/main.go` itself | No dedicated check yet — build it manually (`docker run` against the pinned Go image, or `dagger develop -m .dagger` to confirm it still generates) |

Before opening a PR, run the full suite:

```sh
dagger check -m .dagger
```

This mirrors `gofmt -l .`, `go vet ./...`, `go build ./...`,
`go test ./... -race`, and `golangci-lint`, plus the five `test-shell-*`
checks. `.github/workflows/ci.yml` runs the exact same command on every
push/PR to `main` and on a daily schedule — a clean local run just gets you
the same feedback sooner.

### Keeping GitHub Actions workflows secure and current

Two checks audit `.github/workflows/*.yml` itself, rather than Go code:

- [`zizmor`](https://docs.zizmor.sh/) — static security analysis (unpinned
  `uses:` refs, overly broad `permissions:`, credential persistence, etc.)
- [`actions-up`](https://github.com/azat-io/actions-up) — checks every
  `uses:` is pinned to a commit SHA and at its latest compatible version

Both benefit from an authenticated GitHub API call (the unauthenticated rate
limit is 60/hr) via an optional `--gh-auth-token` flag on the module,
reusing your own `gh` CLI session rather than minting a PAT:

```sh
dagger -m .dagger call --gh-auth-token='cmd://gh auth token' zizmor
dagger -m .dagger call --gh-auth-token='cmd://gh auth token' actions-up
```

When either check fails, fix what's auto-fixable in one pass and inspect the
diff before applying it:

```sh
dagger -m .dagger call autofix export --path=.
```

`autofix` runs zizmor's `--fix=all` first, then `actions-up --yes` against
the zizmor-fixed tree, and returns the combined diff — it does not overwrite
anything until you `export` it. Re-run `zizmor`/`actions-up` afterward: some
findings (e.g. `permissions:` set at the workflow level instead of scoped to
the one job that needs it) require a manual restructure neither tool does
for you.

## Previewing documentation changes

The [docs site](https://neirda24.github.io/envoke/) lives under `docs/` +
`mkdocs.yml`. Preview it locally before opening a PR that touches either:

```sh
pip install -r docs/requirements.txt && mkdocs serve   # localhost:8000, live-reload
```

Or, without installing Python, via the same Dagger CLI as above:

```sh
dagger -m ./.dagger call docs up --ports 8000:8000
```

## Code conventions

- Table-driven tests via subtests, named `Test<Func>_<Scenario>`.
- New bug-fix behavior should include a regression test, ideally tied to the
  ondir bug it addresses if applicable (see `docs/design-notes.md`).
- Prefer editing existing files over adding new abstractions; no speculative
  generalization ahead of an actual second use case.
- Static, dependency-free binaries — no cgo, no new imports unless the
  change genuinely requires one (discuss in the issue/PR first).

## Submitting changes

1. Open an issue first for anything beyond a small fix, so scope and
   approach can be agreed before you write code.
2. Keep PRs focused on a single capability or bug fix — avoid bundling
   unrelated cleanup.
3. Run `dagger check -m .dagger` (see [Verifying your change](#verifying-your-change))
   and confirm everything passes.
4. Describe *why* in the PR description, not just what changed — the "why"
   is what reviewers and future contributors need most.

## Reporting bugs

Open a GitHub issue with:

- The config block that triggers the problem (redact anything sensitive).
- The `from`/`to` paths involved.
- Expected vs. actual behavior.

Security vulnerabilities should go through [SECURITY.md](https://github.com/Neirda24/envoke/blob/main/SECURITY.md)
instead of a public issue.

## License

By contributing, you agree that your contributions will be licensed under
the project's [MIT License](LICENSE).

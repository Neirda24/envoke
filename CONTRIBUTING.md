# Contributing to envoke

Thanks for your interest in contributing. `envoke` is early-stage — see the
README's [Status](https://github.com/Neirda24/envoke/blob/main/README.md#status)
section for what's implemented and tested.

## Before you start

- Read the [README](https://github.com/Neirda24/envoke/blob/main/README.md)
  for the pitch and [docs/design-notes.md](https://github.com/Neirda24/envoke/blob/main/docs/design-notes.md)
  for the non-negotiable design principles (RE2-only matching, path-segment
  matching, no implicit enter/leave undo, trust-before-execution) and the
  specific points this project departs from ondir on.
- Proposing something that doesn't exist yet is exactly what issues are
  for — see [How to help](#how-to-help) below. The thing to watch for is
  *implementing* out of the codebase's own build order (matching engine,
  then shell integration, then trust, then packaging): if what you want to
  build depends on a piece that isn't solid yet, say so in the issue and
  it'll get sequenced, rather than starting a PR that's blocked on
  something else landing first.
- If you're diving into the code itself, [CLAUDE.md](https://github.com/Neirda24/envoke/blob/main/CLAUDE.md)
  has the full package-by-package architecture map — it's written for
  whoever (human or AI) is editing this codebase locally, not required
  reading just to open a PR.

## How to help

- **Bug reports and small fixes** are always welcome without prior
  discussion — see [Reporting bugs](#reporting-bugs) below.
- **Anything bigger** (a new capability, a new shell, a new packaging
  target) — open an issue first so scope, approach, and where it fits in
  the current build order can be agreed before you write code.
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
| `internal/shellinit` (hook generation) or `internal/executor` (ENVOKE_* rendering) | `dagger call -m .dagger test-shell-bash` (swap in `zsh`/`fish`/`tcsh`/`powershell`) — each spins up a container with only that one interpreter installed and runs both packages, so nothing silently skips for lack of a binary |
| `internal/config` (the parser or pattern compilation) | `dagger call -m .dagger fuzz` — a short burst per fuzz target. Give it longer when the change is substantial: `dagger call -m .dagger fuzz --fuzz-time=5m`. The seed corpus runs as ordinary tests under `test` regardless |
| Anything that has to keep building for Windows/macOS | `dagger call -m .dagger cross-build` — compiles and vets all six published OS/arch pairs, and is the only thing that type-checks GOOS-gated files like `internal/matcher/matchpath_windows_test.go` |
| `.goreleaser.yaml` | `dagger call -m .dagger snapshot` — runs the whole release pipeline (cross-compile, archives, `.deb`/`.rpm`, SBOMs, checksums) without touching GitHub |
| `.github/workflows/*.yml` | `dagger -m .dagger call zizmor` and `dagger -m .dagger call actions-up` (see below) |
| `.github/ISSUE_TEMPLATE/`, `.github/DISCUSSION_TEMPLATE/`, or any other YAML under `.github/` | `dagger call -m .dagger yaml-lint` (also runs as part of the full `dagger check -m .dagger`) |
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

## Breaking changes

A change is breaking if an existing setup stops working, or starts behaving
differently, without the user changing anything on their end. Concretely,
that covers:

- Config file syntax: block keywords (`enter`/`leave`), pattern syntax,
  `~`/env-var expansion rules.
- The CLI surface: subcommand names, flags, positional arguments, and exit
  codes a script might branch on.
- What a matched script sees: the `ENVOKE_DIR`/`ENVOKE_TYPE`/`ENVOKE_MATCH`/
  `ENVOKE_MATCH_N` env vars — their names, when they're set, what they
  contain.
- Generated shell hook output (`envoke shell-init <shell>`), if the change
  alters what an already-installed hook does.
- The trust store's format or location (`envoke allow`), if the change
  silently invalidates or relocates existing trust records.

Not breaking: anything under `internal/` (no stable public API is committed
to outside this module), behavior-neutral refactors, new opt-in
flags/blocks that don't change existing behavior when unused, doc wording.

If your PR is breaking, say so in the PR description — there's a checkbox
for it — and explain the migration path, if there is one.

## Submitting changes

1. Open an issue first for anything beyond a small fix, so scope and
   approach can be agreed before you write code.
2. Keep PRs focused on a single capability or bug fix — avoid bundling
   unrelated cleanup.
3. Run `dagger check -m .dagger` (see [Verifying your change](#verifying-your-change))
   and confirm everything passes.
4. Describe *why* in the PR description, not just what changed — the "why"
   is what reviewers and future contributors need most.
5. Flag whether the change is breaking (see [Breaking changes](#breaking-changes)
   above) in the PR description.

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

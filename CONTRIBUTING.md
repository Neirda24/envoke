# Contributing to envoke

Thanks for your interest in contributing. `envoke` is early-stage — see the
`README.md`'s Status section for what's implemented and tested.

## Before you start

- Read `README.md` for the pitch and `docs/design-notes.md` for the
  non-negotiable design principles and the specific points this project
  departs from ondir on. That page is the authoritative list of them, and a
  change that steps around one needs to make its case in an issue first.
- Proposing something that doesn't exist yet is exactly what issues are
  for — see [How to help](#how-to-help) below. The thing to watch for is
  *implementing* out of the codebase's own build order (matching engine,
  then shell integration, then trust, then packaging): if what you want to
  build depends on a piece that isn't solid yet, say so in the issue and
  it'll get sequenced, rather than starting a PR that's blocked on
  something else landing first.
- If you're diving into the code itself, every package under `internal/`
  opens with a doc comment saying what it owns and why it exists — start
  there rather than with any one file.

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
macOS).

Requires the [`dagger` CLI](https://docs.dagger.io/getting-started/installation)
and a container runtime (Docker or similar).

Pick the command that matches what you changed:

| You changed... | Run this |
|---|---|
| Go code anywhere in `internal/`/`cmd/` | `dagger check -m .dagger` (runs the check set — which is not every row below: `fuzz`, `snapshot`, `zizmor` and `actions-up` are deliberately not checks) |
| Just want a quick loop while iterating | `dagger call -m .dagger fmt`, `vet`, `build`, or `test` individually — the Go build and module caches live in persistent Dagger cache volumes, so only the first run after an image pull pays for compiling the standard library |
| `internal/shellinit` (hook generation) or `internal/executor` (ENVOKE_* rendering) | `dagger call -m .dagger test-shell-bash` (swap in `zsh`/`fish`/`tcsh`/`powershell`) — each spins up a container with only that one interpreter installed and runs both packages, so nothing silently skips for lack of a binary |
| `internal/config` (the parser or pattern compilation) | `dagger call -m .dagger fuzz` — a short burst per fuzz target. Give it longer when the change is substantial: `dagger call -m .dagger fuzz --fuzz-time=5m`. The seed corpus runs as ordinary tests under `test` regardless |
| Bumping the Go toolchain, or before a release | `dagger call -m .dagger vuln` — govulncheck against the standard library, which is this module's only dependency. It reports only advisories on a code path envoke actually reaches, so a finding is a reason to bump, not a note to file |
| Anything that has to keep building for Windows/macOS | `dagger call -m .dagger cross-build` — compiles and vets all six published OS/arch pairs, and is the only thing that type-checks GOOS-gated files like `internal/matcher/matchpath_windows_test.go` |
| `.goreleaser.yaml` | `dagger call -m .dagger snapshot` — runs the whole release pipeline (cross-compile, archives, `.deb`/`.rpm`, SBOMs, checksums) without touching GitHub |
| `.github/workflows/*.yml` | `dagger -m .dagger call zizmor` and `dagger -m .dagger call actions-up` (see below) |
| `.github/ISSUE_TEMPLATE/`, `.github/DISCUSSION_TEMPLATE/`, or any other YAML under `.github/` | `dagger call -m .dagger yaml-lint` (also runs as part of the full `dagger check -m .dagger`) |
| `docs/` or `mkdocs.yml` | `docs-build` — the same strict build the docs deploy runs, and the only thing that catches a page missing from the nav. To read the change instead of just validating it: `mkdocs serve`, or `dagger -m ./.dagger call docs up --ports 8000:8000` (see [Previewing documentation](#previewing-documentation-changes)) |
| `.dagger/main.go` itself | No dedicated check yet — build it manually (`docker run` against the pinned Go image, or `dagger develop -m .dagger` to confirm it still generates) |

Before opening a PR, run the full suite:

```sh
dagger check -m .dagger
```

That runs every function the Dagger module marks as a check — among them the
`gofmt -l .`, `go vet ./...`, `go build ./...` and `go test ./... -race`
equivalents, plus `golangci-lint`. Rather than keep a list here that drifts,
ask the module: `dagger functions -m .dagger` prints its whole surface, with
descriptions, in a couple of seconds.

`.github/workflows/ci.yml` runs the exact same command on every push/PR to
`main` and on a daily schedule — a clean local run gets you the same feedback
sooner for everything except the two platforms below.

### What only CI can check

Dagger's engine runs Linux containers. A Windows container isn't merely
unconfigured, it's impossible, and Wine is not a substitute (this was tested:
the Wine versions that run on Apple Silicon lack a DLL modern Go requires).
So the `native` job in `.github/workflows/ci.yml` runs the real suite —
`go test ./... -race`, the whole tree — on GitHub's own `macos-latest` and
`windows-latest` runners instead. Both run everything, and each covers what
nothing else can: macOS is where the generated hook scripts meet bash 3.2,
still `/bin/bash` on every Mac, and Windows is the only thing that proves the
path handling *works* rather than merely compiles, as well as the only place
PowerShell's hook runs on the platform its hook point belongs to.

A test that cannot mean anything on one of those platforms skips itself and
says why — Windows has no POSIX interpreter for most of the hook drivers and no
permission bits for the "writable by someone else" warnings to fire on. So a
`SKIP` in a green log is expected rather than a gap, and reading those lines is
how the list of what a platform can't express stays honest. The `test-shell-*`
checks are what stop a shell from passing by skipping.

`dagger call -m .dagger cross-build` is what keeps both platforms *compiling*
from a Linux container, and is worth running before you push anything touching
path handling or a build-tagged file — but it is compile-level only and cannot
see a behavior difference.

When `native` goes red, no Dagger command will reproduce it. Read the failing
platform's log; if you have that platform to hand, the runner's own command
(`go test ./... -race`) is the only local reproduction that exists, and it is
the one place the "always through Dagger" rule above has to give, because no
container can cover it. Otherwise add the regression test in a build-tagged
file, confirm `cross-build` still type-checks it, and let CI report.

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

Reading the page is not the same as validating it, and only one of the two
catches an unlisted page. Run the strict build before you push:

```sh
dagger call -m .dagger docs-build
```

Every `.md` file under `docs/` has to appear in `mkdocs.yml`'s nav, and this is
what fails when one doesn't. The dev server and the Dagger `docs` service both
render happily either way, and the deploy workflow — which runs the strict
build on `main` — is where the omission would otherwise surface, after merge.
`docs-build` is part of `dagger check -m .dagger`, so CI catches it on a pull
request too.

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

Security vulnerabilities should go through `SECURITY.md` instead of a public
issue.

## License

By contributing, you agree that your contributions will be licensed under
the project's MIT License, in `LICENSE`.

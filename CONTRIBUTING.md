# Contributing to envoke

Thanks for your interest in contributing. `envoke` is early-stage — see [CLAUDE.md](https://github.com/Neirda24/envoke/blob/main/CLAUDE.md) for the current MVP scope and what's already done.

## Before you start

- Read the [README](https://github.com/Neirda24/envoke/blob/main/README.md) for the design rationale and the [Design notes](https://github.com/Neirda24/envoke/blob/main/README.md#design-notes) table listing the ondir bugs this project exists to fix.
- Read [CLAUDE.md](https://github.com/Neirda24/envoke/blob/main/CLAUDE.md) for the non-negotiable design principles (RE2-only matching, path-segment matching, no implicit enter/leave undo, trust-before-execution) and the MVP scope order.
- Don't jump ahead to a later MVP step before the current one has tests passing — open an issue first if you want to work on something out of order.

## Development setup

Requires Go 1.23+. No other tooling — the project has zero non-stdlib dependencies by design; keep it that way unless there's a strong reason to add one.

## Verifying your change

Before opening a PR, run this exact sequence from the repo root. All four commands must exit with status 0 and produce no output (aside from `go build`/`go test`'s normal build/pass messages) — any output from `gofmt -l` or `go vet`, or a non-zero exit from any command, means the change is not ready:

```sh
gofmt -l .            # must print nothing — if it prints file names, run: gofmt -w .
go vet ./...           # must print nothing
go build ./...         # must succeed
go test ./... -race    # must print "ok" for every package, no FAIL
```

There is no Makefile yet, but there is a [Dagger](https://dagger.io) pipeline under [`.dagger/`](.dagger/) that mirrors these four commands, adds `golangci-lint`, runs the shellinit end-to-end tests against a real interpreter for each of the five supported shells (installing whichever one a given check needs, so none of them get silently skipped for lack of the binary the way a local `go test` run does), and lints/audits the GitHub Actions workflows themselves (see below). Run the whole suite with:

```sh
dagger check -m .dagger
```

This requires the [`dagger` CLI](https://docs.dagger.io/getting-started/installation) and a container runtime (Docker or similar). `.github/workflows/ci.yml` runs the exact same `dagger check` on every push/PR to `main` and on a daily schedule, so a local run before opening a PR just gets you the same feedback sooner — it's also the only way to actually exercise fish/tcsh/powershell if you don't have them installed locally.

### Keeping GitHub Actions workflows secure and current

Two of the checks above audit `.github/workflows/*.yml` itself, rather than the Go code:

- [`zizmor`](https://docs.zizmor.sh/) — static security analysis for GitHub Actions (unpinned `uses:` refs, overly broad `permissions:`, credential persistence, etc.)
- [`actions-up`](https://github.com/azat-io/actions-up) — checks every `uses:` is pinned to a commit SHA and at its latest compatible version

Run either standalone:

```sh
dagger -m .dagger call zizmor
dagger -m .dagger call actions-up
```

Both benefit from an authenticated GitHub API call (the unauthenticated rate limit is 60/hr) via an optional `--gh-auth-token` flag on the module itself — reuse your own `gh` CLI session rather than minting a PAT:

```sh
dagger -m .dagger call --gh-auth-token='cmd://gh auth token' zizmor
```

When either check fails, fix what's auto-fixable in one pass and inspect the diff before applying it:

```sh
dagger -m .dagger call autofix export --path=.
```

`autofix` runs zizmor's `--fix=all` first, then `actions-up --yes` against the zizmor-fixed tree, and returns the combined diff — it does not overwrite anything until you `export` it. Re-run `zizmor`/`actions-up` afterward: some findings (e.g. `permissions:` set at the workflow level instead of scoped to the one job that needs it) require a manual restructure that neither tool will do for you.

## Previewing documentation changes

The [docs site](https://neirda24.github.io/envoke/) lives under `docs/` + `mkdocs.yml`. Preview it locally before opening a PR that touches either:

```sh
pip install -r docs/requirements.txt && mkdocs serve   # localhost:8000, live-reload
```

Or, without installing Python, via the same Dagger CLI as above:

```sh
dagger -m ./.dagger call docs up --ports 8000:8000
```

## Code conventions

- Table-driven tests via subtests, named `Test<Func>_<Scenario>`.
- New bug-fix behavior should include a regression test, ideally tied to the ondir bug it addresses if applicable.
- Prefer editing existing files over adding new abstractions; no speculative generalization ahead of an actual second use case.
- Static, dependency-free binaries — no cgo, no new imports unless the MVP step genuinely requires one (discuss in the issue/PR first).

## Submitting changes

1. Open an issue first for anything beyond a small fix, so scope and approach can be agreed before you write code.
2. Keep PRs focused on a single MVP step or bug fix — avoid bundling unrelated cleanup.
3. Run the four commands in [Verifying your change](#verifying-your-change) and confirm all pass.
4. Describe *why* in the PR description, not just what changed — the "why" is what reviewers and future contributors need most.

## Reporting bugs

Open a GitHub issue with:
- The config block that triggers the problem (redact anything sensitive).
- The `from`/`to` paths involved.
- Expected vs. actual behavior.

## License

By contributing, you agree that your contributions will be licensed under the project's [MIT License](LICENSE).

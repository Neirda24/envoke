# Contributing to envoke

Thanks for your interest in contributing. `envoke` is early-stage — see [CLAUDE.md](CLAUDE.md) for the current MVP scope and what's already done.

## Before you start

- Read the [README](README.md) for the design rationale and the [Design notes](README.md#design-notes) table listing the ondir bugs this project exists to fix.
- Read [CLAUDE.md](CLAUDE.md) for the non-negotiable design principles (RE2-only matching, path-segment matching, no implicit enter/leave undo, trust-before-execution) and the MVP scope order.
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

There is no Makefile yet, but there is a [Dagger](https://dagger.io) pipeline under [`.dagger/`](.dagger/) that mirrors these four commands, adds `golangci-lint`, and runs the shellinit end-to-end tests against a real interpreter for each of the five supported shells (installing whichever one a given check needs, so none of them get silently skipped for lack of the binary the way a local `go test` run does). Run it with:

```sh
dagger check -m .dagger
```

This requires the [`dagger` CLI](https://docs.dagger.io/getting-started/installation) and a container runtime (Docker or similar). It isn't wired into GitHub Actions yet, so running it is optional but recommended before a PR that touches `internal/shellinit` or anything shell-integration-related — it's the only way to actually exercise fish/tcsh/powershell if you don't have them installed locally.

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

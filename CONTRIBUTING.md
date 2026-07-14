# Contributing to envoke

Thanks for your interest in contributing. `envoke` is early-stage — see [CLAUDE.md](CLAUDE.md) for the current MVP scope and what's already done.

## Before you start

- Read the [README](README.md) for the design rationale and the [Design notes](README.md#design-notes) table listing the ondir bugs this project exists to fix.
- Read [CLAUDE.md](CLAUDE.md) for the non-negotiable design principles (RE2-only matching, path-segment matching, no implicit enter/leave undo, trust-before-execution) and the MVP scope order.
- Don't jump ahead to a later MVP step before the current one has tests passing — open an issue first if you want to work on something out of order.

## Development setup

Requires Go 1.23+. No other tooling — the project has zero non-stdlib dependencies by design; keep it that way unless there's a strong reason to add one.

```sh
go build ./...
go test ./... -race
go vet ./...
```

## Code conventions

- Table-driven tests via subtests, named `Test<Func>_<Scenario>`.
- New bug-fix behavior should include a regression test, ideally tied to the ondir bug it addresses if applicable.
- Prefer editing existing files over adding new abstractions; no speculative generalization ahead of an actual second use case.
- Static, dependency-free binaries — no cgo, no new imports unless the MVP step genuinely requires one (discuss in the issue/PR first).

## Submitting changes

1. Open an issue first for anything beyond a small fix, so scope and approach can be agreed before you write code.
2. Keep PRs focused on a single MVP step or bug fix — avoid bundling unrelated cleanup.
3. Make sure `go test ./... -race` and `go vet ./...` pass.
4. Describe *why* in the PR description, not just what changed — the "why" is what reviewers and future contributors need most.

## Reporting bugs

Open a GitHub issue with:
- The config block that triggers the problem (redact anything sensitive).
- The `from`/`to` paths involved.
- Expected vs. actual behavior.

## License

By contributing, you agree that your contributions will be licensed under the project's [MIT License](LICENSE).

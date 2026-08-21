# .dagger

A separate Go module (own `go.mod`, `dagger/envoke`), **not** a dependency of
the main module. It is the reason `go build`/`go test`/`go vet`/`gofmt`/
`golangci-lint` are never run directly on the host.

## Finding out what's here

`dagger functions -m .dagger` lists every function with its description, in
about two seconds. That is the list — don't keep a copy of it in this file.

Which of them are **checks** is the one thing that command doesn't show:
membership is the `// +check` pragma on its own line above the function, and
nothing else. Don't infer it from the doc comments — several of them mention
`+check` precisely to say the function *isn't* one. `grep -c '^// +check$'
main.go` is the honest count.

`.github/workflows/ci.yml` runs `dagger check -m .dagger` on push/PR to `main`
and daily. `fmt`/`vet`/`build`/`test` mirror the manual commands
`CONTRIBUTING.md` documents, so those must keep behaving identically.

## What the doc comments can't tell you

- `cross-build` is the only thing that type-checks the GOOS-gated test files
  the Linux containers never load. Its job looks like "compile for every
  target"; dropping it loses Windows/darwin compile coverage of tests silently.
- `test-shell-*` run `internal/shellinit`'s **and `internal/executor`'s**
  suites — both emit shell code and both `t.Skip` without the interpreter, so
  one container per shell is what stops a check from passing by skipping.
- `vuln`: zero non-stdlib imports means the standard library *is* the
  dependency, and govulncheck is the tool that knows which of its functions a
  given Go release has an advisory against. `.dagger` is excluded because its
  deps come from SDK codegen and never ship.
- `zizmor`/`actions-up` are **not** checks because the `check` CLI verb never
  forwards constructor flags, so their `GhAuthToken` (which raises the
  unauthenticated GitHub API rate limit) only arrives via `dagger call zizmor
  --gh-auth-token=...`. `ci.yml` runs them as their own token-authenticated
  steps; keeping them out of `+check` means CI's unauthenticated `dagger check`
  never needs to know about the token. Omit it for a quick local run.
- **The Dagger version is pinned in `dagger.json`'s `engineVersion` and again as
  `version:` in every workflow that uses `dagger-for-github`, with a leading `v`
  on the first and none on the rest.** Renovate does hold both sides: a custom
  manager each, joined by the `dagger/dagger` `packageRules` entry into one PR,
  with `semver-coerced` absorbing that spelling difference — so don't hand-bump
  them "because nothing keeps them in sync". What can go wrong is narrower: the
  workflow-side manager lists the files it covers one by one, so a **new**
  workflow passing `version:` is invisible to it until it is added to
  `managerFilePatterns`, and the match pattern requires `version:` to be the
  first key under `with:`, so reordering `module:` above it freezes that file
  silently. Check `renovate.json` against `grep -rn 'version:'
  .github/workflows/` when either changes.
- `autofix` mutates the tree, which is why it isn't a check.
  `.github/workflows/actions-autofix.yml` runs it nightly (02:00) and on
  `workflow_dispatch` and opens a PR, so a pending fix normally arrives as a PR
  rather than something you run by hand.

## Layer discipline

Two invariants about where a step sits relative to `withSource`, both
unenforced and both silent when broken — the check still passes, it just gets
slow again:

- **Anything that runs `go` derives from `goBase`.** That is the only place
  `GOCACHE`/`GOMODCACHE` and their cache volumes are mounted, so a container
  built straight from `dag.Container().From(goImage)` recompiles the standard
  library (with `-race` instrumentation, under `test`) every invocation. The
  mounts are `CacheSharingModeShared`: `dagger check` runs checks
  concurrently, `go` is built for concurrent use of both caches, and `LOCKED`
  would queue every Go check behind one mount — more expensive than the
  problem. `goreleaserBase` deliberately doesn't share them; it is a different
  image with its own `GOPATH`, and `snapshot`/`publish` aren't checks.
- **A tool install goes in its own `*Base`, above the source mount.**
  `golangciLintBase`/`govulncheckBase` exist for nothing else. Both started
  life as a `go install` appended inside `Lint`/`Vuln`, i.e. downstream of
  `withSource`, where each source edit rebuilt the whole tool before the check
  began. `docsBase`/`yamllintBase` are the same discipline for pip. Appending
  a `WithExec` to the check function is the easy, wrong place.

Two of `renovate.json`'s `customManagers` match `golangciLintPath` and
`govulncheckPath` out of `main.go` by constant name, module path and all. The
constants may move between functions, but renaming one, or splitting its `@vX`
into a constant of its own, stops the manager matching and freezes that pin
with nothing reporting it — check `renovate.json` before reshaping either.

## What this module cannot cover

Windows and macOS are tested by the `native` job in `.github/workflows/ci.yml`,
on real runners, and never here: the Dagger engine runs Linux containers, so a
Windows container is not merely unconfigured but impossible. Wine is not a
substitute either, and this is settled — Wine 8 lacks the
`bcryptprimitives.dll` modern Go requires, and Wine 10 needs x86 segmentation
that Apple Silicon's emulation doesn't provide, so it isn't even reproducible
locally. Both were verified; don't re-litigate it. `cross-build` is what keeps
those platforms *compiling* from inside a Linux container.

`CONTRIBUTING.md`'s "What only CI can check" states this for contributors —
which runners, which package set each runs, and that a red `native` job has no
local Dagger reproduction. Keep the two from drifting: if the matrix in
`ci.yml` changes, that section is the published one that has to change with
it.

## Generated files

`dagger.gen.go`/`internal/dagger`/`internal/telemetry` are generated and
gitignored — regenerate with `dagger develop -m .dagger` after changing
`New`'s signature, don't hand-edit.

Consequence worth knowing: an IDE will report *"Struct Envoke has methods on
both value and pointer receivers"* on this package. Every method in `main.go`
takes `*Envoke`; the sole value receiver is `func (r Envoke) MarshalJSON()` in
the generated `dagger.gen.go`. It is neither actionable (the file is
regenerated and untracked) nor a bug (dagger only ever holds a `*Envoke`).
Suppress it locally if it bothers you — there is nothing here to commit.

## Verifying changes to this module

It has its own `go.mod`, so the checks above don't cover it. Verify via
`docker run` against the pinned Go image — `scripts/test-shellinit-all-shells.sh`
is one such rerunnable one-off, for running the full `internal/shellinit` suite
with every interpreter installed at once.

## Packaging (`snapshot`/`publish` + `.goreleaser.yaml`)

`goreleaser` only ever runs inside the Dagger container, never installed on the
dev machine. `syft`, `nfpm` and `cosign` already ship inside the pinned
goreleaser image — verify before adding anything that assumes another tool is
there.

`snapshot` never touches GitHub, so it is safe to run anytime, and it is what
exercises the `nfpms:` deb/rpm build and the `sboms:` syft step locally since
neither needs an external repo.

`publish` needs a pushed `v*` tag. Two details the config doesn't explain:
it uses `homebrew_casks:`, **not** the deprecated `brews:` key; and the pushes
to `Neirda24/homebrew-tap` and `Neirda24/scoop-bucket` share one short-lived
GitHub App installation token scoped to just those two repos, minted per-run in
`release.yml` rather than being a long-lived cross-repo PAT — so the ambient
per-job `GITHUB_TOKEN` only ever needs write access to `envoke` itself. Both
target repos are provisioned and the App's installation covers them; verified
end to end against v0.1.4, where `Neirda24/scoop-bucket` holds a
`bucket/envoke.json` manifest for the tag and the release carries both `.deb`
and `.rpm` assets.

`release.footer` in `.goreleaser.yaml` appends the install/upgrade instructions
to every release's notes — update that template, not the README, if a new
install method ships.

# .dagger

A separate Go module (`dagger/envoke`), **not** a dependency of the main one.
It is why `go`/`gofmt`/`golangci-lint`/`govulncheck` are never run on the host.

## Finding out what's here

- `dagger functions -m .dagger` lists every function with its description in
  ~2s. Don't keep a copy of that list here.
- Which are **checks** is the one thing it doesn't show: the `// +check`
  pragma on its own line, and nothing else. Several doc comments mention
  `+check` precisely to say the function *isn't* one. `grep -c '^// +check$'
  main.go` is the honest count.
- `fmt`/`vet`/`build`/`test` mirror the manual commands `CONTRIBUTING.md`
  documents and must keep behaving identically.

## What the doc comments can't tell you

- **`cross-build`** is the only thing that type-checks the GOOS-gated test
  files the Linux containers never load. Dropping it loses that silently.
- **`test-shell-*`** run `internal/executor`'s suite as well as
  `internal/shellinit`'s — both `t.Skip` without the interpreter, so one
  container per shell is what stops a check passing by skipping.
- **`zizmor`/`actions-up` are not checks** because the `check` verb never
  forwards constructor flags, so `GhAuthToken` only arrives via `dagger call`.
  `ci.yml` runs them as their own authenticated steps.
- **`autofix` mutates the tree**, hence not a check.
  `.github/workflows/actions-autofix.yml` runs it nightly and opens a PR, so a
  pending fix normally arrives as a PR rather than something you run by hand.

## Layer discipline

Two invariants about where a step sits relative to `withSource`. Both
unenforced and both silent when broken — the check still passes, it just gets
slow again.

- **Anything that runs `go` derives from `goBase`**, the only place
  `GOCACHE`/`GOMODCACHE` and their cache volumes are mounted.
  `goreleaserBase` deliberately doesn't share them: different image, own
  `GOPATH`, and `snapshot`/`publish` aren't checks.
- **A tool install goes in its own `*Base`, above the source mount.**
  `golangciLintBase`/`govulncheckBase` exist for nothing else; both began as a
  `go install` appended inside the check, where every source edit rebuilt the
  whole tool first. `docsBase`/`yamllintBase` are the same for pip. Appending
  a `WithExec` to the check function is the easy, wrong place.
- **`snapshot`/`publish` use `withGitSource`, not `withSource`.** `.git` is
  out of the constructor's `source` so a commit doesn't bust every check's
  cache, which means goreleaser gets no version and no tag from `withSource`
  alone. That failure is silent in `snapshot` (`--snapshot` implies
  `--skip=validate`, so it just builds `0.0.0`, with `none` for the commit,
  and drops the rpm packager) and fatal in `publish`. `dagger call -m .dagger
  snapshot entries` showing a real version is the check that it still works.
- **Nothing tracked goes in the constructor's `+ignore` list.** With a history
  mounted beside the tree, an ignored tracked file reads as deleted and
  `goreleaser release` refuses a dirty tree.

## Renovate coupling

Renovate does hold both sides of each pin — don't hand-bump "because nothing
keeps them in sync". What breaks is narrower:

- `golangciLintPath`/`govulncheckPath` are matched **by constant name and full
  module path**. Renaming one, or splitting its `@vX` into its own constant,
  freezes that pin with nothing reporting it.
- The Dagger version lives in `dagger.json`'s `engineVersion` and as
  `version:` in every workflow using `dagger-for-github`. The workflow-side
  manager lists its files one by one, so a **new** workflow passing `version:`
  is invisible until added to `managerFilePatterns`; the pattern also requires
  `version:` to be the first key under `with:`, so reordering `module:` above
  it freezes that file silently.
- Check `renovate.json` against `grep -rn 'version:' .github/workflows/` when
  either changes.

## What this module cannot cover

- Windows and macOS are tested by `ci.yml`'s `native` job on real runners, and
  never here: the Dagger engine runs Linux containers, so a Windows container
  is impossible.
- **Wine is settled, don't re-litigate.** Wine 8 lacks the
  `bcryptprimitives.dll` modern Go requires; Wine 10 needs x86 segmentation
  Apple Silicon's emulation doesn't provide. Both verified.
- `CONTRIBUTING.md`'s "What only CI can check" is the published version of
  this. If `ci.yml`'s matrix changes, that section changes with it.

## Generated files

- `dagger.gen.go`/`internal/dagger`/`internal/telemetry` are generated and
  gitignored — `dagger develop -m .dagger` after changing `New`'s signature,
  never hand-edit.
- An IDE will report *"Struct Envoke has methods on both value and pointer
  receivers"*: the sole value receiver is `MarshalJSON` in the generated file.
  Neither actionable nor a bug; there is nothing here to commit.

## Verifying changes to this module

It has its own `go.mod`, so the checks above don't cover it. Verify via
`docker run` against the pinned Go image —
`scripts/test-shellinit-all-shells.sh` is one such rerunnable one-off.

## Packaging

- `goreleaser` only ever runs inside the container. `syft`, `nfpm` and
  `cosign` already ship in the pinned image — verify before adding anything
  that assumes another tool is there.
- `snapshot` never touches GitHub, and is what exercises the `nfpms:` and
  `sboms:` steps locally.
- `publish` needs a pushed `v*` tag. Two things the config doesn't explain: it
  uses `homebrew_casks:`, **not** the deprecated `brews:`; and the tap and
  bucket pushes share one short-lived GitHub App token scoped to those two
  repos, so the ambient `GITHUB_TOKEN` only ever needs write access to
  `envoke` itself.
- `release.footer` in `.goreleaser.yaml` appends install/upgrade instructions
  to every release's notes — update that template, not the README, when a new
  install method ships.

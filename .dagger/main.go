// Dagger CI pipeline for envoke.
//
// Mirrors CONTRIBUTING.md's "Verifying your change", adds golangci-lint, and
// runs the shellinit and executor end-to-end tests against a real interpreter
// for each of the five supported shells — which a plain local `go test`
// silently skips for lack of the binary.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"dagger/envoke/internal/dagger"
)

const (
	// renovate: datasource=docker
	goImage = "golang:1.27-bookworm@sha256:484ef6066fa69acb059fdfeda7ba2b8f7391f2ef6abc6f9b8411e669ebd56466"
	// renovate.json has one regex manager per constant here, matching on the
	// constant's name plus the module path spelled out in full. Renaming
	// either, or splitting the version off into a value of its own, stops
	// the manager matching and freezes that pin with nothing reporting it.
	golangciLintPath = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1"
	govulncheckPath  = "golang.org/x/vuln/cmd/govulncheck@v1.7.0"
	// Set as GOCACHE/GOMODCACHE in goBase rather than left to the image's
	// defaults: a mount that isn't where the toolchain looks caches nothing
	// and says nothing.
	goBuildCachePath = "/root/.cache/go-build"
	goModCachePath   = "/go/pkg/mod"
	// renovate: datasource=docker
	goreleaserImage = "goreleaser/goreleaser:v2.17.1@sha256:1098a0be4da1780f9616a85f4c5050447b53e3e74804d8017ec1e2bbb1fb697a"
	// renovate: datasource=docker
	pythonImage     = "python:3.14-slim@sha256:ce40764625a4ff50df3548277632e7f96c4e77fe75fa848aae9885476e7df5a4"
	yamllintVersion = "1.38.0"
	// zizmorImage intentionally tracks :latest, not a pinned tag — no renovate hint.
	zizmorImage = "ghcr.io/zizmorcore/zizmor:latest"
	// renovate: datasource=docker
	nodeImage = "node:24-alpine@sha256:d32cdf619f63fe0471182d08996dd516c6275bb5fd31ae06e55a570bd9e1ad43"
	docsPort  = 8000
)

type Envoke struct {
	// Source is the envoke repository root.
	Source *dagger.Directory
	// GitDir is the repository's own .git, kept out of Source and mounted
	// only by withGitSource.
	GitDir *dagger.Directory
	// GhAuthToken authenticates zizmor's and actions-up's GitHub API calls,
	// raising their unauthenticated 60/hr rate limit. Optional: both tools
	// still work without it, just slower/flakier on a busy day.
	GhAuthToken *dagger.Secret
}

func New(
	// The repository root. The source digest is every check's cache key, so
	// anything no check reads is excluded. Keep it that way: .github and docs
	// are read by yaml-lint and docs-build, so neither may be added.
	//
	// .git is excluded here and taken as gitDir below rather than left out
	// altogether, because goreleaser does read it. Nothing tracked may be
	// added to this list now that it is: git compares the mounted history
	// against the mounted tree, so an ignored tracked file reads as deleted
	// and `goreleaser release` refuses a dirty tree.
	//
	// +defaultPath="/"
	// +ignore=[".git", ".idea", "review", "*_handoff.md", "CLAUDE.local.md"]
	source *dagger.Directory,
	// The repository's history, which goreleaser derives the version and the
	// tag from. Separate from source so that a commit invalidates only the
	// two functions that mount it, rather than every check's cache.
	//
	// +defaultPath="/.git"
	// +optional
	gitDir *dagger.Directory,
	// +optional
	ghAuthToken *dagger.Secret,
) *Envoke {
	return &Envoke{Source: source, GitDir: gitDir, GhAuthToken: ghAuthToken}
}

// goBase is a bare Go toolchain container, before the source tree is
// mounted, so package-install layers cache independently of source edits.
//
// The caches are persistent volumes because the source mount invalidates
// every exec below it: without them each check recompiles the standard
// library, and Test instruments it for -race, on every invocation.
//
// SHARED is the default, stated to make the choice reviewable: `dagger check`
// runs concurrently and the `go` command is built for that — build-cache
// entries are content-addressed and land by rename, and the module cache
// serializes downloads through lock files. LOCKED would queue every Go check
// behind one mount.
func (m *Envoke) goBase() *dagger.Container {
	// CGO must stay enabled: go test -race requires cgo.
	return dag.Container().From(goImage).
		WithEnvVariable("GOCACHE", goBuildCachePath).
		WithEnvVariable("GOMODCACHE", goModCachePath).
		WithMountedCache(goBuildCachePath, dag.CacheVolume("go-build"), dagger.ContainerWithMountedCacheOpts{
			Sharing: dagger.CacheSharingModeShared,
		}).
		WithMountedCache(goModCachePath, dag.CacheVolume("go-mod"), dagger.ContainerWithMountedCacheOpts{
			Sharing: dagger.CacheSharingModeShared,
		})
}

func (m *Envoke) withSource(c *dagger.Container) *dagger.Container {
	return c.WithMountedDirectory("/src", m.Source).WithWorkdir("/src")
}

// withGitSource is withSource for goreleaser, which is the only thing here
// that reads the repository's history: it derives the version from the
// nearest tag, and `goreleaser release` refuses to run without one at all.
// Without this the release job publishes 0.0.0 or fails outright, and
// nothing else in the pipeline would notice — no check reads .git.
func (m *Envoke) withGitSource(c *dagger.Container) *dagger.Container {
	c = m.withSource(c)
	if m.GitDir == nil {
		return c
	}
	return c.WithMountedDirectory("/src/.git", m.GitDir)
}

// aptInstall installs the given Debian packages on top of goBase.
func (m *Envoke) aptInstall(pkgs ...string) *dagger.Container {
	return m.goBase().
		WithExec([]string{"apt-get", "update"}).
		WithExec(append([]string{"apt-get", "install", "-y", "--no-install-recommends"}, pkgs...))
}

// powershellBase installs PowerShell (pwsh) via Microsoft's Debian 12 apt
// repo, per Microsoft's documented install steps.
func (m *Envoke) powershellBase() *dagger.Container {
	return m.aptInstall("wget", "apt-transport-https", "software-properties-common").
		WithExec([]string{"sh", "-c", "wget -q https://packages.microsoft.com/config/debian/12/packages-microsoft-prod.deb -O /tmp/packages-microsoft-prod.deb"}).
		WithExec([]string{"dpkg", "-i", "/tmp/packages-microsoft-prod.deb"}).
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "powershell"})
}

// golangciLintBase installs golangci-lint on top of goBase, before the source
// tree is mounted: downstream of withSource it would rebuild the whole linter
// on every source edit, which is the check's dominant cost.
func (m *Envoke) golangciLintBase() *dagger.Container {
	return m.goBase().
		// golangci-lint's own go.mod may require a newer Go than this
		// module's; let the toolchain fetch itself rather than pinning the
		// base image to whatever golangci-lint currently needs. It lands in
		// the module cache, so it downloads once.
		WithEnvVariable("GOTOOLCHAIN", "auto").
		WithExec([]string{"go", "install", golangciLintPath})
}

// govulncheckBase installs govulncheck on top of goBase, upstream of the
// source mount for the same reason as golangciLintBase.
func (m *Envoke) govulncheckBase() *dagger.Container {
	return m.goBase().
		WithExec([]string{"go", "install", govulncheckPath})
}

// goreleaserBase is a goreleaser container, before the source tree is
// mounted, so the image layer caches independently of source edits.
func (m *Envoke) goreleaserBase() *dagger.Container {
	return dag.Container().From(goreleaserImage)
}

// docsBase installs the pinned mkdocs-material version from
// docs/requirements.txt only, so this layer caches independently of
// unrelated source edits (it only invalidates when requirements.txt itself
// changes, not on every doc page edit).
func (m *Envoke) docsBase() *dagger.Container {
	return dag.Container().From(pythonImage).
		WithFile("/tmp/requirements.txt", m.Source.File("docs/requirements.txt")).
		WithExec([]string{"pip", "install", "--no-cache-dir", "-r", "/tmp/requirements.txt"})
}

// yamllintBase installs yamllint on top of the pinned Python image, kept
// separate from docsBase so this layer caches independently of
// docs/requirements.txt edits.
func (m *Envoke) yamllintBase() *dagger.Container {
	return dag.Container().From(pythonImage).
		WithExec([]string{"pip", "install", "--no-cache-dir", "yamllint==" + yamllintVersion})
}

// shellTest runs the two packages whose tests drive a real interpreter end to
// end rather than string-matching generated text: internal/shellinit for the
// hook scripts, internal/executor for the ENVOKE_* assignments those hooks
// eval. Both t.Skip without the interpreter installed, which is why each
// TestShell* check below builds a container with one shell in it.
func (m *Envoke) shellTest(ctx context.Context, c *dagger.Container) error {
	_, err := m.withSource(c).
		WithExec([]string{"go", "test", "./internal/shellinit/...", "./internal/executor/...", "-race", "-v"}).
		Sync(ctx)
	return err
}

// Fmt checks that every file is gofmt-formatted.
//
// +check
func (m *Envoke) Fmt(ctx context.Context) error {
	out, err := m.withSource(m.goBase()).
		WithExec([]string{"gofmt", "-l", "."}).
		Stdout(ctx)
	if err != nil {
		return err
	}
	if out != "" {
		return fmt.Errorf("gofmt found unformatted files:\n%s", out)
	}
	return nil
}

// Vet runs go vet across every package.
//
// +check
func (m *Envoke) Vet(ctx context.Context) error {
	_, err := m.withSource(m.goBase()).
		WithExec([]string{"go", "vet", "./..."}).
		Sync(ctx)
	return err
}

// Build compiles every package.
//
// +check
func (m *Envoke) Build(ctx context.Context) error {
	_, err := m.withSource(m.goBase()).
		WithExec([]string{"go", "build", "./..."}).
		Sync(ctx)
	return err
}

// CrossBuild compiles and vets every OS/arch .goreleaser.yaml publishes, so a
// platform that ships as a release artifact can't silently stop building. `go
// vet` also loads the GOOS-specific test files the Linux containers never see.
//
// Compile-level only, and still worth having beside ci.yml's `native` job: it
// catches a broken cross-compile before a push, and covers the arm64 targets
// no runner exercises.
//
// +check
func (m *Envoke) CrossBuild(ctx context.Context) error {
	targets := []struct{ goos, goarch string }{
		{"linux", "amd64"}, {"linux", "arm64"},
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
	}

	c := m.withSource(m.goBase())
	for _, t := range targets {
		// CGO off: cross-compiling with cgo would need a cross toolchain,
		// and envoke is a pure-Go, dependency-free binary anyway (the race
		// detector in Test is the only thing that needs cgo).
		staged := c.
			WithEnvVariable("CGO_ENABLED", "0").
			WithEnvVariable("GOOS", t.goos).
			WithEnvVariable("GOARCH", t.goarch).
			WithExec([]string{"go", "build", "./..."}).
			WithExec([]string{"go", "vet", "./..."})
		if _, err := staged.Sync(ctx); err != nil {
			return fmt.Errorf("%s/%s: %w", t.goos, t.goarch, err)
		}
	}
	return nil
}

// Test runs the full test suite with the race detector.
//
// +check
func (m *Envoke) Test(ctx context.Context) error {
	_, err := m.withSource(m.goBase()).
		WithExec([]string{"go", "test", "./...", "-race"}).
		Sync(ctx)
	return err
}

// fuzzTargets are the fuzz functions Fuzz drives, as package/name pairs.
// Listed explicitly rather than discovered, so adding a target without
// wiring it in here is a visible omission rather than a silent one.
var fuzzTargets = []struct{ pkg, name string }{
	{"./internal/config", "FuzzParse"},
	{"./internal/config", "FuzzParseBytesMatchesParse"},
	{"./internal/config", "FuzzCompilePattern"},
}

// Fuzz runs each fuzz target for a short burst: a smoke run to catch a target
// that has started failing or stopped compiling, not a soak. The seed corpus
// already runs under Test on every commit; real fuzzing is a longer -fuzztime
// locally when touching the parser.
//
// Not a +check — a fixed-duration run per target would add minutes to every CI
// run for a low hit rate. Run it deliberately:
//
//	dagger call -m .dagger fuzz
func (m *Envoke) Fuzz(
	ctx context.Context,
	// +optional
	// +default="20s"
	fuzzTime string,
) error {
	for _, t := range fuzzTargets {
		_, err := m.withSource(m.goBase()).
			WithExec([]string{
				"go", "test", t.pkg,
				"-run=^$", // no ordinary tests, just the fuzzing
				"-fuzz=^" + t.name + "$",
				"-fuzztime=" + fuzzTime,
			}).
			Sync(ctx)
		if err != nil {
			return fmt.Errorf("%s %s: %w", t.pkg, t.name, err)
		}
	}
	return nil
}

// Lint runs golangci-lint.
//
// +check
func (m *Envoke) Lint(ctx context.Context) error {
	_, err := m.withSource(m.golangciLintBase()).
		WithExec([]string{"golangci-lint", "run", "./..."}).
		Sync(ctx)
	return err
}

// Vuln runs govulncheck against the main module.
//
// Having no non-stdlib imports does not mean there is nothing to scan: the
// standard library is the dependency, and govulncheck knows which of its
// functions a given Go release has an advisory against, on a path this binary
// actually reaches. A finding here is a reason to bump the toolchain.
//
// Only ./... : .dagger is a separate module whose dependencies come from the
// SDK's codegen, are not independently upgradable, and never ship.
//
// +check
func (m *Envoke) Vuln(ctx context.Context) error {
	_, err := m.withSource(m.govulncheckBase()).
		WithExec([]string{"govulncheck", "./..."}).
		Sync(ctx)
	return err
}

// YamlLint checks the syntax of every YAML file under .github with the
// relaxed profile: a bad indent or a duplicate key fails, line length doesn't.
// Catches a broken issue-form field before it breaks GitHub's "New issue"
// picker.
//
// +check
func (m *Envoke) YamlLint(ctx context.Context) error {
	_, err := m.withSource(m.yamllintBase()).
		WithExec([]string{"yamllint", "-d", "relaxed", ".github"}).
		Sync(ctx)
	return err
}

// zizmorBase is a zizmor container, before the source tree is mounted, with
// a persistent cache volume for zizmor's own audit cache so repeat runs
// don't re-fetch what they already know.
func (m *Envoke) zizmorBase() *dagger.Container {
	c := dag.Container().From(zizmorImage).
		WithMountedCache("/zizmor-cache", dag.CacheVolume("zizmor"))
	if m.GhAuthToken != nil {
		c = c.WithSecretVariable("ZIZMOR_GITHUB_TOKEN", m.GhAuthToken)
	}
	return c
}

// actionsUpBase is a Node container, before the source tree is mounted.
func (m *Envoke) actionsUpBase() *dagger.Container {
	c := dag.Container().From(nodeImage)
	if m.GhAuthToken != nil {
		c = c.WithSecretVariable("GITHUB_TOKEN", m.GhAuthToken)
	}
	return c
}

// Zizmor lints GitHub Actions workflows for common security issues
// (unpinned actions, excessive permissions, credential persistence, etc.)
// via https://github.com/zizmorcore/zizmor. Deliberately not a +check: the
// `check` CLI verb never forwards constructor flags, so a
// token-authenticated run only works via `dagger call
// zizmor --gh-auth-token=...`; keeping it out of `+check` means CI's
// unauthenticated `dagger check` run never needs to know about it either.
func (m *Envoke) Zizmor(ctx context.Context) error {
	_, err := m.withSource(m.zizmorBase()).
		WithExec([]string{"zizmor", ".", "--cache-dir=/zizmor-cache"}).
		Sync(ctx)
	return err
}

// actionsUpReport is the subset of `actions-up --json`'s output this
// function reads to decide pass/fail.
type actionsUpReport struct {
	Summary struct {
		TotalUpdates int `json:"totalUpdates"`
	} `json:"summary"`
}

// ActionsUp checks that every `uses:` reference under .github is pinned to
// a commit SHA and at its latest compatible version, via
// https://github.com/azat-io/actions-up. On failure it re-runs verbosely so
// the error message shows exactly what's out of date. Deliberately not a
// +check, for the same reason as Zizmor above.
func (m *Envoke) ActionsUp(ctx context.Context) error {
	c := m.withSource(m.actionsUpBase())

	out, err := c.WithExec([]string{"npx", "-y", "actions-up", "--dry-run", "--json"}).Stdout(ctx)
	if err != nil {
		return err
	}

	var report actionsUpReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		return fmt.Errorf("parsing actions-up JSON output: %w", err)
	}
	if report.Summary.TotalUpdates == 0 {
		return nil
	}

	verbose, verboseErr := c.WithExec([]string{"npx", "-y", "actions-up", "--dry-run", "--yes"}).Stdout(ctx)
	if verboseErr != nil {
		verbose = verboseErr.Error()
	}
	return fmt.Errorf("%d GitHub Actions reference(s) need updates:\n\n%s", report.Summary.TotalUpdates, verbose)
}

// Autofix applies every automatic fix zizmor and actions-up know how to make:
// zizmor's `--fix=all` first, then actions-up against the fixed tree,
// returning the combined diff. Apply it with:
//
//	dagger -m .dagger call autofix export --path=.
//
// ReturnTypeAny because `zizmor --fix=all` exits non-zero whenever findings
// remain after fixing — ones needing the riskier `--fix=unsafe`, which this
// deliberately doesn't pass — even though the safe fixes did apply.
func (m *Envoke) Autofix(ctx context.Context) (*dagger.Changeset, error) {
	zizmorFixed := m.withSource(m.zizmorBase()).
		WithExec([]string{"zizmor", ".", "--cache-dir=/zizmor-cache", "--fix=all"}, dagger.ContainerWithExecOpts{
			Expect: dagger.ReturnTypeAny,
		}).
		Directory("/src")

	actionsUpFixed := m.actionsUpBase().
		WithMountedDirectory("/src", zizmorFixed).
		WithWorkdir("/src").
		WithExec([]string{"npx", "-y", "actions-up", "--yes"}).
		Directory("/src")

	changes := actionsUpFixed.Changes(m.Source)
	if _, err := changes.Sync(ctx); err != nil {
		return nil, err
	}
	return changes, nil
}

// TestShellBash runs the shell end-to-end tests against a real bash
// (already present in the base Go image).
//
// +check
func (m *Envoke) TestShellBash(ctx context.Context) error {
	return m.shellTest(ctx, m.goBase())
}

// TestShellZsh runs the shell end-to-end tests against a real zsh.
//
// +check
func (m *Envoke) TestShellZsh(ctx context.Context) error {
	return m.shellTest(ctx, m.aptInstall("zsh"))
}

// TestShellFish runs the shell end-to-end tests against a real fish.
//
// +check
func (m *Envoke) TestShellFish(ctx context.Context) error {
	return m.shellTest(ctx, m.aptInstall("fish"))
}

// TestShellTcsh runs the shell end-to-end tests against a real tcsh.
//
// +check
func (m *Envoke) TestShellTcsh(ctx context.Context) error {
	return m.shellTest(ctx, m.aptInstall("tcsh"))
}

// TestShellPowershell runs the shell end-to-end tests against a real
// pwsh.
//
// +check
func (m *Envoke) TestShellPowershell(ctx context.Context) error {
	return m.shellTest(ctx, m.powershellBase())
}

// Snapshot runs the full goreleaser pipeline for every OS/arch in
// .goreleaser.yaml and returns dist/ for local inspection. `--snapshot`
// implies `--skip=announce,publish,validate`, so nothing leaves the
// container; `sign` is skipped explicitly because --snapshot does not skip it
// and cosign's keyless signing then hangs for the OIDC device flow's full
// timeout waiting for a GitHub Actions token a local run never has.
func (m *Envoke) Snapshot(ctx context.Context) (*dagger.Directory, error) {
	dist := m.withGitSource(m.goreleaserBase()).
		WithExec([]string{"goreleaser", "release", "--snapshot", "--clean", "--skip=sign"}).
		Directory("/src/dist")
	if _, err := dist.Sync(ctx); err != nil {
		return nil, err
	}
	return dist, nil
}

// Publish builds and publishes a real GitHub Release via goreleaser:
// cross-platform archives plus a checksums.txt attached to the current git
// tag, keylessly signed with cosign (see .goreleaser.yaml's signs: block).
// githubToken needs write access to github.com/Neirda24/envoke only (the
// ambient per-run GITHUB_TOKEN covers this) — a tag must be checked out in
// Source, goreleaser release refuses to run otherwise.
//
// actionsIDTokenRequestURL/actionsIDTokenRequestToken are GitHub Actions' OIDC
// endpoint and its bearer credential. The runner injects both into every
// step's environment once the job has `permissions: id-token: write`, but a
// Dagger container is isolated from that ambient environment, so cosign only
// finds them if they are forwarded explicitly.
//
// homebrewTapToken authenticates the homebrew_casks and scoops pushes — a
// short-lived GitHub App installation token scoped to those two repositories
// alone, minted per run by release.yml, so a compromise of the release job
// cannot reach beyond them.
func (m *Envoke) Publish(ctx context.Context, githubToken *dagger.Secret, actionsIDTokenRequestURL *dagger.Secret, actionsIDTokenRequestToken *dagger.Secret, homebrewTapToken *dagger.Secret) (string, error) {
	return m.withGitSource(m.goreleaserBase()).
		WithSecretVariable("GITHUB_TOKEN", githubToken).
		WithSecretVariable("ACTIONS_ID_TOKEN_REQUEST_URL", actionsIDTokenRequestURL).
		WithSecretVariable("ACTIONS_ID_TOKEN_REQUEST_TOKEN", actionsIDTokenRequestToken).
		WithSecretVariable("HOMEBREW_TAP_GITHUB_TOKEN", homebrewTapToken).
		WithExec([]string{"goreleaser", "release", "--clean"}).
		Stdout(ctx)
}

// DocsBuild builds the docs site the way the deploy does, and fails on
// anything MkDocs would otherwise only mention.
//
// --strict does the work: mkdocs.yml raises validation.nav.omitted_files to a
// warning, so a page no nav entry lists would otherwise publish at its own URL
// with the run still green. A broken internal link is the same shape.
//
// A check because the deploy workflow is the only other place the strict build
// runs, and it runs on push to main behind a paths filter.
//
// +check
func (m *Envoke) DocsBuild(ctx context.Context) error {
	_, err := m.withSource(m.docsBase()).
		WithExec([]string{"mkdocs", "build", "--strict"}).
		Sync(ctx)
	return err
}

// Docs starts a live-reloading mkdocs dev server for the docs/ site, bound
// to docsPort. Run it with:
//
//	dagger -m ./.dagger call docs up --ports 8000:8000
//
// then open http://localhost:8000 — edits under docs/ or mkdocs.yml
// live-reload without restarting the service.
func (m *Envoke) Docs() *dagger.Service {
	return m.withSource(m.docsBase()).
		WithExposedPort(docsPort).
		AsService(dagger.ContainerAsServiceOpts{Args: []string{"mkdocs", "serve", "-a", fmt.Sprintf("0.0.0.0:%d", docsPort)}})
}

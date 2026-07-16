// Dagger CI pipeline for envoke.
//
// Mirrors the four commands from CONTRIBUTING.md's "Verifying your change"
// (gofmt, go vet, go build, go test -race), adds golangci-lint, and runs the
// shellinit end-to-end tests against a real interpreter for each of the five
// supported shells (bash, zsh, fish, tcsh, powershell) so none of them are
// silently skipped for lack of the binary, as they are in a plain local
// `go test` run.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"dagger/envoke/internal/dagger"
)

const (
	// renovate: datasource=docker
	goImage          = "golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651"
	golangciLintPath = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2"
	// renovate: datasource=docker
	goreleaserImage = "goreleaser/goreleaser:v2.17.0@sha256:054eefd282c02233a2556ce2d1a60cd2f51dc565ffc2520dc38b5deb4dd1ad30"
	// renovate: datasource=docker
	pythonImage = "python:3.14-slim@sha256:d3400aa122fa42cf0af0dbe8ec3091b047eac5c8f7e3539f7135e86d855dc015"
	// zizmorImage intentionally tracks :latest, not a pinned tag — no renovate hint.
	zizmorImage = "ghcr.io/zizmorcore/zizmor:latest"
	// renovate: datasource=docker
	nodeImage = "node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2"
	docsPort  = 8000
)

type Envoke struct {
	// Source is the envoke repository root.
	Source *dagger.Directory
	// GhAuthToken authenticates zizmor's and actions-up's GitHub API calls,
	// raising their unauthenticated 60/hr rate limit. Optional: both tools
	// still work without it, just slower/flakier on a busy day.
	GhAuthToken *dagger.Secret
}

func New(
	// +defaultPath="/"
	source *dagger.Directory,
	// +optional
	ghAuthToken *dagger.Secret,
) *Envoke {
	return &Envoke{Source: source, GhAuthToken: ghAuthToken}
}

// goBase is a bare Go toolchain container, before the source tree is
// mounted, so package-install layers cache independently of source edits.
func (m *Envoke) goBase() *dagger.Container {
	// CGO must stay enabled: go test -race requires cgo.
	return dag.Container().From(goImage)
}

func (m *Envoke) withSource(c *dagger.Container) *dagger.Container {
	return c.WithMountedDirectory("/src", m.Source).WithWorkdir("/src")
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

// shellinitTest runs the shellinit package's tests (which drive a real
// interpreter end to end rather than string-match generated scripts) inside
// the given container.
func (m *Envoke) shellinitTest(ctx context.Context, c *dagger.Container) error {
	_, err := m.withSource(c).
		WithExec([]string{"go", "test", "./internal/shellinit/...", "-race", "-v"}).
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

// Test runs the full test suite with the race detector.
//
// +check
func (m *Envoke) Test(ctx context.Context) error {
	_, err := m.withSource(m.goBase()).
		WithExec([]string{"go", "test", "./...", "-race"}).
		Sync(ctx)
	return err
}

// Lint runs golangci-lint.
//
// +check
func (m *Envoke) Lint(ctx context.Context) error {
	_, err := m.withSource(m.goBase()).
		// golangci-lint's own go.mod may require a newer Go than this
		// module's; let the toolchain fetch itself rather than pinning
		// the base image to whatever golangci-lint currently needs.
		WithEnvVariable("GOTOOLCHAIN", "auto").
		WithExec([]string{"go", "install", golangciLintPath}).
		WithExec([]string{"golangci-lint", "run", "./..."}).
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
// `check` CLI verb never forwards constructor flags (confirmed — see
// CLAUDE.md), so a token-authenticated run only works via `dagger call
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

// Autofix applies every automatic fix zizmor and actions-up know how to
// make: zizmor's own `--fix=all` first, then actions-up's pinning/update
// pass against the zizmor-fixed tree, returning the combined diff. Apply it
// locally with:
//
//	dagger -m .dagger call autofix export --path=.
//
// then re-run the Zizmor/ActionsUp checks to confirm only genuinely
// unfixable issues (if any) remain: `zizmor --fix=all` exits non-zero
// whenever findings remain after fixing (e.g. ones needing the riskier
// `--fix=unsafe`, which this deliberately doesn't pass), even though the
// safe fixes were applied successfully — so this step must tolerate any
// exit code rather than the default success-only expectation.
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

// TestShellBash runs the shellinit end-to-end tests against a real bash
// (already present in the base Go image).
//
// +check
func (m *Envoke) TestShellBash(ctx context.Context) error {
	return m.shellinitTest(ctx, m.goBase())
}

// TestShellZsh runs the shellinit end-to-end tests against a real zsh.
//
// +check
func (m *Envoke) TestShellZsh(ctx context.Context) error {
	return m.shellinitTest(ctx, m.aptInstall("zsh"))
}

// TestShellFish runs the shellinit end-to-end tests against a real fish.
//
// +check
func (m *Envoke) TestShellFish(ctx context.Context) error {
	return m.shellinitTest(ctx, m.aptInstall("fish"))
}

// TestShellTcsh runs the shellinit end-to-end tests against a real tcsh.
//
// +check
func (m *Envoke) TestShellTcsh(ctx context.Context) error {
	return m.shellinitTest(ctx, m.aptInstall("tcsh"))
}

// TestShellPowershell runs the shellinit end-to-end tests against a real
// pwsh.
//
// +check
func (m *Envoke) TestShellPowershell(ctx context.Context) error {
	return m.shellinitTest(ctx, m.powershellBase())
}

// Snapshot runs the full goreleaser pipeline (cross-compile, archive,
// checksum) for every OS/arch in .goreleaser.yaml, but `--snapshot` implies
// `--skip=announce,publish,validate` so nothing leaves this container.
// `sign` is skipped explicitly too, on top of that: unlike the others,
// --snapshot does NOT skip it automatically (confirmed by running this
// function before this fix existed — it hung for the OIDC device-flow's
// full 5-minute timeout, then failed, since cosign's keyless signing needs
// a real GitHub Actions OIDC token that a local/snapshot run never has).
// Returns the dist/ directory for local inspection.
func (m *Envoke) Snapshot(ctx context.Context) (*dagger.Directory, error) {
	dist := m.withSource(m.goreleaserBase()).
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
// Requires a GITHUB_TOKEN secret with write access to
// github.com/Neirda24/envoke, and a tag checked out in Source (goreleaser
// release refuses to run otherwise).
//
// actionsIDTokenRequestURL/actionsIDTokenRequestToken are GitHub Actions'
// OIDC token-minting endpoint and its bearer credential
// (ACTIONS_ID_TOKEN_REQUEST_URL/ACTIONS_ID_TOKEN_REQUEST_TOKEN) — the
// runner injects both automatically into every step's process environment
// once the job has `permissions: id-token: write`, but a Dagger container
// is isolated from that ambient environment by design, so they have to be
// forwarded explicitly for cosign's GitHub Actions ambient-credential
// detection to find them and mint a Fulcio certificate. Threaded the same
// way as githubToken (see release.yml's `call: publish
// --github-token=... --actions-id-token-request-url=env://... `).
func (m *Envoke) Publish(ctx context.Context, githubToken *dagger.Secret, actionsIDTokenRequestURL *dagger.Secret, actionsIDTokenRequestToken *dagger.Secret) (string, error) {
	return m.withSource(m.goreleaserBase()).
		WithSecretVariable("GITHUB_TOKEN", githubToken).
		WithSecretVariable("ACTIONS_ID_TOKEN_REQUEST_URL", actionsIDTokenRequestURL).
		WithSecretVariable("ACTIONS_ID_TOKEN_REQUEST_TOKEN", actionsIDTokenRequestToken).
		WithExec([]string{"goreleaser", "release", "--clean"}).
		Stdout(ctx)
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

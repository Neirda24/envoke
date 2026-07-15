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
	"fmt"

	"dagger/envoke/internal/dagger"
)

const (
	goImage          = "golang:1.23-bookworm"
	golangciLintPath = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2"
	goreleaserImage  = "goreleaser/goreleaser:v2.17.0"
	pythonImage      = "python:3.12-slim"
	docsPort         = 8000
)

type Envoke struct {
	// Source is the envoke repository root.
	Source *dagger.Directory
}

func New(
	// +defaultPath="/"
	source *dagger.Directory,
) *Envoke {
	return &Envoke{Source: source}
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
// Returns the dist/ directory for local inspection.
func (m *Envoke) Snapshot(ctx context.Context) (*dagger.Directory, error) {
	dist := m.withSource(m.goreleaserBase()).
		WithExec([]string{"goreleaser", "release", "--snapshot", "--clean"}).
		Directory("/src/dist")
	if _, err := dist.Sync(ctx); err != nil {
		return nil, err
	}
	return dist, nil
}

// Publish builds and publishes a real GitHub Release via goreleaser:
// cross-platform archives plus a checksums.txt attached to the current git
// tag. Requires a GITHUB_TOKEN secret with write access to
// github.com/Neirda24/envoke, and a tag checked out in Source (goreleaser
// release refuses to run otherwise).
func (m *Envoke) Publish(ctx context.Context, githubToken *dagger.Secret) (string, error) {
	return m.withSource(m.goreleaserBase()).
		WithSecretVariable("GITHUB_TOKEN", githubToken).
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

# Security Policy

`envoke` is a small, personal open-source project. This policy is intentionally
lightweight, but the tool's threat model is worth stating plainly: `envoke`
executes shell scripts automatically on `cd` once a config is trusted (see
[CLAUDE.md](CLAUDE.md)'s trust model), and its release pipeline builds and
distributes the binary itself — a vulnerability in either has real impact on
anyone who's installed it.

## Reporting a vulnerability

Please report security issues privately rather than opening a public GitHub
issue, so there's time to fix the problem before it's disclosed.

- Preferred: open a [GitHub Security Advisory](https://github.com/Neirda24/envoke/security/advisories/new) for this repository (private by default, visible only to the maintainer until published).
- Alternative: email adrien@roc-it.tech with a description of the issue, steps to reproduce, and its potential impact.

This is a one-person project, so there's no formal SLA — expect an initial
response within a few days. Please don't publicly disclose the issue until a
fix is released.

## Secrets and tokens

The release pipeline (`.github/workflows/release.yml`, `.dagger/main.go`'s
`Publish` function) uses the following secrets:

- **`PUBLISH_HOMEBREW_TAP`** — a personal access token (PAT) with write
  access to *both* `Neirda24/envoke` (to create the GitHub Release) and
  `Neirda24/homebrew-tap` (to push the updated Cask). This is a broader
  scope than a fine-grained GitHub App would need, and is a known,
  deliberate tradeoff (see `security_audit.md`'s Finding 6) rather than an
  oversight — a scoped GitHub App migration is a possible future hardening
  step. **Rotation policy**: rotate immediately if compromise is suspected
  (e.g. an unexpected push to `homebrew-tap`, or a leaked CI log); absent
  any incident, rotate at least annually as a baseline.
- **`GITHUB_TOKEN`** — the default per-run token every GitHub Actions job
  gets automatically, used to authenticate `zizmor`/`actions-up` against the
  GitHub API (raising their unauthenticated 60/hr rate limit). Scoped to
  the repository the workflow runs in; expires with the run.

If you believe either secret has been exposed or misused, please report it
per the process above.

## Verifying release artifacts

Starting with the release that first ships this file, `checksums.txt`
attached to each [GitHub Release](https://github.com/Neirda24/envoke/releases)
is signed keylessly with [cosign](https://docs.sigstore.dev/cosign/) via
GitHub Actions OIDC (Sigstore/Fulcio) — no long-lived private signing key
exists anywhere. See the "Verifying releases" section of each release's
notes (generated from `.goreleaser.yaml`'s `release.footer`) for the exact
`cosign verify-blob` command. A successful verification proves
`checksums.txt`, and transitively every archive it hashes, was built by this
repository's own `release.yml` workflow and hasn't been tampered with.

Signature verification is a hardening measure on top of, not a replacement
for, GitHub's own HTTPS-delivered release assets — treat a verification
failure as a reason to stop and investigate, not a formality.

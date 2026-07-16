# Security Policy

`envoke` executes shell scripts automatically when you `cd`, once a config has
been explicitly approved via `envoke allow`. That trust step is the core of
the tool's threat model — see the [Trust Model](https://neirda24.github.io/envoke/trust/)
docs for how it works.

## Reporting a vulnerability

Please report security issues privately rather than opening a public GitHub
issue, so there's time to fix the problem before it's disclosed.

Open a [GitHub Security Advisory](https://github.com/Neirda24/envoke/security/advisories/new)
for this repository — private by default, visible only to the maintainer
until published.

This is a one-person project with no formal SLA; expect an initial response
within a few days. Please don't disclose publicly until a fix has shipped.

## Verifying release artifacts

Every release's `checksums.txt` is signed keylessly with
[cosign](https://docs.sigstore.dev/cosign/) via GitHub Actions OIDC
(Sigstore/Fulcio) — no long-lived private signing key exists anywhere. Each
[release](https://github.com/Neirda24/envoke/releases)'s notes include the
exact `cosign verify-blob` command to check it.

Treat a failed verification as a reason to stop and investigate, not a
formality — it's a hardening layer on top of, not a replacement for, GitHub's
own HTTPS-delivered release assets.

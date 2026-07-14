# Trust Model

Any config that runs arbitrary shell code on `cd` needs an opt-in step before it executes for the first time — direnv-style (`direnv allow`). ondir has no such mechanism: any `~/.ondirrc` runs unconditionally, which means `cd`ing into a directory with a malicious or accidentally-broken config runs its script with no warning. `envoke` requires an explicit approval before a new or changed config block is executed.

## Approving a config

```sh
envoke allow            # trusts config.Locate()'s resolved path
envoke allow /path/to/config   # trusts an explicit path
```

`envoke allow` refuses to trust a config that doesn't even parse — you can't accidentally approve something broken.

## How trust is tracked

Trust is a SHA-256 hash of the config file's **content**, recorded under `$XDG_DATA_HOME/envoke/allow/<sha256(abs path)>` (or `~/.local/share/envoke/allow/...` if `$XDG_DATA_HOME` isn't set) — one record file per config path, so distinct configs never collide.

When `envoke shell-hook` runs, it recomputes the current file's content hash and compares it to the trusted record:

- **Match** → the resolved blocks execute (via `executor.Render`, `eval`'d by your shell).
- **No match, or no record at all** → nothing executes. envoke reports the untrusted match on stderr only (never stdout), along with an `envoke allow <path>` hint, and stops there.

Any edit to the config — even whitespace — changes the content hash and revokes trust until you run `envoke allow` again. This means there's no way to silently smuggle a change into an already-trusted config; every modification requires a fresh, explicit approval.

## Why this is non-negotiable

Trust-before-execution is one of envoke's core design principles: no code path is allowed to auto-execute an unapproved config, including "convenience" paths. If you're ever unsure what a config would do before trusting it, use [`envoke debug`](debugging.md) — it reports matches without executing anything, trusted or not.

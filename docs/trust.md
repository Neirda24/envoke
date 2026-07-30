# Trust Model

Any config that runs arbitrary shell code on `cd` needs an opt-in step before it executes for the first time — direnv-style (`direnv allow`). ondir has no such mechanism: any `~/.ondirrc` runs unconditionally, which means `cd`ing into a directory with a malicious or accidentally-broken config runs its script with no warning. `envoke` requires an explicit approval before a new or changed config block is executed.

## Approving a config

```sh
envoke allow            # trusts config.Locate()'s resolved path
envoke allow /path/to/config   # trusts an explicit path
```

`envoke allow` refuses to trust a config that doesn't even parse — you can't accidentally approve something broken.

Before recording trust, `envoke allow` shows you what you're about to approve (see below for the first-time-vs-re-approval cases) and then asks for confirmation:

```
envoke: trust and run these blocks on every matching cd? [y/N]
```

Only `y` or `yes` (case-insensitive) proceeds; anything else — a different answer, an empty line, or closing stdin (EOF) — aborts without trusting anything:

```
$ envoke allow
envoke: about to trust /home/you/.envokerc -- review each block below before confirming:

  enter ~/Projects/([^/]+) (line 1)
    source venv/bin/activate

  leave ~/Projects/([^/]+) (line 4)
    deactivate

envoke: trust and run these blocks on every matching cd? [y/N] y
envoke: trusted /home/you/.envokerc
```

Answering anything but `y`/`yes` prints `envoke: aborted, not trusted` to stderr and exits non-zero, leaving the config untrusted.

For non-interactive use — dotfiles bootstrap scripts, CI, provisioning — pass `--yes` (or `-y`) to skip the prompt and trust immediately, the same as answering `y`:

```sh
envoke allow --yes
envoke allow -y /path/to/config
```

The flag may come before or after the path — `envoke allow --yes ~/.envokerc` and `envoke allow ~/.envokerc --yes` both work.

## Seeing and withdrawing trust

```sh
envoke list                    # what's trusted, and whether it still matches
envoke revoke                  # withdraw trust for the located config
envoke revoke /path/to/config  # ...or an explicit one
envoke prune                   # drop records whose config no longer exists
```

`envoke list` prints one line per record, with the status of the config as
it exists right now:

```
$ envoke list
  trusted   /home/you/.envokerc
  changed   /home/you/work/envokerc
  missing   /home/you/old-project/envokerc
```

- **trusted** — the file's current content is what you approved, so
  `shell-hook` will act on it.
- **changed** — it has been edited since; nothing runs until you
  `envoke allow` it again.
- **missing** — the config file is gone, but its record (and the copy of its
  content, see below) is still in the store. `envoke prune` clears those.

`envoke revoke` puts a config back to needing an explicit approval, without
having to edit it or delete files out of the store by hand. Revoking
something that wasn't trusted is a no-op, not an error.

!!! note "Records approved by an older envoke"

    Records written before envoke started storing the config's path can't be
    resolved back to a file. `envoke list` shows them as `unknown` with their
    store path, and `envoke prune` deliberately leaves them alone rather than
    guessing — re-run `envoke allow` on the config to replace such a record,
    or delete the file it names.

### The store keeps a copy of what you approved

`envoke allow` writes the approved content into the trust store so it can
show you a diff next time. That is a **plaintext second copy** of your
config, and since exporting project-scoped secrets is one of envoke's main
uses, that copy may well contain them. It's written `0600` in a `0700`
directory, but it does mean deleting a config isn't the whole story:

```sh
envoke revoke /path/to/config   # removes the record and its content copy
envoke prune                    # same, for configs already deleted
```

## Re-approving a changed config

What `envoke allow` shows you before the confirmation prompt depends on whether the config was trusted before, and whether it's changed since:

- **First time trusting this config** — the full block-by-block dump shown above: every block's type, pattern, source line, and script body.
- **Trusted before, content byte-for-byte unchanged** — nothing to review. `envoke allow` prints a one-line status and returns immediately, without prompting and without touching the trust record again (it's already trusted):
  ```
  $ envoke allow
  envoke: /home/you/.envokerc is unchanged since it was last trusted -- nothing to review
  ```
  `--yes` is a no-op here, since there's no prompt to skip.
- **Trusted before, content changed** — a line-level diff against the previously-approved content, instead of the full dump, so a small edit to an already-trusted config doesn't require re-reading the whole file:
  ```
  $ envoke allow
  envoke: /home/you/.envokerc changed since it was last trusted -- here's what's different:

  -     echo old-line
  +     echo new-line

  envoke: trust and run these blocks on every matching cd? [y/N]
  ```
  Lines prefixed `-` were removed, lines prefixed `+` were added — the same convention as `diff -u`/`git diff`. Unchanged lines are omitted entirely. The `[y/N]` prompt (or `--yes`) still applies in this case, same as first-time trust.

## How trust is tracked

Trust is a SHA-256 hash of the config file's **content**, recorded under `$XDG_DATA_HOME/envoke/allow/<sha256(abs path)>` (or `~/.local/share/envoke/allow/...` if `$XDG_DATA_HOME` isn't set) — one record per config path, so distinct configs never collide. Each record is three files:

| File | Holds | Used for |
|---|---|---|
| `<sha256(abs path)>` | the approved content's hash | the trust decision itself |
| `<sha256(abs path)>.content` | a copy of the approved content | the diff on re-approval |
| `<sha256(abs path)>.path` | the config's absolute path | `envoke list` / `envoke prune` |

The two siblings were added after the hash file and are optional on read, so upgrading envoke never revokes an existing approval — a record with no siblings is a normal state, not corruption. The hash file is always written last, and every file is written atomically, so an interrupted write leaves the config *untrusted* rather than trusted against content it doesn't describe.

When `envoke shell-hook` runs, it recomputes the current file's content hash and compares it to the trusted record:

- **Match** → the resolved blocks execute (via `executor.Render`, `eval`'d by your shell).
- **No match, or no record at all** → nothing executes. envoke reports the untrusted match on stderr only (never stdout), along with an `envoke allow <path>` hint, and stops there.

Any edit to the config — even whitespace — changes the content hash and revokes trust until you run `envoke allow` again. This means there's no way to silently smuggle a change into an already-trusted config; every modification requires a fresh, explicit approval.

### The config is read exactly once per command

Both `envoke allow` and `envoke shell-hook` read the config file a single
time and use those same bytes for everything they do with it — parsing it,
showing it to you, hashing it, and rendering it into your shell. That is a
security property, not an implementation detail: reading the file more than
once would open a window between the read that gets *validated* and the read
that gets *executed*, so a config could be run in one version while being
approved in another. On a config another local user can write to — exactly
what the permission warning below is about — that window is reachable, so
the trust check operates on bytes already in hand rather than on a path it
re-opens.

## File permission warnings

Content-hash revocation protects you from *silently* running a config that
changed since you last trusted it — but on a shared machine (multi-user
box, NFS home), nothing stops another local user from editing a config
you've already approved. `envoke allow`, `envoke shell-hook`, and `envoke
debug` all check whether the config file is writable by anyone other than
its owner (group or other write bits set) and print a non-fatal warning to
stderr if so:

```
envoke: warning: /home/you/.envokerc is writable by group/other (mode 664) -- consider tightening its permissions
```

This is a warning, not a block — fix it with `chmod go-w ~/.envokerc` if you
see it unexpectedly.

The same check runs against the **trust store directory** itself, and that
one matters more:

```
envoke: warning: the trust store /home/you/.local/share/envoke/allow is writable by group/other (mode 777) -- anyone who can write there can forge an approval; run `chmod go-w ...`
```

A writable config can be tampered with, but the tampering revokes its own
trust — the content hash stops matching. A writable *store* lets someone
drop in a record that makes any config read as trusted, forging an approval
you never gave. envoke creates the store `0700`, but that only applies to
directories it actually creates: a pre-existing `~/.local/share` tree, or an
`$XDG_DATA_HOME` with loose permissions, keeps whatever mode it already had.

## Directory names are never executed

The trust model only means something if the *only* code envoke can run is
code you approved. A directory name is attacker-controllable in ordinary
situations — an extracted archive, a cloned repository, a shared or NFS home
— so no shell hook may ever let one reach a shell parser as code.

Every generated hook passes the two directories to `envoke shell-hook`
without any string interpolation into something that gets re-parsed. tcsh is
the awkward one: its `cwdcmd` alias can only pipe into `source` from inside
an `eval`, and `eval` re-parses its argument. The hook therefore keeps that
`eval` string a fixed constant and passes the directories through the
environment instead:

```tcsh
setenv ENVOKE_FROM "$owd" ; setenv ENVOKE_TO "$cwd" ; eval "\envoke shell-hook --shell tcsh | source /dev/stdin" ; unsetenv ENVOKE_FROM ; unsetenv ENVOKE_TO
```

`envoke shell-hook` reads `$ENVOKE_FROM`/`$ENVOKE_TO` when it is given no
positional arguments; explicit arguments always take precedence. This is
covered by a cross-shell regression test that `cd`s a real bash, zsh, fish,
tcsh and PowerShell into a directory whose name is packed with shell
metacharacters and asserts nothing was executed.

## Why this is non-negotiable

Trust-before-execution is one of envoke's core design principles: no code path is allowed to auto-execute an unapproved config, including "convenience" paths. If you're ever unsure what a config would do before trusting it, use [`envoke debug`](debugging.md) — it reports matches without executing anything, trusted or not.

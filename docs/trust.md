# Trust Model

Any config that runs arbitrary shell code on `cd` needs an opt-in step before it executes for the first time — direnv-style (`direnv allow`). ondir has no such mechanism: any `~/.ondirrc` runs unconditionally, which means `cd`ing into a directory with a malicious or accidentally-broken config runs its script with no warning. `envoke` requires an explicit approval before a new or changed config block is executed.

**Every config file is approved separately**, including each `envokerc.d`
fragment. Approving your central config says nothing about a fragment
symlinked in from a repository you cloned.

## Nothing is discovered; everything is approved

envoke only ever loads configs from **your own config directory** — the
central config, and the files in `envokerc.d`. It does not read a config
because you walked into the directory holding it, so no file you did not put
there can ever ask to be trusted.

That is why there is no prompt on the way in. An unapproved config is
reported and skipped:

```
envoke: 1 block(s) matched for /work -> /work/api but /home/you/.config/envoke/envokerc.d/10-work is not trusted: run `envoke allow /home/you/.config/envoke/envokerc.d/10-work`
```

A config committed inside a project joins the set only through a symlink you
create yourself (see
[Bringing a project's own config in](configuration.md#bringing-a-projects-own-config-in)).
Its content still has to be approved, and it is confined to the project's own
directory tree — a `git pull` that rewrites it revokes its approval and cannot
widen where it applies. That bound is not left implicit either: `envoke allow`'s
review states it before asking you to confirm, and `envoke debug` prints it
under the config's status line.

Text printed from a config that has not been approved — patterns, script
bodies, its path, and any error quoting them — is escaped first, so an escape
sequence in a file or a directory name cannot redraw what you are reading.

## Approving a config

```sh
envoke allow                   # every config envoke would load
envoke allow /path/to/config   # just that one
```

With no path, `envoke allow` covers the whole set: the central config plus
every `envokerc.d` fragment. Splitting rules across files is an organisational
choice, not a decision to approve them one at a time — you review each in turn
and answer once, as the second transcript below shows.

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
    source "$ENVOKE_DIR/venv/bin/activate"

  leave ~/Projects/([^/]+) (line 4)
    deactivate

envoke: trust and run these blocks on every matching cd? [y/N] y
envoke: trusted /home/you/.envokerc
envoke: to apply it to this shell without leaving the directory: eval "$(envoke reload)"
```

Answering anything but `y`/`yes` prints `envoke: aborted, not trusted` to stderr and exits non-zero, leaving the config untrusted.

That last line is there because `envoke allow` is a child process of your shell and cannot export anything into it — what you just approved applies from your next `cd` onwards. See [Applying a config without leaving the directory](debugging.md#applying-a-config-without-leaving-the-directory).

With a set, the shape is the same and there is still **one** prompt. Every
config is reviewed first, in set order, and the `[y/N]` at the end covers
everything still pending:

```
$ envoke allow
envoke: about to trust /home/you/.envokerc -- review each block below before confirming:

  enter ~/work/([^/]+) (line 1)
    . "$ENVOKE_DIR/venv/bin/activate"

envoke: /home/you/.config/envoke/envokerc.d/10-work is unchanged since it was last trusted -- nothing to review
envoke: /home/you/.config/envoke/envokerc.d/20-python: line 3: enter ~/Projects has no script body
envoke: trust and run these blocks on every matching cd? [y/N] y
envoke: trusted /home/you/.envokerc
envoke: to apply it to this shell without leaving the directory: eval "$(envoke reload)"
```

Three things to read there: the config already trusted verbatim is reported and
skipped, since there is nothing to confirm about it; the one that doesn't parse
is reported on stderr and skipped, without stopping the others; and answering
`y` trusts everything that *was* pending. The exit code is 1 all the same,
because something was skipped — see
[Exit codes](reference.md#exit-codes).

For non-interactive use — dotfiles bootstrap scripts, CI, provisioning — pass `--yes` (or `-y`) to skip the prompt and trust immediately, the same as answering `y`:

```sh
envoke allow --yes
envoke allow -y /path/to/config
```

The flag may come before or after the path — `envoke allow --yes ~/.envokerc` and `envoke allow ~/.envokerc --yes` both work.

### Reviewing a symlinked project fragment

A fragment symlinked in from a project is reviewed **through the link**: the
file envoke parses, hashes and prints for you is the target, since that is the
file that will be loaded on every `cd`. Approving content whose displayed
meaning differs from its effective one would approve nothing. So the review
names the target, and states the tree the config's blocks are
[confined](configuration.md#bringing-a-projects-own-config-in) to, before the
prompt:

```
$ envoke allow
envoke: about to trust /home/you/.config/envoke/envokerc.d/api -- review each block below before confirming:

  enter ./src (line 1)
    export SRC=1

  note: symlink to /home/you/work/api/envoke.conf
  note: confined to /home/you/work/api -- its blocks cannot match outside that directory, whatever their patterns say

envoke: trust and run these blocks on every matching cd? [y/N]
```

Both notes appear on the diff shown for a config that changed, too — which is
the path a `git pull` that rewrote a project's config actually takes, and the
one where the bound matters most. For your central config, or an ordinary file
in `envokerc.d`, there is no link to resolve and no bound to state, so neither
note appears. `envoke debug` reports the same two facts under each
config's status line, which is where to look when a config reads `trusted` and
still fires nothing — see [Debugging](debugging.md).

## Seeing and withdrawing trust

```sh
envoke list                    # what's trusted, and whether it still matches
envoke revoke                  # withdraw trust for the whole set
envoke revoke /path/to/config  # ...or for one config
envoke prune                   # drop records whose config no longer exists
```

`revoke` is `allow` backwards, defaults included: with no path it covers the
same whole set `envoke allow` does — the central config plus every
`envokerc.d` fragment — and with a path, only that file. Neither command has a
different idea of what "the set" is, so approving a set and then withdrawing it
leaves nothing behind. What neither touches is a record for a config *outside*
the set; those are the second half of `envoke list`'s output, and `envoke
prune` is what clears the ones whose file is gone.

`envoke list` answers two questions that are not the same: **what envoke would
load**, and **what the store has recorded**. A config can be loaded with no
record at all — that is a file being skipped on every `cd` — and a record can
outlive the config it was written for. So the two are listed separately:

```
$ envoke list
envoke: configs envoke would load
  trusted   central   /home/you/.envokerc
  changed   fragment  /home/you/.config/envoke/envokerc.d/10-work
  untrusted fragment  /home/you/.config/envoke/envokerc.d/20-python

envoke: other trust records (not in the current config set)
  missing             /home/you/old-project/envokerc
```

For a config in the set, the status is what would happen to it on your next
`cd`:

- **trusted** — the file's current content is what you approved, so its blocks
  will run.
- **changed** — approved before, edited since. Nothing runs until you
  `envoke allow` it again, and you'll get a diff rather than a full re-read.
- **untrusted** — never approved. Nothing runs until you review it.
- **missing** / **unreadable** — the file was listed but couldn't be read. For
  a fragment that usually means a broken symlink.

The second section is everything else the store holds. It is not an error list:
a record for a config you keep under a different `$ENVOKERC`, or one you have
since split into fragments, belongs there legitimately. But it is where a stale
record shows up:

- **missing** — the config file is gone, though its record (and the copy of its
  content, see below) is still in the store. `envoke prune` clears those.
- **trusted** / **changed** / **unreadable** — the file is still there, envoke
  just isn't loading it right now.

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

Both siblings are optional on read, so upgrading envoke never revokes an existing approval — a record with no siblings is a normal state, not corruption. The hash file is always written last, and every file is written atomically, so an interrupted write leaves the config *untrusted* rather than trusted against content it doesn't describe.

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
you've already approved. `envoke allow`, `envoke shell-hook`, `envoke reload`
and `envoke debug` all check whether the config file is writable by anyone
other than its owner (group or other write bits set) and print a non-fatal
warning to stderr if so:

```
envoke: warning: /home/you/.envokerc is writable by group/other (mode 664) -- consider tightening its permissions
```

`envoke allow` and `envoke debug` additionally check the **directory** the
config lives in, which is the stronger signal of the two: a config whose own
mode is `644` looks fine, but anyone who can write the directory holding it
can rename it away and drop their own file in its place, which the file's
permissions say nothing about.

```
envoke: warning: the directory /home/you/.config/envoke/envokerc.d is writable by group/other (mode 777) -- anyone who can write it can replace 10-work outright; run `chmod go-w /home/you/.config/envoke/envokerc.d`
```

The shell hook deliberately skips that second check. It runs on every
directory change, and the directory in question is your own config
directory — paying a syscall per config per `cd` to report something that can
only be true if you made it true is the wrong trade. The commands you actually
read the output of are where it fires.

These are warnings, not blocks — fix them with `chmod go-w` if you see one
unexpectedly.

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

This check covers the directories *above* the store too, up to the data home
— and names whichever one is actually writable. A `0700` store inside a
`0777` parent is a `0777` store: anyone who can write the parent can rename
the store away and put their own in its place, records and all. The walk
stops at the data home, because a writable directory above that means your
whole home is writable, which is not a fact about envoke.

!!! info "None of these warnings fire on Windows"

    They are a Unix answer to a Unix question. Windows governs access
    through ACLs, which Go's `os.Stat` does not report: it makes the
    permission word up from the read-only attribute alone, so every writable
    file reads as `0666` and every directory as `0777`. Testing the
    group/other bits against that would flag every config and the store
    itself on a perfectly ordinary machine — and, since the store check runs
    on the path every `cd` takes, print a warning at every prompt.

    So envoke says nothing there rather than something false. Reading the
    real ACL would mean a dependency outside the standard library, which is
    a large price for a warning. If you share a Windows machine, check the
    store's permissions yourself — `icacls %USERPROFILE%\.local\share\envoke`,
    or wherever `$XDG_DATA_HOME` points if you set it — and expect no entry
    beyond you, SYSTEM and Administrators.

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

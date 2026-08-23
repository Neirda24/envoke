---
title: Recipes
description: >-
  Worked envoke configs for the things people actually automate on cd —
  virtualenvs, kubectl contexts, cloud profiles, generated shell functions,
  dotenv files — including how to unwind each one on the way out.
---

# Recipes

Each recipe below is a complete `enter`/`leave` pair, with the unwinding
written out rather than left as an exercise. Drop them straight into your
config; the patterns are written for
[`~/.envokerc`](configuration.md#locating-the-central-config), so adjust the
paths to your own layout.

The blocks are POSIX/bash/zsh shell. envoke translates the `ENVOKE_*` plumbing
into your shell's dialect, but **a block's body is emitted verbatim** — under
fish or tcsh you write fish or tcsh.

## Read this first

Four things account for most of the surprises, and every recipe below is built
around them.

**Use `$ENVOKE_DIR`, never a relative path.** Through the shell hook a block
runs in the directory your shell landed in, which is only the matched
directory when you `cd` exactly onto it. `cd ~/work/api/cmd/srv` fires
`~/work/([^/]+)` once, for `~/work/api`, while your shell sits three levels
down. See [Where the script runs](configuration.md#where-the-script-runs).

**Entering a subdirectory does not re-fire the parent's block.** Going from
`~/work/api` to `~/work/api/src` enters only `~/work/api/src`, so a block
matching `~/work/api` fires once on the way in and its `leave` fires once when
you leave the tree for good. That is what makes save-and-restore work at all.

**A `leave` block can run when its `enter` never did** — you opened a terminal
already inside the directory, then left. Every restore below is guarded
against the variable being empty, because that is not a rare case.

**Nothing is auto-undone, and a failing block is quiet.** envoke has no
snapshot/restore; the `leave` block is the whole unwind. And a block that
fails does not stop the ones after it and does not reach `$?` — if a failure
matters, say so in the block itself:

```
enter ~/work/api
    . "$ENVOKE_DIR/venv/bin/activate" || echo "envoke: venv activation failed" >&2
```

!!! warning "Don't name your own variables `ENVOKE_*`"
    envoke sets `ENVOKE_DIR`, `ENVOKE_TYPE`, `ENVOKE_MATCH` and
    `ENVOKE_MATCH_N` around each block and clears them again afterwards. It
    won't touch a variable it didn't set, but the prefix is envoke's to grow
    into. Name yours something else — the recipes below use `SAVED_*`.

---

## Saving and restoring an external tool's state

The shape that covers `kubectl`, `gcloud`, `AWS_PROFILE`, `umask` and most of
what people want: read the current value on the way in, stash it in a shell
variable, put it back on the way out.

### kubectl context

```
enter ~/work/infra
    SAVED_KUBE_CONTEXT=$(kubectl config current-context 2>/dev/null)
    kubectl config use-context staging >/dev/null

leave ~/work/infra
    if [ -n "$SAVED_KUBE_CONTEXT" ]; then
        kubectl config use-context "$SAVED_KUBE_CONTEXT" >/dev/null
    fi
    unset SAVED_KUBE_CONTEXT
```

Three details that matter:

- **`SAVED_KUBE_CONTEXT` is deliberately not exported.** The block is
  evaluated by your own shell, so a plain shell variable already survives
  until the `leave` block reads it. Exporting it would push your cluster name
  into the environment of every process you start afterwards, for no gain.
- **`2>/dev/null` on the save.** `kubectl config current-context` exits
  non-zero and prints to stderr when no context is set at all; without this
  you'd get an error on your first `cd` on a fresh machine.
- **The `-n` guard on the restore** is what handles opening a terminal
  directly inside `~/work/infra`: no `enter` ran, nothing was saved, and
  restoring an empty context would be worse than doing nothing.

!!! note "This shape needs the shell hook"
    `envoke exec` runs each block in its own subprocess, so a variable set by
    an `enter` block is gone before the `leave` block could read it. Save and
    restore only works through the shell hook. See
    [Non-interactive Use](non-interactive.md).

### AWS profile

Same shape, one line shorter because the state is already a variable:

```
enter ~/work/([^/]+)
    SAVED_AWS_PROFILE="$AWS_PROFILE"
    export AWS_PROFILE=work

leave ~/work/([^/]+)
    if [ -n "$SAVED_AWS_PROFILE" ]; then
        export AWS_PROFILE="$SAVED_AWS_PROFILE"
    else
        unset AWS_PROFILE
    fi
    unset SAVED_AWS_PROFILE
```

The `else` branch is the difference between restoring and *inventing* state:
if `AWS_PROFILE` was unset before you arrived, it has to be unset again, not
set to the empty string — the AWS SDKs treat those differently.

### umask

```
enter ~/Projects/secrets
    SAVED_UMASK=$(umask)
    umask 077

leave ~/Projects/secrets
    [ -n "$SAVED_UMASK" ] && umask "$SAVED_UMASK"
    unset SAVED_UMASK
```

`umask` with no argument prints the current mask in a form `umask` accepts
back, so this round-trips whatever the user's shell was configured with rather
than assuming `022`.

---

## Python virtualenv, per project

```
enter ~/Projects/([^/]+)
    [ -f "$ENVOKE_DIR/venv/bin/activate" ] && . "$ENVOKE_DIR/venv/bin/activate"

leave ~/Projects/([^/]+)
    command -v deactivate >/dev/null && deactivate
```

One generic pair covers every project under `~/Projects`, including the ones
you clone next month — `([^/]+)` matches a whole path segment, so it fires for
`~/Projects/api` and not for `~/Projects/api/src`.

The `-f` test is what lets the same rule cover projects that have no venv, and
`command -v deactivate` keeps the `leave` block quiet when the `enter` never
activated anything.

The matched project name is available as `$ENVOKE_MATCH_1` if you want it:

```
enter ~/Projects/([^/]+)
    export PROJECT="$ENVOKE_MATCH_1"
```

---

## Project-scoped secrets

```
enter ~/work/api-server
    export API_KEY="$(cat ~/.secrets/api-server-key)"
    export API_ENV=staging

leave ~/work/api-server
    unset API_KEY API_ENV
```

**Never paste a secret into the config itself.** Two reasons, and the second
is the one people miss:

- The config is a plain file, usually in a dotfiles repository.
- The trust store keeps a **plaintext copy** of every config you approve, for
  the re-approval diff. A secret written inline ends up in a second place you
  have to remember to clean out. See [The store keeps a copy of what you
  approved](trust.md#the-store-keeps-a-copy-of-what-you-approved).

Fetch it at runtime instead. Then what lives in the config — and in that copy
— is a *reference*, not the secret:

```
# 1Password CLI
enter ~/work/api-server
    export API_KEY="$(op read 'op://Private/api-server/credential')"

# pass / gopass
enter ~/work/api-server
    export API_KEY="$(pass show work/api-server)"

# macOS Keychain
enter ~/work/api-server
    export API_KEY="$(security find-generic-password -s api-server -w)"

# Linux, Secret Service (gnome-keyring, KWallet)
enter ~/work/api-server
    export API_KEY="$(secret-tool lookup service api-server)"
```

```
leave ~/work/api-server
    unset API_KEY
```

!!! tip "Keep the `cd` fast"
    Every one of these shells out on each matching `cd`, and an unlocked-vault
    prompt in the middle of a directory change is unpleasant. If your manager
    supports a session token or a long-lived unlock, set it up — or fetch
    lazily instead, exporting a small function the block defines rather than
    the value itself, so the cost is paid when the secret is first used.

### Loading a `.env` file

The direnv `dotenv` equivalent, written out:

```
enter ~/work/([^/]+)
    if [ -f "$ENVOKE_DIR/.env" ]; then
        set -a
        . "$ENVOKE_DIR/.env"
        set +a
    fi

leave ~/work/([^/]+)
    if [ -f "$ENVOKE_DIR/.env" ]; then
        unset $(sed -n 's/^[[:space:]]*\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' "$ENVOKE_DIR/.env")
    fi
```

`set -a` exports every variable assigned until `set +a`, so the file itself
stays plain `KEY=value` lines. The `leave` block re-reads the same file to
learn which names to unset — which means **editing `.env` while inside the
directory and then leaving will fail to unset the names you removed**. That is
the honest cost of explicit unwinding; direnv avoids it by snapshotting the
whole environment, which envoke deliberately does not do.

---

## Generated shell functions, refreshed when their source changes

For a project that generates helper functions — from Docker labels, a
`Makefile`, a service manifest — the useful shape is *generate if stale, then
source*, so the cost is paid once rather than on every `cd`.

Have your generator write two files: the functions, and a plain list of the
names it defined.

```
enter ~/work/([^/]+)
    if [ -f "$ENVOKE_DIR/compose.yaml" ]; then
        if [ ! -f "$ENVOKE_DIR/.envoke-functions.sh" ] \
            || [ "$ENVOKE_DIR/compose.yaml" -nt "$ENVOKE_DIR/.envoke-functions.sh" ]; then
            generate-functions "$ENVOKE_DIR" \
                > "$ENVOKE_DIR/.envoke-functions.sh" 2> "$ENVOKE_DIR/.envoke-functions.log" \
                || echo "envoke: function generation failed, see .envoke-functions.log" >&2
        fi
        . "$ENVOKE_DIR/.envoke-functions.sh"
    fi

leave ~/work/([^/]+)
    if [ -f "$ENVOKE_DIR/.envoke-functions.list" ]; then
        while IFS= read -r fn; do
            unset -f "$fn" 2>/dev/null
        done < "$ENVOKE_DIR/.envoke-functions.list"
    fi
```

- **The `-nt` staleness check** is the whole point: without it every `cd` into
  the tree re-runs the generator, which is exactly the cost that makes people
  give up on this idea.
- **A generated name list beats guessing.** Parsing the generated file for
  `name()` in the `leave` block looks tidier and breaks on the first function
  defined inside a heredoc or a conditional.
- **Redirect the generator's stderr.** A block's failure output lands in the
  middle of your prompt otherwise, on every `cd`, with no way to scroll back
  to what caused it.
- Add `.envoke-functions.*` to the project's `.gitignore`.

---

## Adding a project's `bin/` to `PATH`

The direnv `PATH_add` equivalent. Removing an entry is the fiddly half:

```
enter ~/work/([^/]+)
    [ -d "$ENVOKE_DIR/bin" ] && export PATH="$ENVOKE_DIR/bin:$PATH"

leave ~/work/([^/]+)
    export PATH="$(printf '%s' "$PATH" | tr ':' '\n' \
        | grep -vxF "$ENVOKE_DIR/bin" | paste -sd: -)"
```

`grep -vxF` matches the whole line, literally, so a directory containing regex
metacharacters or a name that is a prefix of another `PATH` entry is removed
correctly — a plain `sed 's|...||'` gets both of those wrong.

---

## Node / toolchain version per project

```
enter ~/Projects/node/([^/]+)
    [ -f "$ENVOKE_DIR/.nvmrc" ] && nvm use --silent

leave ~/Projects/node/([^/]+)
    nvm use default --silent >/dev/null 2>&1
```

`nvm` is a shell function, not a binary — it only works here because the hook
evaluates blocks in your own shell rather than a subprocess. The same is true
of `pyenv shell`, `conda activate` and `rbenv shell`. Under `envoke exec` none
of them would take effect.

---

## Putting it together

A single config combining several of the above, with the teardown ordered
deliberately — the reason is under the config:

```
# ~/.envokerc

# --- Every project under ~/work: venv, project bin, .env ---
enter ~/work/([^/]+)
    export PROJECT="$ENVOKE_MATCH_1"
    [ -f "$ENVOKE_DIR/venv/bin/activate" ] && . "$ENVOKE_DIR/venv/bin/activate"
    [ -d "$ENVOKE_DIR/bin" ] && export PATH="$ENVOKE_DIR/bin:$PATH"

# --- The infra repo also switches cluster ---
enter ~/work/infra
    SAVED_KUBE_CONTEXT=$(kubectl config current-context 2>/dev/null)
    kubectl config use-context staging >/dev/null

# --- The unwinding, written in the order it has to happen ---
leave ~/work/infra
    if [ -n "$SAVED_KUBE_CONTEXT" ]; then
        kubectl config use-context "$SAVED_KUBE_CONTEXT" >/dev/null
    fi
    unset SAVED_KUBE_CONTEXT

leave ~/work/([^/]+)
    command -v deactivate >/dev/null && deactivate
    export PATH="$(printf '%s' "$PATH" | tr ':' '\n' \
        | grep -vxF "$ENVOKE_DIR/bin" | paste -sd: -)"
    unset PROJECT
```

Both `~/work/([^/]+)` and `~/work/infra` match `~/work/infra`, so both fire —
and **within one file, blocks fire in declaration order in both directions**.
Nothing reverses the `leave` blocks for you: unwinding in reverse is a property
of the config *set*, not of the blocks inside one file. That is why the two
`leave` blocks here are written after both `enter` blocks and in the opposite
order to them — the cluster is restored before the venv is deactivated because
that is the order the file puts them in, not because leaving reverses anything.
Order them yourself whenever two rules touch the same thing; see [The order
blocks fire in](configuration.md#the-order-blocks-fire-in).

Approve it with `envoke allow`, then check your reasoning against reality
before trusting it in anger:

```sh
envoke debug ~/ ~/work/infra/cmd/api
```

`envoke debug` prints exactly which blocks a move would fire, in order, and
never runs a thing. See [Debugging](debugging.md).

---

## Splitting these across `envokerc.d`

Once you have more than a handful of rules, one file gets hard to read. The
combined config above splits cleanly into an
[`envokerc.d`](configuration.md#the-envokercd-directory) directory — one file
per concern, plus the project's own config linked in:

```
~/.config/envoke/envokerc.d/
├── 10-work            # the ~/work/([^/]+) pair: venv, PROJECT, bin/ on PATH
├── 20-infra           # the ~/work/infra pair: kubectl context save/restore
└── api-server -> ~/work/api-server/envoke.conf
```

Nothing about the blocks themselves changes — same syntax, same patterns. One
`envoke allow` still covers the lot.

**The one thing that changes is ordering.** Within a file, blocks fire in
declaration order whichever direction you are going, which is why the combined
config above has its `leave` blocks hand-ordered. Across files, the *set* order
decides: fragments apply in relative path order on the way in and in the
**reverse** order on the way out. So the `10-work` / `20-infra` split gets the
unwinding for free — the `10-` prefix is what puts the venv before the cluster
switch on the way in, and the same prefix is what puts the cluster restore
before the venv teardown on the way out — and each file can go back to holding
its own `enter`/`leave` pair.

If you split them the other way round, or name them `infra` and `work`, that
ordering silently flips. `envoke debug` prints the blocks in the order they
would run, with the file each came from, which is the quickest way to check
you got it right:

```sh
envoke debug ~/ ~/work/infra
```

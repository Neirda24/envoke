---
title: envoke vs. direnv
description: >-
  How envoke differs from direnv: path patterns in your own config directory
  instead of an .envrc found by walking into it, arbitrary shell scripts
  instead of environment variables only, and an explicit leave block instead
  of automatic environment restore.
---

# envoke vs. direnv

[direnv](https://direnv.net/) is the tool most people already have when they
find envoke, so this page is the honest point-by-point: what each one is
shaped for, where they overlap, and when it's worth swapping — or running
both.

!!! abstract "The short version"
    **direnv** manages **environment variables**, declared by a file **inside**
    each directory, and restores them for you when you leave.

    **envoke** runs **arbitrary shell scripts** on enter *and* leave, declared
    by **path pattern**, in configs that live in *your* config directory — one
    file, or one per project in `envokerc.d`. A project's own config can join
    in, but by a symlink you create, never by walking into the directory.

    Neither replaces the other outright. direnv's stdlib (`layout python`,
    `use nix`, `dotenv`) and its automatic environment restore have no
    equivalent here. envoke is for rules that belong to *you*, or that do
    things which aren't environment variables.

## Side by side

| | direnv | envoke |
|---|---|---|
| **Where rules live** | An `.envrc` in each directory, found by walking into it | Your own config directory — one central file and/or `envokerc.d` fragments — selecting directories by path pattern |
| **How a directory is selected** | By the file's own location | RE2 regex matched against whole path segments, capture groups exposed to the script |
| **What it can change** | Environment variables — exported vars captured from a bash subshell | Anything shell code can do; the hook's output is `eval`'d by your own shell |
| **Leaving a directory** | Automatic: the previous environment is restored | Explicit: you write a `leave` block; nothing is auto-undone |
| **Script language** | bash, always — even if your shell is fish or PowerShell | Your shell's own syntax |
| **Trust step** | `direnv allow`, per `.envrc` | `envoke allow`, once per config file and covering the whole set by default, showing a **diff** on re-approval |
| **Shells** | bash, zsh, fish, tcsh, elvish, pwsh, murex (+ export formats) | bash, zsh, fish, tcsh, PowerShell |
| **Batteries** | Large stdlib: `layout python`, `use nix`, `PATH_add`, `dotenv`, … | None — you write plain shell |
| **Runtime dependencies** | Needs bash available to evaluate `.envrc` | One static binary, no cgo, nothing else |

## 1. The rules don't have to live in the directory

This is the difference that brings most people here. direnv *needs* a file in
the directory it acts on, which is a poor fit when:

- the repository isn't yours, or your team doesn't want an `.envrc` committed;
- you'd rather not add another entry to `.gitignore` in every checkout;
- the rule is really "everything under `~/work/`", not "this one project";
- you don't want to run an approval command again for each new clone.

It's a long-standing ask on direnv's own tracker — see
[direnv#1108 "Allow external dir mappings"](https://github.com/direnv/direnv/issues/1108),
[direnv#280 "How about having a global envrc that describes all direnvs"](https://github.com/direnv/direnv/issues/280),
and [direnv#1311](https://github.com/direnv/direnv/issues/1311).

envoke starts from the other end: one config you own, patterns that cover
whole trees, and nothing dropped inside the directories.

```
# ~/.envokerc — applies to every project under ~/work, including ones cloned tomorrow
enter ~/work/([^/]+)
    export PROJECT_NAME="$ENVOKE_MATCH_1"
    export AWS_PROFILE=work
```

**And when they should live in the repository, they can — but you opt in.** A
config committed inside a project works, with patterns written relative to
itself, and joins the set through a symlink you create:

```sh
ln -s ~/work/api/envoke.conf ~/.config/envoke/envokerc.d/api
```

```
# ~/work/api/envoke.conf — committed with the repository
enter .
    export PROJECT_ROOT="$ENVOKE_DIR"

enter ./services/([^/]+)
    export SERVICE="$ENVOKE_MATCH_1"
```

That is the deliberate difference from direnv: envoke never reads a config
because you walked into the directory holding it. The link is the decision.
Such a config is also confined to its own directory tree — however its
patterns are written, it cannot fire outside the project — and is approved
separately from your central one.

**The flip side, stated plainly:** direnv's per-project model is more than a
file location. Its stdlib (`layout python`, `use nix`, `dotenv`) is what most
`.envrc` files actually consist of, and envoke has no equivalent: you write
the shell yourself.

See [Configuration](configuration.md) for the full pattern syntax and the
`ENVOKE_*` variables a matched block receives.

## 2. It isn't only about environment variables

direnv evaluates `.envrc` in a bash subshell and captures the **exported
variables** out of it. That's a deliberate, robust design — it's what lets one
implementation serve every shell — but it also means anything that isn't an
environment variable doesn't survive the trip back: shell functions, aliases,
shell options, a `source`d activation script that defines functions.

envoke's shell hook doesn't run your script in a subprocess at all. It emits
shell text in your shell's dialect, which your *own* shell evaluates — so
`source`, `alias`, `set -o`, and shell functions all work as written:

```
enter ~/Projects/([^/]+)
    source "$ENVOKE_DIR/venv/bin/activate"
    alias t='go test ./...'

leave ~/Projects/([^/]+)
    deactivate
    unalias t
```

The same applies to side effects that live outside the shell entirely —
switching a `kubectl` context, renaming a tmux window, adding an ssh key,
tightening `umask`.

## 3. Leaving is a script you write

direnv restores the environment it changed, automatically. That's genuinely
less work, and for pure environment variables it's the better default.

envoke deliberately does not do this. There's no state snapshot and no
auto-undo — a `leave` block runs only what you wrote in it. The cost is
explicit teardown code; the benefit is that teardown can undo things direnv
*can't* restore, because they were never environment variables:

```
enter ~/Projects/infra
    kubectl config use-context staging

leave ~/Projects/infra
    kubectl config use-context default
```

This is a design principle, not a missing feature — see
[Design Notes](design-notes.md).

## 4. Matching is a regex over path segments

Because rules are central, envoke needs a way to say *which* directories a
block applies to. Patterns compile to Go's `regexp` (RE2), so matching is
linear-time no matter how the pattern is written, and they're anchored so they
match **whole path segments** — `~/Projects/foo` never matches
`~/Projects/foobar`. Capture groups reach the script as `ENVOKE_MATCH_1`,
`ENVOKE_MATCH_2`, … so one block can cover a whole family of directories.

Moving straight from `/a` to `/a/x/y/z` fires the rules for `/a/x` and
`/a/x/y` too, not just the endpoint.

direnv has no equivalent concept, and doesn't need one — the file's location
*is* the match.

## 5. Both gate execution, differently

Neither tool will run code you haven't approved. Both hash the approved
content, and both revoke trust the moment it changes.

The differences are ergonomic:

- **One approval covers a whole tree.** envoke's unit of trust is the config
  file, so a central rule for `~/work/([^/]+)` is approved once and applies to
  every clone under it — including tomorrow's. And `envoke allow` with no
  argument covers every config at once, however many files you split into.
- **A diff on re-approval.** After the first `envoke allow`, later approvals
  show what changed since the version you trusted, instead of re-dumping the
  whole file for you to re-read.
- **Nothing to approve that you didn't add.** direnv asks about any `.envrc`
  you walk into. envoke only ever loads configs from your own config
  directory, so an unapproved config is one you put there yourself.
- **No cross-directory unloading.** In direnv, an untrusted `.envrc` in a
  subdirectory unloads the trusted parent environment as well — see the
  discussion in [direnv#1493](https://github.com/direnv/direnv/issues/1493).
  In envoke each config is independent: an untrusted one is reported and
  skipped, and the trusted ones still run.

envoke also warns when the config file is group- or other-writable: hash-based
trust protects against silent modification, not against another local user
editing the file outright.

Details in [Trust Model](trust.md), and
[`envoke debug`](debugging.md) shows exactly which blocks a given move would
fire — without executing anything.

## Can I run both?

Yes. They hook the shell independently and don't interfere; a common split is
direnv for repositories that ship their own `.envrc`, envoke for your own
machine-wide rules across trees. The only thing to watch is that if both set
the same variable for the same directory, whichever hook runs last wins — so
keep them responsible for different things.

## Moving a project over

A typical `.envrc`:

```sh
export DATABASE_URL=postgres://localhost/myapp_dev
export AWS_PROFILE=myapp-dev
PATH_add bin
```

The equivalent in `~/.envokerc` — note that the teardown is now yours to
write, and that `$ENVOKE_DIR` is the directory that matched (which isn't
necessarily where you ended up, if you jumped several levels deep):

```
enter ~/Projects/myapp
    export DATABASE_URL=postgres://localhost/myapp_dev
    export AWS_PROFILE=myapp-dev
    export PATH="$ENVOKE_DIR/bin:$PATH"

leave ~/Projects/myapp
    unset DATABASE_URL AWS_PROFILE
    export PATH="$(printf '%s' "$PATH" | tr ':' '\n' \
        | grep -vxF "$ENVOKE_DIR/bin" | paste -sd: -)"
```

The `PATH` teardown looks heavier than a `${PATH#...}` prefix strip because
that shorter form only works while the entry is still leftmost — see
[Recipes](recipes.md#adding-a-projects-bin-to-path) for why removal by whole
line is the version that survives something else being prepended.

Then `envoke allow` to review and approve it — until you do, none of it runs.

## What about ondir, mise, and shadowenv?

- **[ondir](https://github.com/alecthomas/ondir)** — the same model envoke
  targets: `enter`/`leave` blocks matched by path, in one central config. It's
  feature-complete by its own maintainer's account and still works, but hasn't
  seen a release in years. [Design Notes](design-notes.md) lists the specific
  points where envoke departs from it.
- **[mise](https://mise.jdx.dev/)** — tool-version manager that also covers
  direnv's env use case via a per-project `mise.toml`. Same shape as direnv for
  the purposes of this page: the rules live in the project.
- **[shadowenv](https://github.com/Shopify/shadowenv)** — per-directory
  `.shadowenv.d`, an s-expression DSL, environment-focused. Again, a file in
  the directory.

The recurring theme: everything else in this space puts a file in the
directory and manages environment variables. envoke is the option that keeps
the rules in one place you control and lets a block run whatever you want.

## Try it

```sh
brew install neirda24/tap/envoke
```

Then head to [Getting Started](getting-started.md) — hook your shell, write a
first block, `envoke allow`, and `cd`. Other install methods (Scoop, `.deb`,
`.rpm`, `go install`, prebuilt binaries) are listed there too.

# Configuration

envoke reads two kinds of config, in the same format:

- **your central config** — one file, the documented default;
- **fragments in an `envokerc.d` directory** — one file per project or concern, so rules don't accumulate in a single file.

Both are loaded together, both live in a directory you own, and both are
approved separately. Nothing in either runs until it is.

!!! info "Already have a `~/.envokerc`?"
    Nothing changes. `envokerc.d` is *additive*: your central config is
    located and loaded exactly as before, and the fragment directory is only
    consulted if it exists. There is nothing to migrate.

## Locating the central config

envoke resolves the central config path in this order:

1. `$ENVOKERC` — used verbatim, even if the file doesn't exist yet. A path that doesn't exist is treated as "no config", silently: the shell hook stays quiet on every `cd` rather than reporting it, so you can point `$ENVOKERC` at a file before writing it. A config that *does* exist but fails to parse, or can't be read, is still reported — that's a config you believe is in effect and isn't.
2. `~/.envokerc` — if present.
3. `$XDG_CONFIG_HOME/envoke/config` (or `~/.config/envoke/config`) — if present.
4. Not found — this is a normal state, not an error; envoke simply has nothing to match centrally.

## The `envokerc.d` directory

Rather than growing one file, you can split rules across an `envokerc.d`
directory. Every file in it is a config in its own right, using exactly the
same block syntax.

envoke looks for it in the same spirit as the central config:

1. `$ENVOKERC_D` — used verbatim, even if it doesn't exist yet.
2. `~/.envokerc.d` — if it exists.
3. `$XDG_CONFIG_HOME/envoke/envokerc.d` (or `~/.config/envoke/envokerc.d`) — if it exists.

**These are alternatives, not a merged set:** envoke uses the *first* of them
that exists and never looks at the others. If you have both `~/.envokerc.d`
and one under `~/.config`, only `~/.envokerc.d` is read. The same is true of
the central config's three locations above.

**envoke never goes looking for configs anywhere else.** It does not read a
file because you walked into the directory containing it; every config it
loads is one you put in your own config directory. That is a deliberate limit
— see [Trust Model](trust.md).

```
~/.config/envoke/envokerc.d/
├── 10-work            # AWS profile and kubectl context for ~/work
├── 20-python          # virtualenv activation, every project
└── api-server -> ~/work/api-server/envoke.conf
```

- **Order is the relative path.** Files are read recursively and applied in
  order of their path relative to the directory (for a flat directory, that
  is simply filename order), which is what makes `10-`, `20-` prefixes do
  what you expect.
- **Ignored:** names starting with `.` or ending with `~` — the two spellings
  that cover editor backups and swap files. Directories named that way are
  skipped whole. **Nothing else is ignored**: a `10-work.bak` or a
  `10-work.swp` left beside the real file is a config like any other, and
  will be loaded, parsed and hashed. Keep scratch copies outside the
  directory.
- **Each file is trusted separately**, and one `envoke allow` covers the lot
  (see [Trust Model](trust.md)).
- **A file that doesn't parse is reported and skipped**, not fatal — one
  broken fragment never disables the others.
- **The directory is bounded**: at most 512 files, nested at most 8 levels
  deep, and every fragment has to be a regular file — a FIFO, a device, or a
  symlink to either is refused before anything opens it, because opening one is
  itself what hangs or reads forever. Every file in it is opened and parsed on
  every directory change, so an `$ENVOKERC_D` accidentally pointing at a home
  directory — or at `/` — would put a whole-tree walk in front of every shell
  prompt. Any of the three is an error naming the bound it hit, never a silently
  shortened list — and unlike a fragment that merely doesn't parse, it takes the
  whole directory out of the set.
- **A single config is bounded as well**: 1 MiB, your central config included,
  since every config in the set is read whole on every directory change. Past
  that the file fails to load with an error naming the bound, rather than being
  read in part. The regular-file rule above is a fragment rule only —
  `$ENVOKERC` is honoured verbatim and not checked, so pointing it at a FIFO
  hangs on every directory change.
- **A fragment is cheap**: roughly 25µs per file per directory change on a
  container runner, so fifty fragments cost about 1.3ms before trust
  checking. Split by whatever makes the rules readable; the thing to avoid
  is pointing `$ENVOKERC_D` at a tree rather than at a config directory.

### Bringing a project's own config in

The last line of the tree above is a **symlink**, and it is how a config
committed inside a repository joins the set:

```sh
ln -s ~/work/api-server/envoke.conf ~/.config/envoke/envokerc.d/api-server
```

The link is the opt-in. envoke will not pick that file up because you `cd`
into the project; you decide once, by creating the link, and the file's
content still has to be approved like any other.

Two things follow from the link:

- **`./` in that file means the project**, not your config directory — see
  [Relative patterns](#relative-patterns) below. That is what lets the file be
  committed with no absolute path in it.
- **It may only match inside the project.** However its patterns are written,
  a fragment that points out of your config directory cannot fire for a
  directory outside its own tree. Its content is what a `git pull` can
  rewrite, so the blast radius is bounded to the repository it came with.

The tree that bound names is where the linked file *really* is, symlinks
resolved — so a project reached through a symlinked parent (anything under
`/var` on macOS, a link to `/private/var`) is inside its own bound, and its
blocks fire as written. That same resolution is what the `ENVOKE_MATCH`
captures reflect; see [What a matched script
sees](#what-a-matched-script-sees).

Neither the bound nor the target is left for you to infer: `envoke debug`
prints both under the config's status line, and `envoke allow`'s review states
them before asking you to confirm — the target because that file, not the link,
is what envoke parses, hashes and shows you. See [Debugging](debugging.md) and
[Reviewing a symlinked project
fragment](trust.md#reviewing-a-symlinked-project-fragment).

If instead your whole config directory is a symlink into a dotfiles
repository — the usual dotfiles layout — nothing is confined: those files are
yours, and envoke resolves the directory before reading it, so the paths it
prints name the real files in your dotfiles repo.

Two things about that layout across several machines:

- **Trust records are per machine, and are not in the repository.** They live
  under your data home and are keyed on each config's absolute path (see [How
  trust is tracked](trust.md#how-trust-is-tracked)), so cloning your dotfiles
  onto a new box approves nothing. Put the approval in your bootstrap script,
  where there is nobody to answer a prompt: `envoke allow --yes`.
- **Prefer `./`-relative patterns** in a fragment whose repository may be
  checked out at a different path on the next machine. An absolute pattern has
  to be edited per machine; a relative one doesn't.

### Relative patterns

Inside any config, a pattern starting with `./` or `../` resolves against the
directory the config file itself lives in — for a symlinked fragment, the
directory of the file it points at:

```
# ~/work/api-server/envoke.conf, symlinked into envokerc.d
enter .
    export PROJECT_ROOT="$ENVOKE_DIR"

enter ./src
    echo "in the source tree"

enter ./services/([^/]+)
    echo "service $ENVOKE_MATCH_1"
```

`.` is the config's own directory, `./x` a child of it, `../x` a sibling —
though a *symlinked* fragment like this one is confined to its own tree, so a
`../` pattern in it compiles fine and then never fires. `envoke debug` names
that bound under the config's status line, which is how to tell this apart from
a pattern that is simply wrong. Only the central config, or a fragment that
really lives in your config directory, can match a directory outside its own
tree.

Relative patterns are what make such a file portable: no absolute path appears
in it, so it works wherever the repository is checked out — including on
another operating system, since patterns are always written with `/` and envoke
normalizes the paths it tests to match (`C:\proj` is tested as `C:/proj`).

Only a *leading* `./` or `../` is special, exactly as only a leading `~` is.
Everything else keeps its regular-expression meaning, so an alternation like
`(/opt|/srv)/x` is unaffected — and a pattern that merely starts with a dot,
such as `...`, is still an ordinary regex.

Relative patterns work in the central config too, where the base is that
file's own directory (`$HOME`, for `~/.envokerc`).

## Block syntax

A config file declares `enter`/`leave` blocks: an unindented header line, followed by an indented script body.

```
enter <path-pattern>
    <script line 1>
    <script line 2>

leave <path-pattern>
    <script line>
```

- `enter <path-pattern>`: runs when you `cd` into a directory matching the pattern.
- `leave <path-pattern>`: runs when you `cd` out of a directory matching the pattern.
- Moving straight from `/a` to `/a/x/y/z` still fires `/a/x`'s and `/a/x/y`'s rules — envoke walks every intermediate directory, not just the endpoints.
- The complement holds too: a directory you were **already inside** is not entered again, so moving from `/a/x` to `/a/x/y` does not re-fire `/a`'s or `/a/x`'s `enter` block. That is what makes save-and-restore work at all.
- **Enter and leave are independent, explicit blocks.** envoke does not snapshot state on enter and auto-restore it on leave. If entering exports a variable or activates a venv, the matching `leave` block is responsible for explicitly unwinding it.

The details of the format:

- A body ends at the next unindented, non-blank line, or at end of file. **Blank lines inside a body do not end it**, so a multi-line script can breathe.
- The common leading whitespace is stripped from a body, so the script's own indentation — a `for` loop, an `if` — is preserved relative to the block rather than to column 0.
- **`#` starts a comment only outside a block.** Inside a body it is part of the script, which is what you want, since it is a shell comment there. So indent the `#` with the rest of the body to comment inside a block. An unindented `#` *ends* the block above it — and picking the body back up afterwards is a positioned parse error, not a silently truncated block.
- A block header with no script body is an error, not an empty block.

A malformed config fails with a positioned error (line number + message) rather than silently misbehaving.

### The order blocks fire in

For a single directory change:

1. **`leave` blocks first, deepest directory first** — unwinding the nested-most rule before the ones above it, mirroring a stack.
2. **Then `enter` blocks, shallowest directory first** — the outer rule before the nested one, so a project-wide block runs before a subdirectory's.
3. When several configs match the *same* directory, they apply in set order on the way in — the central config first, then each `envokerc.d` fragment in relative path order — and in the reverse order on the way out.
4. When several blocks in one config match the same directory, they fire in the order they are declared in the file — **in both directions**. Only the *set* is reversed on the way out, never the blocks inside one file, so a single file's `leave` blocks unwind in the order you wrote them.

## Path patterns

Patterns are matched with Go's `regexp` package (RE2) — linear-time matching, guaranteed regardless of the pattern.

- A leading `~` expands to your home directory.
- A leading `./` or `../` resolves against the config file's own directory — see [Relative patterns](#relative-patterns).
- `$VAR` / `${VAR}` expand as literal substitutions (not re-interpreted as regex) before the pattern is compiled.
- **An undefined variable is an error, not an empty string.** `$HOEM/Projects` fails with a positioned parse error naming `HOEM`, instead of quietly compiling to a pattern that can never match. A variable that is set but empty is a value, and expands to nothing as you'd expect.
- A `$` that isn't followed by a variable name stays a literal `$` — so it still works as the regex end anchor (`~/Projects/(a|b)$`).
- The final pattern is anchored as `^(?:...)$` against each path segment being tested — this is what makes matching **segment-based** rather than a raw string prefix, so `~/Projects/foo` never falsely matches `~/Projects/foobar` (unlike ondir's raw prefix matching).
- **Matching is case-sensitive, even where the filesystem is not.** macOS by
  default, and Windows, treat `~/work/API` and `~/work/api` as one directory; a
  pattern matches only the spelling it is written with. A leading `(?i)` makes a
  pattern case-insensitive — though not a `./`-relative one, since only a
  *leading* `./` is special and `(?i)./src` is an ordinary regex. For a
  [confined](#bringing-a-projects-own-config-in) fragment this has a further
  twist, in
  [Troubleshooting](troubleshooting.md#10-the-directory-and-the-pattern-differ-in-case).

## What a matched script sees

Each matched block runs with these environment variables set:

| Variable | Meaning |
|---|---|
| `ENVOKE_DIR` | The directory that matched, as your shell spelled it — symlinks not resolved. |
| `ENVOKE_TYPE` | `enter` or `leave`. |
| `ENVOKE_MATCH` | The full text the pattern matched. |
| `ENVOKE_MATCH_N` | Capture group `N` (e.g. `ENVOKE_MATCH_1`), if the pattern has capture groups. |

For example, `enter ~/Projects/([^/]+)` exposes the matched project name via `ENVOKE_MATCH_1`, so one generic block can handle every directory under `~/Projects` instead of duplicating config per project.

These variables are scoped to the block that sets them: they are cleared again as soon as its script finishes, so one block never sees another's capture groups and nothing leaks into the processes you start afterwards.

**For a [confined](#bringing-a-projects-own-config-in) fragment, `ENVOKE_MATCH`
and `ENVOKE_DIR` can name the same directory two different ways.** Such a
config is bounded to where its files really are, so its patterns are matched
against the directory with every symlink resolved — and captures can only come
from the path the pattern actually ran against. `ENVOKE_MATCH` and every
`ENVOKE_MATCH_N` therefore hold segments of the **resolved** path, while
`ENVOKE_DIR` stays the directory your shell reported, because that is where the
`cd` landed and what the block runs in. On macOS the two differ for any project
under `/var`, a symlink to `/private/var`. So a block that captures a path
prefix and builds on it — `export ROOT="$ENVOKE_MATCH"` — will see the resolved
form; use `$ENVOKE_DIR` when you want the path as typed. Every other config,
your central one and any fragment that really lives in `envokerc.d`, matches
the directory as your shell spelled it, and the two agree.

### Where the script runs

**Use `$ENVOKE_DIR` for anything relative — the working directory is not the directory that matched.**

Through the shell hook, a block runs in your own shell, which has already arrived at your destination. The two are the same only when you `cd` exactly onto the matching directory:

```sh
cd ~/Projects/my-app            # ENVOKE_DIR and the working directory both ~/Projects/my-app
cd ~/Projects/my-app/cmd/srv    # ENVOKE_DIR is ~/Projects/my-app, you are three levels below it
```

The pattern `~/Projects/([^/]+)` matches whole path segments, so it still fires exactly once in both cases, for `~/Projects/my-app` — but in the second, `source venv/bin/activate` would look under `cmd/srv`. Write `source "$ENVOKE_DIR/venv/bin/activate"` and it works from anywhere in the tree. `leave` blocks always run from outside the directory they matched, so they never have a usable relative path.

`envoke exec` differs here: it runs each block as a subprocess with the matched directory as its working directory. `envoke debug` points out the discrepancy whenever it applies.

### When a block fails

**Through the shell hook, a failing block does not stop the ones after it.** envoke hands your shell one script containing every matched block in order, and your shell runs it the way it runs any script: a command that exits non-zero is not fatal. If an `enter` block's `source` fails, the next block still runs.

**And the failure does not reach `$?`.** Every hook saves the exit status it was entered with and restores it before returning — otherwise a prompt that colours on the last command's status would report envoke's instead of yours, on every single `cd`. The cost of that is real: a failing block shows up on stderr and nowhere else. If a block must not fail silently, say so in the block itself:

```
enter ~/Projects/api-server
    source "$ENVOKE_DIR/venv/bin/activate" || echo "envokerc: venv activation failed" >&2
```

`envoke exec` is the opposite on both counts: it stops at the first block that exits non-zero and exits 1 itself. Nothing already applied is unwound — see the enter/leave independence rule above.

## Example envokerc

Blocks are separated by a blank line; a line starting with `#` outside a
block is a comment. Here's a single config combining several common cases —
[Recipes](recipes.md) works each of these through properly, including the
guards and the unwinding they need in practice:

```
# ~/.envokerc

# --- Python virtualenv, activated per project ---
enter ~/Projects/([^/]+)
    source "$ENVOKE_DIR/venv/bin/activate"

leave ~/Projects/([^/]+)
    deactivate

# --- Project-scoped secrets ---
enter ~/Projects/api-server
    export API_KEY=$(cat ~/.secrets/api-server-key)
    export API_ENV=staging

leave ~/Projects/api-server
    unset API_KEY API_ENV

# --- Kubernetes context per infra repo ---
enter ~/Projects/infra
    kubectl config use-context staging

leave ~/Projects/infra
    kubectl config use-context default

# --- Node version per project, from the matched directory name ---
enter ~/Projects/node/([^/]+)
    nvm use --silent

# --- Tighten umask for a sensitive tree, restore it on the way out ---
enter ~/Projects/secrets
    umask 077

leave ~/Projects/secrets
    umask 022
```

Save this as `~/.envokerc`, or split it across [`envokerc.d`](#the-envokercd-directory)
files (or wherever [the lookup order](#locating-the-central-config)
resolves for you), then run `envoke allow` to review and approve it — no
block in this file runs until you do.

## A note on `envoke shell-hook`'s execution model

The shell hook that runs on every `cd` doesn't exec your script in a subprocess — it renders shell text (`export`/`source` statements, in the right dialect for your shell) that your *own* shell then `eval`s. That's deliberate: only running in the parent shell process makes exported variables or `source`d scripts (like venv activation) actually visible after the `cd` completes.

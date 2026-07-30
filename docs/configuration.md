# Configuration

## Locating the config file

envoke resolves the config path in this order:

1. `$ENVOKERC` — used verbatim, even if the file doesn't exist yet. A path that doesn't exist is treated as "no config", silently: the shell hook stays quiet on every `cd` rather than reporting it, so you can point `$ENVOKERC` at a file before writing it. A config that *does* exist but fails to parse, or can't be read, is still reported — that's a config you believe is in effect and isn't.
2. `~/.envokerc` — if present.
3. `$XDG_CONFIG_HOME/envoke/config` (or `~/.config/envoke/config`) — if present.
4. Not found — this is a normal state, not an error; envoke simply has nothing to match.

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
3. When several blocks match the *same* directory, they fire in the order they are declared in the file.

## Path patterns

Patterns are matched with Go's `regexp` package (RE2) — linear-time matching, guaranteed regardless of the pattern.

- A leading `~` expands to your home directory.
- `$VAR` / `${VAR}` expand as literal substitutions (not re-interpreted as regex) before the pattern is compiled.
- **An undefined variable is an error, not an empty string.** `$HOEM/Projects` fails with a positioned parse error naming `HOEM`, instead of quietly compiling to a pattern that can never match. A variable that is set but empty is a value, and expands to nothing as you'd expect.
- A `$` that isn't followed by a variable name stays a literal `$` — so it still works as the regex end anchor (`~/Projects/(a|b)$`).
- The final pattern is anchored as `^(?:...)$` against each path segment being tested — this is what makes matching **segment-based** rather than a raw string prefix, so `~/Projects/foo` never falsely matches `~/Projects/foobar` (unlike ondir's raw prefix matching).

## What a matched script sees

Each matched block runs with these environment variables set:

| Variable | Meaning |
|---|---|
| `ENVOKE_DIR` | The directory that matched. |
| `ENVOKE_TYPE` | `enter` or `leave`. |
| `ENVOKE_MATCH` | The full text the pattern matched. |
| `ENVOKE_MATCH_N` | Capture group `N` (e.g. `ENVOKE_MATCH_1`), if the pattern has capture groups. |

For example, `enter ~/Projects/([^/]+)` exposes the matched project name via `ENVOKE_MATCH_1`, so one generic block can handle every directory under `~/Projects` instead of duplicating config per project.

These variables are scoped to the block that sets them: they are cleared again as soon as its script finishes, so one block never sees another's capture groups and nothing leaks into the processes you start afterwards.

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
block is a comment. Here's a single config combining several common cases:

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

Save this as `~/.envokerc` (or wherever [`Locate()`](#locating-the-config-file)
resolves for you), then run `envoke allow` to review and approve it — no
block in this file runs until you do.

## A note on `envoke shell-hook`'s execution model

The shell hook that runs on every `cd` doesn't exec your script in a subprocess — it renders shell text (`export`/`source` statements, in the right dialect for your shell) that your *own* shell then `eval`s. That's deliberate: only running in the parent shell process makes exported variables or `source`d scripts (like venv activation) actually visible after the `cd` completes.

# Configuration

## Locating the config file

envoke resolves the config path in this order:

1. `$ENVOKERC` — used verbatim, even if the file doesn't exist yet.
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

A malformed config fails with a positioned error (line number + message) rather than silently misbehaving.

## Path patterns

Patterns are matched with Go's `regexp` package (RE2) — linear-time matching, guaranteed regardless of the pattern.

- A leading `~` expands to your home directory.
- `$VAR` / `${VAR}` expand as literal substitutions (not re-interpreted as regex) before the pattern is compiled.
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

## Example envokerc

Blocks are separated by a blank line; a line starting with `#` outside a
block is a comment. Here's a single config combining several common cases:

```
# ~/.envokerc

# --- Python virtualenv, activated per project ---
enter ~/Projects/([^/]+)
    source venv/bin/activate

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

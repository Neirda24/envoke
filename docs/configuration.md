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

Patterns are matched with Go's `regexp` package (RE2) — linear-time matching, no catastrophic backtracking, unlike ondir's POSIX regex.

- A leading `~` expands to your home directory.
- `$VAR` / `${VAR}` expand as literal substitutions (not re-interpreted as regex) before the pattern is compiled.
- The final pattern is anchored as `^(?:...)$` against each path segment being tested — this is what makes matching **segment-based** rather than a raw string prefix, so `~/Projects/foo` never falsely matches `~/Projects/foobar` (a real ondir bug).

## What a matched script sees

Each matched block runs with these environment variables set:

| Variable | Meaning |
|---|---|
| `ENVOKE_DIR` | The directory that matched. |
| `ENVOKE_TYPE` | `enter` or `leave`. |
| `ENVOKE_MATCH` | The full text the pattern matched. |
| `ENVOKE_MATCH_N` | Capture group `N` (e.g. `ENVOKE_MATCH_1`), if the pattern has capture groups. |

For example, `enter ~/Projects/([^/]+)` exposes the matched project name via `ENVOKE_MATCH_1`, so one generic block can handle every directory under `~/Projects` instead of duplicating config per project.

## Example use cases

- **Python (or any) virtualenv activation** — `enter` sources `venv/bin/activate`, `leave` runs `deactivate`.
- **Per-directory umask** — `enter` tightens `umask` for a sensitive tree, `leave` restores the default explicitly.
- **Project-scoped environment variables** — `enter` exports API keys/endpoints/feature flags, `leave` unsets them.
- **Cloud/k8s context switching** — `enter` runs `kubectl config use-context ...` / sets `AWS_PROFILE` / activates a `gcloud` configuration; `leave` switches back explicitly.
- **Dev toolchain version switching** — `enter` swaps Node/Ruby/Go versions for a project tree without depending on each tool's own per-directory convention.

## A note on `envoke shell-hook`'s execution model

The shell hook that runs on every `cd` doesn't exec your script in a subprocess — it renders shell text (`export`/`source` statements, in the right dialect for your shell) that your *own* shell then `eval`s. That's deliberate: only running in the parent shell process makes exported variables or `source`d scripts (like venv activation) actually visible after the `cd` completes.

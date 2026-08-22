# CLAUDE.md

How to work on `envoke` as an agent. This is **not** project documentation and
must never become it.

| Question | Where the answer lives |
|---|---|
| What envoke does, and how it's used | [README.md](README.md), [docs/](docs/) |
| Why it's designed this way | [docs/design-notes.md](docs/design-notes.md) |
| How to build, test and submit a change | [CONTRIBUTING.md](CONTRIBUTING.md) |
| What holds across the whole codebase | here |
| Where a rule is enforced in *this* package | that package's own `CLAUDE.md` |
| A ritual spanning files that no file states | `.claude/skills/*/SKILL.md` |

## Rules for these files

- **The code is the truth; the docs are the intent.** If this file and a docs
  page disagree, the docs page wins and this file needs fixing.
- **Don't restate documented behaviour**, and don't restate a Go comment
  either. Add only what a reader of the source needs and nothing else carries:
  which test pins a rule, which files must change together, what a
  plausible-looking refactor would break.
- **Prefer the route to a fact over a copy of it.** `dagger functions -m
  .dagger`, `envoke help`, a signature, a flag's default — record how to find
  it. A copied list goes stale silently and is believed anyway.
- **Per-package detail lives next to the code**, loaded only once a file in
  that directory is read. Read one before changing that package; put new
  package-specific findings there rather than growing this file back.
- **Bullets, not essays.** Every word here is prepended to every prompt.

## Agent instructions are referenced only by other agent instructions

- No Go comment, README, `CONTRIBUTING.md`, workflow, `renovate.json` or
  script header may point at a `CLAUDE.md` or a `SKILL.md`. A comment needing
  a reason must **state** the reason; too long to state means it belongs in
  `docs/` or a commit message.
- Agent files may point at source, `docs/`, `CONTRIBUTING.md` and each other
  freely. What rots is a pointer at *content* — a build filter matching a
  filename is not one.
- Same rule outward: agent instructions stay out of anything published.
  `mkdocs.yml` enforces it without naming a file — every `.md` under `docs/`
  must appear in the nav, so a stray draft breaks `mkdocs build --strict`
  instead of quietly publishing.
- **Local scratch notes** (`*_handoff.md`, `CLAUDE.local.md`) are never
  committed, never referenced from anything committed, and never added to
  `.gitignore` — that is the maintainer's global ignore file's job.
  `git add -A` sweeps them in; stage deliberately. `.claude/.gitignore` is not
  a counter-example: what it excludes is named by the *tool*, not by a person.

## What this is

- Shell scripts run when you `cd` into or out of a directory, matched by path
  pattern. A Go rewrite of [ondir](https://github.com/alecthomas/ondir);
  [docs/design-notes.md](docs/design-notes.md) has the points it departs from.
- **What ships is what `envoke help` lists.** A gap between `envoke help`,
  README's Status section, `docs/` and the code is a bug to report, not an
  unfinished feature. Packaging is live, not scaffolding.
- Build in the order the codebase establishes — matching engine, shell
  integration, trust, packaging.

## Where each design principle is enforced

[docs/design-notes.md](docs/design-notes.md) has the principles and wins if
this table drifts. Only the enforcement point belongs here.

| Principle | Enforced by |
|---|---|
| RE2 matching only | `internal/config`'s use of stdlib `regexp`. Never vendor a backtracking engine. |
| Path-segment matching | the `^(?:...)$` wrapper in `compilePattern`. Never a raw `strings.HasPrefix`. |
| Enter/leave independent | `internal/envoke.Transition` runs leaves then enters and unwinds nothing. No auto-undo. |
| Trust before execution | `configset.Decide`, called by `internal/envoke.Transition` and `cmd/envoke`'s `mayRun`. No other path may execute a block. |
| Never discover a config the user doesn't own | `cmd/envoke`'s `locateConfigs`, the only thing that resolves *where* configs live. An earlier iteration walked the filesystem; design-notes records what that cost. |
| A config pointing out of the config directory is confined | `matcher.newMatch`, with symlinks resolved on both sides. |
| One binary generates all shell integration | `internal/shellinit`, static strings per shell. |
| A config feeding a trust decision is read exactly once | `config.LoadFile` returns the bytes with the parsed config; `cmd/envoke`'s review path takes its bytes from the entry. A second read is a TOCTOU hole, not a refactor. |

## Package map

Every package opens with a doc comment saying what it owns; this is only
enough to route you there.

| Package | Routing | |
|---|---|---|
| `internal/config` | parsing, locating, patterns, permissions | [notes](internal/config/CLAUDE.md) |
| `internal/fsperm` | "writable by someone else", per platform | [notes](internal/fsperm/CLAUDE.md) |
| `internal/matcher` | what a `cd` left and entered; which blocks match | [notes](internal/matcher/CLAUDE.md) |
| `internal/configset` | *which* configs envoke acts on; the trust rule | [notes](internal/configset/CLAUDE.md) |
| `internal/executor` | running a matched block: `Run`, `Render` | [notes](internal/executor/CLAUDE.md) |
| `internal/envoke` | `Transition`, the loop behind `envoke exec` | [notes](internal/envoke/CLAUDE.md) |
| `internal/state` | data home; the disable/enable off switch | [notes](internal/state/CLAUDE.md) |
| `internal/trust` | content-hash trust records | [notes](internal/trust/CLAUDE.md) |
| `internal/shellinit` | hook and completion scripts, five shells | [notes](internal/shellinit/CLAUDE.md) |
| `cmd/envoke` | dispatcher and all terminal output | [notes](cmd/envoke/CLAUDE.md) |
| `.dagger/` | CI checks and packaging (separate module) | [notes](.dagger/CLAUDE.md) |

## Go conventions

- **Verify only through `dagger call -m .dagger <check>`**, never the Go
  toolchain on the host: Dagger runs the pinned images CI uses. A `SKIP` means
  run it through Dagger, not that the case is covered.
- **Comments carry what the code can't say, and nothing else** — csh expanding
  `!` inside single quotes, bash 3.2 lacking `mapfile`, why the hash record is
  written last. Not what the code used to do, not which bug a line came from:
  that is what commit messages and `docs/design-notes.md` are for.
- Static, dependency-free binaries — no cgo, zero non-stdlib imports.
- Table-driven tests via subtests (`Test<Func>_<Scenario>`), including a
  regression test for each ondir bug fixed. `go test ./... -race` must pass.
- **Every package's tests run on all three platforms.** That is a rule about
  fixtures: a path or pattern goes through the package's own helper (`tp`/`np`
  and neighbours — the package's notes say which spelling a *pattern* takes
  and which a *printed path* takes), never hardcoded Unix-style; a case
  needing something a platform lacks skips itself with a reason; a helper
  isolating `$HOME` sets **both** `HOME` and `USERPROFILE`.
- Windows and macOS run on real runners — the `native` job in
  `.github/workflows/ci.yml`, whose comment states the matrix.
- **CLI framework: stdlib `flag`; config parser: hand-rolled.** Both
  deliberate, both with a revisit condition — see
  [cmd/envoke](cmd/envoke/CLAUDE.md) and
  [internal/config](internal/config/CLAUDE.md).

### What `.claude/settings.json` covers, and where it stops

A list of command spellings, not a boundary — taking it for one is the worse
failure, because you stop checking. It denies the Go toolchain, `goreleaser`,
`publish` and the two tree-writing exports **in the spellings it lists**, and
allows a prefix on `dagger call -m .dagger`. No deny entry can bound a prefix:
a prefix match sees neither inside a flag value nor through a wrapper, so a
Dagger secret flag given `cmd://<any command>` runs that command **on the
host, unprompted**, and `source with-new-file … export --path=.` writes the
tree through the same allow. Verifying only through Dagger holds because you
follow it. A bypass you find belongs in the deny list; narrowing the allow is
the maintainer's call.

## Editing CONTRIBUTING.md

`docs/contributing.md` embeds it verbatim, so **`CONTRIBUTING.md` names
repository files in prose and never links to them**: a relative link breaks
under `docs/`'s path, and a `github.com/...` URL pins to `main`, so a tagged
release's page would point at a newer document. In-page anchors and external
links are fine.

## Don't

- Don't add a feature before the capability it depends on works and is tested.
- Don't add implicit or automatic leave behavior.

# CLAUDE.md

How to work on `envoke` as an agent. This is **not** project documentation and
must never become it:

| Question | Where the answer lives |
|---|---|
| What envoke does, and how it's used | [README.md](README.md), [docs/](docs/) |
| Why it's designed this way | [docs/design-notes.md](docs/design-notes.md) |
| How to build, test and submit a change | [CONTRIBUTING.md](CONTRIBUTING.md) |
| What holds across the whole codebase | here |
| Where a rule is enforced in *this* package, and what a refactor would break | that package's own `CLAUDE.md` |
| The steps of a ritual that spans files, which no single file states | `.claude/skills/*/SKILL.md` |

**The code is the truth; the docs are the intent.** Read the docs for what a
thing is meant to do, then verify against the source. If this file and a docs
page disagree, the docs page wins and this file needs fixing — never the
reverse.

**Don't restate documented behaviour.** Add only what a reader of the source
needs and the docs have no reason to carry: which function enforces a rule,
which failure it was written against, what a plausible-looking refactor would
break.

**Per-package detail lives next to the code.** Each package has its own
`CLAUDE.md`, loaded only once a file in its directory is read — a session on
the shell hooks shouldn't pay for eleven packages' notes. Read one before
changing that package, and put new package-specific findings there rather than
growing this file back.

**Prefer the route to a fact over a copy of it.** Whatever a command or a doc
comment already reports — `dagger functions -m .dagger` (names *and*
descriptions, ~2s), the subcommands `envoke help` prints, a signature, a
flag's default, what a package owns (its doc comment) — belongs in no
agent-instruction file. Record how to find it and spend the space on what
nothing reports. A copied list goes stale silently and is believed anyway.

**Agent instructions are referenced only by other agent instructions.** They
are the `CLAUDE.md` files and `.claude/skills/*/SKILL.md`; they have no
stability promise and get split, renamed and rewritten, so no Go comment,
README, `CONTRIBUTING.md`, workflow, `renovate.json` or script header may
point at one. A comment needing a reason must **state** the reason; too long
to state means it belongs in `docs/` or a commit message. Agent files may
point at source, `docs/` and `CONTRIBUTING.md` freely, and at each other.

What rots, and what the rule therefore forbids, is a pointer at *content*. A
build filter that matches a filename is not one: if the file is renamed the
pattern stops matching and nothing is left asserting otherwise. So the
corollary — not an exception to the rule but the same rule pointed outward —
is that agent instructions stay out of anything published. `mkdocs.yml`
enforces that without naming a file: every `.md` under `docs/` must appear in
the nav, and `mkdocs build --strict` fails otherwise, so a `CLAUDE.md`, a
stray draft or a `*_handoff.md` dropped there breaks CI instead of quietly
publishing at its own URL.

**Local scratch notes** (`*_handoff.md`, `CLAUDE.local.md`) are never
committed, never referenced from anything committed, and never added to
`.gitignore` — keeping them out is the maintainer's own global ignore file's
job, and this project's `.gitignore` is for build output. `git add -A` sweeps
them in; stage deliberately. `.claude/.gitignore` is not a counter-example: what
it excludes is named by the *tool*, not by a person — `settings.local.json` by a
convention that already means "not shared", `worktrees/` by Claude Code creating
it — so it states where the tool puts things rather than one person's habits. A
file you chose the name of does not belong there.

## What this is

`envoke` runs shell scripts when you `cd` into or out of a directory, matched
by path pattern (`enter`/`leave` blocks in a config file). A Go rewrite of
[ondir](https://github.com/alecthomas/ondir), unmaintained for years;
[docs/design-notes.md](docs/design-notes.md) has the points it departs from.
Static, dependency-free binary; no cgo.

## Status

**What ships is what `envoke help` lists**, and README's Status section is the
prose version. Everything there is implemented, tested under `dagger check -m
.dagger` and documented in `docs/` — a gap between the three is a bug to
report, not a feature that was never finished. Packaging is live, not
scaffolding ([.dagger notes](.dagger/CLAUDE.md) has the parts the config
doesn't explain).

Build in the order the codebase already establishes — matching engine, shell
integration, trust, packaging — rather than polishing a later capability
before an earlier one is solid and tested.

## Design principles

The principles live in [docs/design-notes.md](docs/design-notes.md) — read
them there; that page is what CONTRIBUTING points contributors at, and it wins
if this file drifts. What belongs here is only where each is enforced, so a
refactor can't quietly step around one:

| Principle | Enforced by |
|---|---|
| RE2 matching only | `internal/config`'s use of stdlib `regexp`. Never shell out to or vendor a backtracking engine. |
| Path-segment matching | The `^(?:...)$` wrapper in `compilePattern`. Never a raw `strings.HasPrefix`. |
| Enter/leave independent and explicit | `internal/envoke.Transition` runs leaves then enters and unwinds nothing on failure. No "smart" auto-undo; if ever wanted, opt-in, not default. |
| Trust before execution | `configset.Decide`, called by `internal/envoke.Transition` and `cmd/envoke`'s `mayRun`. No other path may execute a block. |
| Never discover a config the user doesn't own | `cmd/envoke`'s `locateConfigs` is the only thing that resolves *where* configs live, via `config.Locate`/`config.LocateDir`; `configset.Load` is handed those two paths and can reach nothing else. An earlier iteration walked the filesystem; [docs/design-notes.md](docs/design-notes.md) records what that cost, and it is not to be reintroduced without answering it. |
| A config pointing out of the config directory is confined | `matcher.NewMatch` refuses any match outside `Config.Dir` for a `Local` config, whatever its patterns say — with symlinks resolved on both sides, since that config's `Dir` and pattern base are physical. |
| One binary generates all shell integration | `internal/shellinit`, static strings per shell — never hand-maintained script files. |
| A config feeding a trust decision is read exactly once | `config.LoadFile`/`LoadFragmentResolved` return the source bytes alongside the parsed config, `configset.Entry` carries them, and `cmd/envoke`'s review path takes its bytes from that entry rather than reading again. A second read is a TOCTOU hole, not a refactor. |

## Package map

`internal/` is not importable outside this module — no stable public API to
commit to yet. Every package opens with a doc comment saying what it owns; the
column below is only enough to route you there.

| Package | Routing | Notes |
|---|---|---|
| `internal/config` | parsing, locating, patterns, permissions | [notes](internal/config/CLAUDE.md) |
| `internal/fsperm` | "writable by someone else", per platform | [notes](internal/fsperm/CLAUDE.md) |
| `internal/matcher` | what a `cd` left and entered; which blocks match | [notes](internal/matcher/CLAUDE.md) |
| `internal/configset` | *which* configs envoke acts on; the trust rule | [notes](internal/configset/CLAUDE.md) |
| `internal/executor` | running a matched block: `Run`, `Render` | [notes](internal/executor/CLAUDE.md) |
| `internal/envoke` | `Transition`, the core loop behind `envoke exec` | [notes](internal/envoke/CLAUDE.md) |
| `internal/state` | data home; the disable/enable off switch | [notes](internal/state/CLAUDE.md) |
| `internal/trust` | content-hash trust records | [notes](internal/trust/CLAUDE.md) |
| `internal/shellinit` | hook and completion scripts, five shells | [notes](internal/shellinit/CLAUDE.md) |
| `cmd/envoke` | dispatcher and all terminal output | [notes](cmd/envoke/CLAUDE.md) |
| `.dagger/` | CI checks and packaging (separate Go module) | [notes](.dagger/CLAUDE.md) |

## Go conventions

- **Verify only through `dagger call -m .dagger <check>`** — never the Go
  toolchain on the host. Dagger runs the pinned images CI uses rather than
  whatever is installed locally; that is the point of `.dagger`, and
  `dagger functions -m .dagger` is where the check names live. A `SKIP` means
  run it through Dagger, not that the case is covered.
- **What `.claude/settings.json` covers, and where it stops** — it is a list of
  command spellings, not a boundary; take it for a boundary and you stop
  checking, which is the worse failure. It denies, by command
  prefix, `go` (the whole command, not a list of verbs), `gofmt`,
  `golangci-lint`, `govulncheck`, `goreleaser` and `gotestsum`, plus `publish`
  and the two exports that write into the tree, `autofix export` and
  `snapshot export`, in the argument spellings the file lists — which is not
  every spelling either of them can be written in. The allow side is a prefix on
  `dagger call -m .dagger` so a new check needs no edit here, and the narrower
  allows are scoped to what they are for: `dagger functions` to this module,
  since an unscoped one would run a third-party module unprompted, and
  `git branch` to `--list`, since the rest of that list is read-only. What no
  deny entry can do is bound that broad prefix. A prefix match sees neither
  inside a flag value nor through a wrapper (a leading `VAR=value`, `env`,
  `sh -c`), so a Dagger secret flag given `cmd://<any command>` runs that
  command **on the host, unprompted** — the argv never contains the string a
  deny entry would match — and `source with-new-file … export --path=.` writes
  the tree through the same allow. So verifying only through Dagger holds
  because you follow it, not because this file makes it hold. A bypass you find
  belongs in the deny list; narrowing the allow is the maintainer's call, not a
  session's.
- Static, dependency-free binaries — no cgo, zero non-stdlib imports. Keep it
  that way unless a real need appears.
- **Comments carry what the code can't say, and nothing else** — csh expanding
  `!` inside single quotes, bash 3.2 lacking `mapfile`, why the hash record is
  written last. Not what the code used to do, which bug a line came from, or
  which section of this file justifies it: that is what commit messages and
  `docs/design-notes.md` are for. Narrated history buries the invariant a
  reader needs and drifts out of date.
- Table-driven tests via subtests (`Test<Func>_<Scenario>`), including a
  regression test for each ondir bug fixed. `go test ./... -race` must pass.
- **Windows and macOS are tested on real runners** — the `native` job in
  `.github/workflows/ci.yml`, whose own comment states the matrix and which
  tests skip themselves where — read it there rather than keeping a copy here.
  Dagger's engine runs Linux containers and cannot;
  [.dagger notes](.dagger/CLAUDE.md) records why Wine is not a substitute, and
  that is settled. Every package's tests run on all three platforms, which is a
  rule about fixtures, not a list of helpers: a path or a pattern is built
  through the package's own helper (`tp`/`np` and their neighbours — that
  package's notes say which spelling a *pattern* takes and which a *printed
  path* takes) and never hardcoded Unix-style; a case that needs something a
  platform lacks skips itself with a reason rather than being left out; and a
  helper isolating `$HOME` sets **both** `HOME` and `USERPROFILE`, since
  `os.UserHomeDir` reads a different one per platform.
- **CLI framework: stdlib `flag`; config parser: hand-rolled.** Both
  deliberate, both with a revisit condition — see
  [cmd/envoke](cmd/envoke/CLAUDE.md) and
  [internal/config](internal/config/CLAUDE.md).

## Editing CONTRIBUTING.md

`docs/contributing.md` embeds it verbatim via `pymdownx.snippets`, so
**`CONTRIBUTING.md` names repository files in prose and never links to them**:
a relative link breaks once rendered under `docs/`'s own path, and an absolute
`github.com/...` URL pins to `main`, so a tagged release's page would send
readers to a newer document than the one they're reading. In-page anchors
(`#how-to-help`) and genuinely external links are fine.

`.github/workflows/docs.yml` builds with `mkdocs build --strict` on push to
`main`, behind a paths filter the workflow itself states — a change outside it
does not deploy, so a strict-build failure is one CI reaches only when the
filter matches. `dagger call -m .dagger docs-build` runs the same strict build
on any change.

## Don't

- Don't add a feature before the capability it depends on works and is tested
  (e.g. don't polish packaging before trust is solid).
- Don't add implicit/automatic leave behavior — re-read the "enter/leave are
  independent" principle first.

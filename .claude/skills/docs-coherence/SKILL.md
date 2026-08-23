---
name: docs-coherence
description: Audit that envoke's four descriptions of itself still agree — `envoke help`, README's Status section, docs/, and the code — and that agent notes still cite identifiers that exist. Use before a release, after adding or changing a command or flag, or when a docs page looks stale.
---

# Documentation coherence audit

envoke describes itself in four places, and the project treats a gap between
them as a bug rather than an unfinished feature: **what ships is what `envoke
help` lists**, README's Status section is the prose version, and everything
there is implemented, tested and documented in `docs/`.

Two of those relationships are enforced by a test, and only in one direction.
The rest drift silently, which is what this audit is for.

## Already automated — don't re-check by hand

Two tests, covering different halves, and knowing which is which is the point:

- `TestRun_CompletionCoversEverySubcommand` (`cmd/envoke`) checks every command
  `envoke help` documents against the generated **bash** completion script. It
  has already caught `completion` forgetting itself. It does **not** check the
  reverse, so a completion candidate for a command that no longer exists passes.
- `TestCompletion_ListsEverySubcommand` (`internal/shellinit`) checks every name
  in the `subcommands` slice against all three generated scripts.
- `dagger call -m .dagger docs-links` compares `docs/llms.txt`'s site URLs
  against `mkdocs.yml`'s nav, **both** directions, reading the site URL and the
  page list out of `mkdocs.yml` rather than hardcoding either. It closes the gap
  the strict build leaves: `llms.txt` is a `.txt`, so `omitted_files` never
  walks it, and its links are absolute, so `validation.links` never resolves
  them. It does not check the notes beside those links — item 10.

Nothing machine-checks the dispatcher against `usage()`, `usage()` against
`docs/reference.md`, or `docs/reference.md` against README — those are below.

## What drifts

Work through these; each is a diff between two sources of truth.

1. **`usage()` (bottom of `cmd/envoke/main.go`) vs `docs/reference.md`.**
   `usage()` is the version users see. `reference.md` claims to be the
   complete list of commands, flags, environment variables read *and*
   exported, files, and exit codes. Check each section against the code, not
   against `usage()` alone — `reference.md` documents things `usage()` has no
   room for.

2. **README's Status section vs `docs/index.md`'s Status admonition.** Two
   prose summaries of the same thing, maintained separately.

3. **`CONTRIBUTING.md`'s "Verifying your change" table vs the real functions.**
   `dagger functions -m .dagger` lists them with descriptions in about two
   seconds. Membership of the check set is the `// +check` pragma and nothing
   else — `grep -c '^// +check$' .dagger/main.go` is the honest count, and
   several doc comments mention `+check` precisely to say the function *isn't*
   one. Don't infer it from prose.

4. **`CONTRIBUTING.md` must name repository files without linking to them.**
   `docs/contributing.md` embeds it verbatim via `pymdownx.snippets`, so a
   relative link breaks under `docs/`'s path and an absolute `github.com/...`
   URL pins to `main`. In-page anchors and genuinely external links are fine.

5. **Agent notes citing identifiers that no longer exist.** The highest-yield
   check, because nothing else catches it. For every backticked Go identifier
   in a `CLAUDE.md` or `SKILL.md`, confirm it still exists:

   ```
   grep -ohE '`[A-Za-z_][A-Za-z0-9_.]*`' \
       $(git ls-files '*CLAUDE.md' '.claude/skills/*/SKILL.md') \
     | tr -d '`' | sed 's/.*\.//' | sort -u \
     | while read -r id; do
         grep -rqE "\b$id\b" --include='*.go' . || echo "MISSING: $id"
       done
   ```

   Filter the result by hand — it flags prose words and filenames too. A real
   hit looks like a package-qualified function that was renamed and cited for
   months afterwards.

   **A citation that resolves can still be wrong**, and this check cannot see
   it: `sed 's/.*\.//'` throws away the package qualifier, so a note naming the
   wrong package or the wrong caller passes clean. Open the function for
   anything a note makes a *claim* about — "X reads Y" and "X is handed Y" look
   identical to the grep and are different facts about who enforces a rule. Same
   blind spot for `.md` file references, which the `sed` reduces to `md`.

6. **The Dagger version pin, and Go comments.** Renovate holds both sides of the
   version pin — `dagger.json`'s `engineVersion` and each workflow's `version:`
   — grouped into one PR, so the audit question is not "did they drift" but "is
   every file that pins it listed": compare `renovate.json`'s
   `managerFilePatterns` against `grep -rn 'version:' .github/workflows/`. While
   there, the check in item 5 is scoped to agent files, but a Go doc comment is
   just as much a route-to-a-fact — run the same sweep over `--include='*.go'`
   comment text when a rename lands.

7. **Prose claims about behaviour, not just literals.** A sentence in `docs/`
   asserting an *ordering*, a *precedence*, or *what is skipped* is a claim about
   a specific function — open it. Two cheap tells that a page is wrong: it
   contradicts another page (cheaper to spot than doc-vs-code, and a reliable
   proxy for it), or it contradicts itself further down. An example that reads
   right and matters is worse than none: a skip rule illustrated with a filename
   the rule doesn't actually skip is what a reader pattern-matches on.

8. **Shown command output against the `fprintf` that emits it.** The single most
   reliable check available, because the padding and wording are exact
   everywhere — so a line that has quietly lost a clause, or gained a `<path>`
   the code never prints, stands out. An elision must be marked as one.

9. **Which pages did *not* change.** When a feature lands, list the `docs/`
   pages the change left alone and ask why each was exempt. That is how a page
   predating a whole subsystem gets found; nothing else surfaces it.

10. **`docs/llms.txt`'s per-link notes.** The URL half is automated — see
    `docs-links` under "Already automated" above — and the notes are not.
    Each link carries a one-line summary of what that page *holds*, so moving
    a section from one page to another leaves both URLs valid and both notes
    wrong, which no diff of page lists can see. Item 9 is the same question
    from the other end: when a page changes, ask what in `llms.txt` claimed to
    describe it.

## The rule when two sources disagree

The code is the truth; the docs are the intent. A docs page wins over a
`CLAUDE.md`, and this ordering is not symmetric — **never resolve a
disagreement by editing a docs page to match an agent note.** Fix the note, or
fix the code, or file the gap.

## Verifying a docs change

`mkdocs build --strict` is what CI runs, and
`dagger call -m .dagger docs-build` is that same strict build as a check, so a
docs change can be verified before merging rather than after. To read the
result, `dagger -m ./.dagger call docs up --ports 8000:8000` serves it locally
without installing Python.

`validation.links` raises unrecognized links, absolute links and dead anchors
to warnings, so `--strict` fails on a `[...](page.md#section)` whose heading has
since been renamed. Item 10's `llms.txt` diff is the gap that guard leaves: its
links are absolute URLs in a `.txt`, which nothing validates.

A new page must be added to `mkdocs.yml`'s `nav` in the same change:
`validation.nav.omitted_files` is raised to `warn`, and `--strict` turns that
into a failure. That is deliberate — an unlisted `.md` would otherwise build
and publish at its own URL with nothing but an `INFO` line to say so.

---
name: add-subcommand
description: Add a subcommand to the envoke CLI. Use when adding, renaming or removing an `envoke <verb>` command — it lists the files that must change together, which no single one of them states.
---

# Adding an envoke subcommand

A subcommand is spread over two packages and one docs page, in seven places.
Two of them have a test that fails if you skip them; the rest fail silently,
which is why this list exists.

Read `cmd/envoke/CLAUDE.md` before starting — it has the reasoning behind the
conventions below.

## The files that must change together

1. **`cmd/envoke/main.go` — the `switch` in `run()`.** Add the `case` and a
   `cmdX(args []string, stdout, stderr io.Writer) int`. `run` returns an exit
   code and never calls `os.Exit`, so it stays testable.

2. **`cmd/envoke/main.go` — `usage()`, at the bottom.** This is what
   `envoke help` prints and the user-visible catalogue: the command, its
   flags, its defaults. Nothing else duplicates it, and
   `TestRun_CompletionCoversEverySubcommand` parses it.

3. **`internal/shellinit/shellinit.go` — four places, not one.** No generated
   script reads the `subcommands` slice; each completion hardcodes its own list.
   So the name goes in **all** of: `subcommands` (alphabetical), the `-W "..."`
   word list in `bashCompletion`, the `_envoke_cmds` array in `zshCompletion`
   (with a description), and a `complete -c envoke -n __fish_use_subcommand -a
   <name> -d '<desc>'` line in `fishCompletion`. Adding it to `subcommands`
   alone completes the command in no shell.

   Two tests split the net between them, and neither covers the other's half:
   `TestCompletion_ListsEverySubcommand` (`internal/shellinit`) fails if a name
   in `subcommands` is missing from any of the three scripts;
   `TestRun_CompletionCoversEverySubcommand` (`cmd/envoke`) compares
   `envoke help` against the generated **bash** script only. Separately, if the
   command takes an argument *type* worth completing, extend the per-subcommand
   cases in each of the three functions — that part is conditional, the four
   list edits above are not.

4. **`docs/reference.md` — the Commands section.** The complete user-facing
   list; a command absent here is undocumented even though `envoke help`
   knows it.

## Conventions that are not optional

- **Flags:** one `flag.FlagSet` per subcommand via `newFlagSet`, never
  hand-rolled argument scanning. Accept `--` before positionals if the command
  can receive a path.
- **Output:** every write goes through the local `fprintf`/`fprintln`/`fprint`
  helpers, never `fmt.Fprintf` directly. They escape every `string` and
  `error` argument, because config text and directory names are
  attacker-controllable and both reach the terminal. Only generated shell code
  and envoke's own usage text use the `raw` type — `grep 'raw('` is the
  complete list of exceptions, and widening it is what
  `TestRun_GeneratedShellCodeIsNeverEscaped` exists to stop.
- **If the command can execute a block**, it must go through
  `configset.Decide` first. That is the only trust gate, and no new path may
  route around it.
- **Tests:** table-driven subtests named `TestRun_<Scenario>` in
  `cmd/envoke/main_test.go`, which CI runs on all three platforms. So no fixture
  may hardcode a Unix-style absolute path: build it with that file's helpers, and
  the choice between them is directional. A **pattern** takes `tp` — forward
  slashes, volume-prefixed, because a pattern is a regex matched against
  `matcher.MatchPath` and `C:\a` would read `\a` as an escape. A **path envoke
  printed back** takes `np`, the platform's native form, because it went through
  `filepath.Clean`. `configBody` applies the same prefixing to every absolute
  pattern in a whole config body, and `resolvedPath` gives the spelling envoke
  reports for a file under `$ENVOKERC_D`, whose walk resolves before walking.
  Swapping `tp` and `np` is how this breaks, and it breaks on Windows only. A
  case needing something a platform lacks skips itself — `requirePOSIXShell`,
  `requirePermissionBits` — rather than being left out. `cmd/envoke/CLAUDE.md`'s
  Testing section has the reasoning.

## Verifying

`dagger call -m .dagger test`, then `dagger call -m .dagger test-shell-bash`
(and `zsh`/`fish`) if you touched completion. Never `go test` on the host.

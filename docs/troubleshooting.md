---
title: Troubleshooting
description: >-
  Why a block didn't fire, in the order the causes actually come up — each one
  with the envoke debug line that confirms it and the fix.
---

# Troubleshooting

Almost every "why didn't that happen?" has the same first step.

## Run this first

```sh
cd ~/work/api/src
envoke debug
```

With no arguments, `envoke debug` takes `<to>` from the directory you are
standing in and `<from>` from `$OLDPWD` — exactly the move that just didn't do
what you expected. It never executes anything, so it is safe to run against a
config you are still editing.

```
envoke debug: /home/you/work/api -> /home/you/work/api/src
  config /home/you/.envokerc (trusted)
  config /home/you/.config/envoke/envokerc.d/10-work (NOT trusted -- run `envoke allow …`)
  note: via the shell hook these run in /home/you/work/api/src, where your shell lands;
        via `envoke exec` each runs in the directory it matched.
        $ENVOKE_DIR always names the matched directory -- use it for relative paths.
  enter /home/you/work/api (line 3 of /home/you/.envokerc: ~/work/([^/]+))
    source "$ENVOKE_DIR/venv/bin/activate"
```

Four things to read, in this order:

1. **The `config` lines** — every config envoke loaded, and whether it would
   run. If the file you edited isn't listed at all, jump to
   [the fragment isn't being loaded](#7-a-fragment-isnt-being-loaded-at-all);
   if it is listed as `failed to load`, jump to
   [it doesn't parse](#8-a-fragment-is-loaded-but-doesnt-parse). A `config`
   line may be followed by indented notes — the file a symlinked fragment
   leads to, and the directory a confined one is bounded to; see
   [§9](#9-a-symlinked-project-fragment-points-out-of-its-project).
2. **The `note:` block**, printed only when a matched directory is not the one
   your shell landed in. It is the answer to
   [§3](#3-the-block-fired-but-ran-somewhere-else) before you have asked the
   question.
3. **The block lines** — what would fire, in order, each naming the file and
   line it came from. Nothing listed means nothing matched: see
   [the pattern doesn't match](#2-the-pattern-doesnt-match-a-whole-segment).
4. **Any extra line** — `envoke debug` reports the off switch here too.

**On PowerShell and tcsh, name the directory you came from.** `$OLDPWD` is a
POSIX shell convention, and neither of those two follows it. PowerShell has no
`$OLDPWD` at all, so a bare `envoke debug` there fails loudly: it has nothing to
infer `<from>` from and says so. tcsh fails quietly, which is worse — it
maintains `$owd` instead, so any `$OLDPWD` a tcsh has was inherited from the
shell that started it and has not moved since, and `envoke debug` reports a
transition that looks plausible and is wrong. Nothing flags it, because an
inherited `$OLDPWD` is an ordinary environment variable and envoke cannot tell
it from a live one. (Your tcsh hook is fine: it passes `$owd` itself. This is
only about commands you type.)

One argument is `<from>`, with `<to>` still taken from where you are — in tcsh:

```sh
envoke debug ~/work/api
```

and in PowerShell:

```powershell
envoke debug C:\work\api
```

That one form covers both shells, works in every other one too, and stands in
for a bare `envoke debug` wherever this page asks for one.

---

## 1. The config isn't approved, or was edited since

**Symptom:** the block is listed by `envoke debug`, but nothing happens on
`cd`. On stderr you get a line about the config not being trusted.

**Confirm:**

```sh
envoke list
```

```
envoke: configs envoke would load
  changed   fragment  /home/you/.config/envoke/envokerc.d/10-work
```

`changed` means you approved it once and edited it since. `untrusted` means it
was never approved. Either way nothing in it runs.

**Fix:** `envoke allow` — with no argument it reviews every config at once.
Editing a config always revokes its approval, deliberately: that is what stops
a change being smuggled into something you already trusted. See
[Trust Model](trust.md).

Freshly approved configs apply from your **next** `cd`, because `envoke allow`
runs as a child of your shell. To apply one where you are standing:

```sh
eval "$(envoke reload)"
```

## 2. The pattern doesn't match a whole segment

**Symptom:** `envoke debug` lists your config as trusted but shows no block
for the directory you expected.

Patterns are anchored and match **whole path segments**. `~/work/foo` does not
match `~/work/foobar`, and it does not match `~/work/foo/src` either — that is
a different directory.

**Confirm:** `envoke debug <from> <to>` with explicit paths lets you probe a
transition without moving:

```sh
envoke debug ~/work ~/work/foo
```

**Fix:** write the pattern for the directory you actually mean:

```
enter ~/work/foo          # fires for ~/work/foo, and nothing else
enter ~/work/foo/[^/]+    # fires for each directory one level under it
enter ~/work/foo/.*       # `.` matches `/` too, so this fires for every
                          # directory at any depth under it -- once for each
                          # level a single cd passes through
enter ~/work/([^/]+)      # fires once per project directory
```

**Watch for:** an undefined `$VAR` in a pattern is a parse error, not an empty
string — envoke refuses the config rather than compiling a pattern that can
never match. Patterns are also case-sensitive even where the filesystem is not,
so `~/work/api` does not match a `cd ~/work/API` on macOS or Windows. And on
Windows, patterns are written with `/` even though paths use `\`. See [Path
patterns](configuration.md#path-patterns).

**If the config is a fragment symlinked in from a project**, two things other
than the pattern's shape can refuse it, and they want opposite fixes: the
confinement bound, where rewriting the pattern is the wrong move
([§9](#9-a-symlinked-project-fragment-points-out-of-its-project)), and a
difference in case between the pattern and the directory, where rewriting it is
the only move ([§10](#10-the-directory-and-the-pattern-differ-in-case)).

## 3. The block fired, but ran somewhere else

**Symptom:** the block runs — you can see it in `envoke debug` — but a
relative path inside it resolves to the wrong place, or `source venv/bin/activate`
reports "no such file".

Through the shell hook, a block runs **where your shell landed**, not in the
directory it matched. `cd ~/work/api/cmd/srv` fires `~/work/([^/]+)` once, for
`~/work/api`, while your shell sits three levels below it.

**Confirm:** `envoke debug` prints a note whenever the two differ.

**Fix:** use `$ENVOKE_DIR`, which always names the matched directory:

```
enter ~/work/([^/]+)
    . "$ENVOKE_DIR/venv/bin/activate"
```

See [Where the script runs](configuration.md#where-the-script-runs).

## 4. envoke is switched off

**Symptom:** nothing fires anywhere, for any config.

**Confirm:** `envoke debug` says so on its own line:

```
  envoke is disabled by the persistent switch -- nothing below would run; re-enable with `envoke enable`
```

**Fix:** `envoke enable`, or `unset ENVOKE_DISABLE` for a single shell. The two
switches are independent and the environment variable wins — see
[Turning envoke off](debugging.md#turning-envoke-off).

## 5. The hook isn't installed in this shell

**Symptom:** `envoke debug` looks completely right, and still nothing happens
on `cd`.

`envoke debug` doesn't need the hook; the hook is what turns a real `cd` into a
call to envoke. If the two disagree, the hook is missing.

**Confirm:**

```sh
# bash / zsh
type _envoke_hook

# fish
functions _envoke_hook

# tcsh
alias cwdcmd
```

**Fix, in order of likelihood:**

- **The shell was started before you added the hook.** Open a new one.
- **The line is in a file this shell doesn't read.** The table in
  [Getting Started](getting-started.md#shell-integration) names the file for
  each shell — and a *login* shell may read `~/.bash_profile` or `~/.profile`
  and never touch `~/.bashrc`, which is what macOS terminals do by default.
  Source the file by hand (`. ~/.bashrc`) and re-run the check above: if the
  hook appears then, the file isn't being read for you.
- **This isn't the shell you installed the hook for.** `ps -p $$ -o comm=`
  names the shell you are actually in, which is not always what `$SHELL` says.

**Not a cause:** having put the line in `~/.zshenv` or `~/.cshrc`. Those files
are read by non-interactive shells too, which is why the hook checks for an
interactive shell before installing itself — but they are read by *interactive*
shells as well, so the hook is there and works. `~/.zshrc` / `~/.tcshrc` are
still the right home for it, since a hook in the other two is evaluated by
every `zsh -c` and `tcsh -c` for nothing; it just isn't what is wrong here.

## 6. The block ran and failed silently

**Symptom:** the block fires, but its effect is missing, and `$?` is 0.

This is by design, and it is two decisions rather than one:

- A failing block **does not stop** the ones after it. Your shell evaluates
  every matched block as one script.
- A failure **does not reach `$?`**. Every hook saves the exit status it was
  entered with and restores it, so an exit-code-aware prompt reports *your*
  last command rather than envoke's.

The cost is that a failure shows up on stderr and nowhere else.

**Fix:** say so in the block itself when it matters:

```
enter ~/work/api
    . "$ENVOKE_DIR/venv/bin/activate" || echo "envoke: venv activation failed" >&2
```

See [When a block fails](configuration.md#when-a-block-fails).

## 7. A fragment isn't being loaded at all

**Symptom:** the file you edited does not appear in `envoke debug`'s `config`
lines. Nothing else on this page applies until it does.

**Confirm:** the `config` lines list every file, loaded or failed, with its
status. A file that loaded but has nothing to say about this transition still
appears there — so if it is absent, envoke never read it.

This is the one that bites when you start splitting rules across
[`envokerc.d`](configuration.md#the-envokercd-directory). Causes, in order:

- **The wrong directory.** envoke looks in `$ENVOKERC_D`, then `~/.envokerc.d`,
  then `$XDG_CONFIG_HOME/envoke/envokerc.d` — and it uses the **first** of
  those that exists. If you have both `~/.envokerc.d` and one under
  `~/.config`, only the first is read.
- **The name is skipped.** Names starting with `.` or ending with `~` are
  ignored, on purpose: `.10-work` and `10-work~` are invisible. Nothing else
  is — `10-work.bak` and `10-work.swp` are **not** skipped, they are loaded as
  configs, so a scratch copy left in the directory usually turns up under
  [§8](#8-a-fragment-is-loaded-but-doesnt-parse) instead of vanishing.
- **A broken symlink.** A fragment linked to a file that has moved is reported
  on stderr rather than skipped silently — check there.
- **It's a directory, not a file.** A symlink pointing at a directory is not a
  fragment.
- **The whole directory hit a bound.** Then no fragment loads at all — see
  [§11](#11-the-fragment-directory-hit-a-bound).

**On ordering:** fragments apply in order of their path relative to the
directory, which is why `10-`/`20-` prefixes work. If two fragments touch the
same thing and the wrong one wins, that ordering is the reason — `envoke debug`
prints the blocks in the order they would run.

## 8. A fragment is loaded but doesn't parse

**Symptom:** the file appears in `envoke debug`'s `config` lines, but as
`failed to load` instead of a trust status, and none of its blocks are listed.
On every `cd` you get one line on stderr.

**Confirm:** the error names the file and the line:

```
envoke: /home/you/.config/envoke/envokerc.d/20-python: line 3: enter ~/Projects has no script body
```

**Fix:** the three causes that come up most — see [Block
syntax](configuration.md#block-syntax) and [Path
patterns](configuration.md#path-patterns) for the rules behind them:

- A block header with no indented body — including one whose body you meant to
  add later. That is an error, not an empty block.
- An unindented `#` inside what you thought was a body: it *ends* the block
  above it, so the lines after it are an indented body with no header. Indent
  comments with the rest of the script.
- An undefined `$VAR` in a pattern. envoke refuses the config rather than
  compiling a pattern that can never match, and names the variable.

One broken fragment never disables the others: the rest of the set still loads
and runs, and the failure is reported rather than swallowed. If the file is one
you didn't mean to have there at all — an editor backup, a `.bak` copy — see
the name rule in [§7](#7-a-fragment-isnt-being-loaded-at-all).

## 9. A symlinked project fragment points out of its project

**Symptom:** the fragment is approved — `envoke list` says `trusted` — and
`envoke debug` shows it loaded, and still nothing it declares ever fires. No
block of it is listed for the directory you expected, and no error is printed
anywhere.

A fragment that is a symlink out of your config directory into a project is
**confined** to that project's own tree: however its patterns are written, none
of its blocks can match a directory outside it (see [Bringing a project's own
config in](configuration.md#bringing-a-projects-own-config-in)). A `../`
pattern, or an absolute one naming somewhere else, compiles cleanly and then
matches nothing. This is the one case on this page where a perfectly good
pattern is not the problem.

**Confirm:** the bound is printed under that config's status line:

```
  config /home/you/.config/envoke/envokerc.d/api (trusted)
    symlink to /home/you/work/api/envoke.conf
    confined to /home/you/work/api -- its blocks cannot match outside that directory, whatever their patterns say
```

If the directory you are chasing is not that one or under it, this is your
answer. If it *is* that directory or under it, the bound is satisfied and
something else refused the block — check the case of the two paths first, which
is [§10](#10-the-directory-and-the-pattern-differ-in-case). `envoke allow`
states the same two facts, each marked `note:`, before its prompt — so the bound
is visible at approval time rather than only afterwards.

A third line — `its symlink could not be followed, so that bound is the link's
own directory` — means envoke could not resolve the link at all and confined
the fragment to where the link itself sits, which is almost never a directory
its patterns name. Fix the link.

**Fix**, depending on what the rules are for:

- **Rules about the project** — write them relative to `./`, which in that file
  means the project's own directory (see [Relative
  patterns](configuration.md#relative-patterns)): `enter ./src`, not
  `enter ~/work/api/src`. The file stays portable across checkouts besides.
- **Rules about anywhere else** — move them into a config that really lives in
  `envokerc.d`, or into your central config. Those are files only you can
  write, so they are not confined.

## 10. The directory and the pattern differ in case

**Symptom:** [§9](#9-a-symlinked-project-fragment-points-out-of-its-project)'s
symptom exactly — a symlinked project fragment, `trusted`, listed as loaded, no
block of it fires, nothing printed anywhere — except that the directory you are
chasing *is* inside the bound, so §9's answer doesn't apply.

Where the filesystem is case-insensitive, `~/work/API` and `~/work/api` are one
directory, and the confinement bound accepts either: it compares which directory
you are in, not how you spelled it. The **pattern** does not. A confined
fragment's `./` patterns carry the project's directory as a literal prefix, in
the one spelling envoke resolved the link to, and a pattern is matched as
written — so after `cd ~/work/API/src` the bound is satisfied and `enter ./src`
still does not match. Nothing in the output says which of the two refused.

macOS is where this comes up, its filesystem being case-insensitive by default.
Windows normalizes a resolved path to the on-disk spelling, so a confined
fragment there is spared it.

**Confirm:** compare the case of the paths `envoke debug` prints against each
other:

```
envoke debug: /Users/you/work/API -> /Users/you/work/API/src
  config /Users/you/.config/envoke/envokerc.d/api (trusted)
    symlink to /Users/you/work/api/envoke.conf
    confined to /Users/you/work/api -- its blocks cannot match outside that directory, whatever their patterns say
  no blocks would fire
```

`API` on the first line, `api` on the `confined to` line. The second is the
spelling the patterns were built from.

**Fix:** `cd` using the spelling the `confined to` line shows — the directory as
the symlink leads to it.

If a rule has to fire under either spelling, the pattern needs a `(?i)`, and a
`./`-relative one cannot take it: only a *leading* `./` resolves against the
config's own directory (see [Relative
patterns](configuration.md#relative-patterns)), so `(?i)./src` is an ordinary
regex that matches nothing you meant. Spell that one pattern out absolutely,
`enter (?i)/Users/you/work/api/src`, and accept that it no longer travels with
the checkout.

## 11. The fragment directory hit a bound

**Symptom:** no fragment loads — `envoke debug` lists your central config and
then the fragment *directory* itself as failed. Nearly always `$ENVOKERC_D`
pointing at something larger than a config directory.

**Confirm:** the error on stderr names what stopped the walk:

```
envoke: read /home/you/.config/envoke/envokerc.d: more than 512 config files; this directory is walked on every directory change, so it is bounded -- $ENVOKERC_D is probably pointing at something larger than a config directory
```

```
envoke: read /home/you/.envokerc.d: /home/you/.envokerc.d/a/b/c/d/e/f/g/h/i is more than 8 levels deep; this directory is walked on every directory change, so it is bounded -- flatten it, or point $ENVOKERC_D at the subdirectory you meant
```

A single file that is not a regular one stops the walk the same way, since every
fragment is opened before anything decides whether to trust it:

```
envoke: read /home/you/.envokerc.d: /home/you/.envokerc.d/scratch is a named pipe; a config fragment has to be a regular file, and every one of them is read on every directory change -- remove it, or point it at the config file you meant
```

And `$ENVOKERC_D` pointing at something that is not a directory at all is
refused before the walk starts, rather than being read as though it were the one
fragment:

```
envoke: /home/you/notes.txt is a regular file, not a directory of config fragments; point $ENVOKERC_D at a directory, or $ENVOKERC at a single config file
```

(The three errors above name the directory the walk was in — `read <dir>:` —
because the walk had already started. This one is refused before it starts, so
there is no directory to name.)

**Fix:** point `$ENVOKERC_D` at a directory that holds only configs, flatten the
nesting, or remove the file the error names. If you meant to load a single file,
that is `$ENVOKERC`, not `$ENVOKERC_D`. Every file in there is opened,
parsed and hashed on every directory change, which is why the walk is bounded at
all — see [The `envokerc.d`
directory](configuration.md#the-envokercd-directory).

This one is all-or-nothing: the walk stops at the bound and the whole directory
drops out of the set rather than being half-read, so you lose every fragment
until it is fixed. Your central config keeps working.

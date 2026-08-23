# Getting Started

## Installation

```sh
brew install neirda24/tap/envoke                          # Homebrew (macOS/Linux)
scoop bucket add neirda24 https://github.com/Neirda24/scoop-bucket && scoop install envoke  # Scoop (Windows)
go install github.com/Neirda24/envoke/cmd/envoke@latest   # Go toolchain
```

Or download a prebuilt binary, `.deb`, or `.rpm` from
[GitHub Releases](https://github.com/Neirda24/envoke/releases)
for macOS/Linux/Windows (amd64/arm64), built with [goreleaser](https://goreleaser.com/).
Each release's `checksums.txt` is signed keylessly with
[cosign](https://docs.sigstore.dev/cosign/) — see the release notes for the
exact verification command.

## Shell integration

Add the line for your shell to the rc file named above it, then restart the
shell (or re-source that file):

=== "bash"

    In `~/.bashrc`:

    ```sh
    eval "$(envoke shell-init bash)"
    ```

=== "zsh"

    In `~/.zshrc` — **not** `~/.zshenv`:

    ```sh
    eval "$(envoke shell-init zsh)"
    ```

=== "fish"

    In `~/.config/fish/config.fish`:

    ```fish
    envoke shell-init fish | source
    ```

=== "tcsh"

    In `~/.tcshrc` — **not** `~/.cshrc`:

    ```tcsh
    # `eval "$(...)"`-style substitution splits multi-line output on newlines
    # in tcsh, so this pipes into source instead.
    envoke shell-init tcsh | source /dev/stdin
    ```

=== "PowerShell"

    In `$PROFILE`:

    ```powershell
    # Out-String joins the (possibly multi-line) output into one string
    # before Invoke-Expression evaluates it.
    & envoke shell-init powershell | Out-String | Invoke-Expression
    ```

**The file matters.** `~/.zshenv`, `~/.cshrc` and fish's `config.fish` are read
by *non-interactive* shells too, so a hook in one of them is evaluated by every
`zsh -c` or `tcsh -c` that changes directory. The hook guards against that
itself — it installs only in an interactive shell, so nothing breaks either
way — but zsh and tcsh each have an interactive-only file, and that is where
the line belongs, which is what the two **not**s mark. fish has no such file
and relies on the guard.

`envoke shell-init` with no argument guesses the shell from `$SHELL`, which
is usually what you want when adding the line to your own rc file. It never
falls back to a default it isn't sure about: an unrecognised `$SHELL` is an
error telling you to name the shell, rather than a bash hook quietly written
into a fish config.

### Tab completion

Optional, and separate from the hook above. bash, zsh and fish only — tcsh and
PowerShell would need a half-working script to be worth shipping, so they get
an explicit error instead.

=== "bash"

    ```sh
    envoke completion bash >> ~/.bashrc
    # or, without touching the rc file:
    source <(envoke completion bash)
    ```

=== "zsh"

    ```sh
    # compinit must already be set up
    envoke completion zsh > "${fpath[1]}/_envoke"
    ```

=== "fish"

    ```fish
    envoke completion fish > ~/.config/fish/completions/envoke.fish
    ```

With no argument it guesses from `$SHELL`, like `shell-init`.

### A note on Windows

Windows binaries are published (and a Scoop manifest with them), and the
PowerShell hook is generated the same way as the others. A few things are
worth knowing before you rely on it:

- **Write patterns with `/`, not `\`.** Patterns are regexes, where `\` is
  the escape character, so `C:\Users\you` would not mean what it looks like.
  envoke normalizes the directories it tests to forward slashes, so
  `C:/Users/you/Projects/([^/]+)` is the form that works. `ENVOKE_DIR` is
  still handed to your script in native `C:\...` form; the `ENVOKE_MATCH*`
  capture variables come from the normalized path.
- **Matching is case-sensitive**, while Windows paths generally are not — the
  same mismatch macOS has, and not a Windows rule. See [Path
  patterns](configuration.md#path-patterns) for what to do about it.
- [`envoke exec`](non-interactive.md) runs blocks through `sh -c` and so
  needs a POSIX shell on `PATH` (Git Bash, WSL, MSYS2). The shell hook
  itself has no such requirement.
- **Give [`envoke debug`](debugging.md) and `envoke exec` the directory you
  came from.** Both work out `<to>` for themselves, but `<from>` defaults to
  `$OLDPWD`, a POSIX shell convention PowerShell has no counterpart to, so the
  no-argument form has nothing to infer and says so. One argument is `<from>`:
  `envoke debug C:\work\api`. Spell it out — PowerShell passes `~` to a native
  command literally, and envoke expands `~` in patterns only.

Windows is tested on a real `windows-latest` CI runner, not just
cross-compiled: the whole test suite runs there, the CLI itself included, and so
does the PowerShell hook, driven through a real PowerShell. The end-to-end tests
that drive bash/zsh/fish/tcsh still run on Linux and macOS only, since those
shells aren't what a Windows user is running anyway. Please
[open an issue](https://github.com/Neirda24/envoke/issues) if something
behaves differently than documented here.

## Checking your version

```sh
$ envoke version
envoke 0.1.1-SNAPSHOT-e4b0eb5 (commit e4b0eb554c40b05a566ae1e01a427dc08e12ac47, built 2026-07-16T13:28:55Z)
go1.26.4 darwin/arm64
```

Useful to have on hand for bug reports — it prints the real version, commit, and build date for a released binary, plus the Go toolchain and OS/arch it was built with. A binary built locally without goreleaser's release `ldflags` (e.g. a plain `go build`) prints `envoke dev (commit unknown, built unknown)` instead, since those values are only injected at release build time.

## Your first config

Create `~/.envokerc`:

```
enter ~/Projects/([^/]+)
    source "$ENVOKE_DIR/venv/bin/activate"

leave ~/Projects/([^/]+)
    deactivate
```

Then approve it — envoke never runs a new or edited config unconditionally. `envoke allow` shows you the blocks it's about to trust and asks for confirmation before recording anything:

```sh
$ envoke allow
envoke: about to trust /home/you/.envokerc -- review each block below before confirming:

  enter ~/Projects/([^/]+) (line 1)
    source "$ENVOKE_DIR/venv/bin/activate"

  leave ~/Projects/([^/]+) (line 4)
    deactivate

envoke: trust and run these blocks on every matching cd? [y/N] y
envoke: trusted /home/you/.envokerc
envoke: to apply it to this shell without leaving the directory: eval "$(envoke reload)"
```

`cd` into a matching directory and the `enter` block runs in your current shell; `cd` back out and `leave` runs.

To apply a config you just approved without moving, run the `eval` line above — `envoke allow` cannot export into the shell that ran it. See [Applying a config without leaving the directory](debugging.md#applying-a-config-without-leaving-the-directory), which also covers `envoke disable`/`enable` for switching envoke off. For non-interactive setups (dotfiles bootstrap scripts, provisioning), pass `--yes`/`-y` to skip the confirmation prompt: `envoke allow --yes`. See [Trust Model](trust.md) for the full confirm/diff/`--yes` behavior, and [`envoke debug`](debugging.md#envoke-debug) for inspecting matches before trusting a config.


### Splitting rules across files

One file gets crowded. `envokerc.d` holds one config per project or concern,
applied in relative path order — `10-` before `20-`:

```
~/.config/envoke/envokerc.d/
├── 10-work
└── 20-python
```

If a project's rules belong with the project, commit them there and symlink
the file in — that link is what brings it into the set, and patterns in it can
be written relative to the project:

```sh
ln -s ~/Projects/my-app/envoke.conf ~/.config/envoke/envokerc.d/my-app
```

```
# ~/Projects/my-app/envoke.conf, committed with the repository
enter .
    source "$ENVOKE_DIR/venv/bin/activate"

leave .
    deactivate
```

Each file is approved separately, and one `envoke allow` covers them all. See
[Configuration](configuration.md#the-envokercd-directory).

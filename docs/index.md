---
title: envoke
description: >-
  Run a shell script when you cd into or out of a directory, matched by path
  pattern. One static binary, five shells, nothing runs until you approve it.
---

# envoke

`envoke` runs a shell script automatically when you `cd` into — or out of — a
directory, matched by path pattern. One static binary, every major shell,
nothing runs until you approve it.

```sh
brew install neirda24/tap/envoke
echo 'eval "$(envoke shell-init zsh)"' >> ~/.zshrc
```

```
# ~/.envokerc
enter ~/Projects/([^/]+)
    source "$ENVOKE_DIR/venv/bin/activate"

leave ~/Projects/([^/]+)
    deactivate
```

```sh
envoke allow          # review the config once, approve it
cd ~/Projects/my-app  # the venv activates
```

## Start here

<div class="grid cards" markdown>

-   **New to envoke**

    ---

    Install it, hook your shell, write a first block and approve it.

    [Getting Started →](getting-started.md)

-   **Writing a config**

    ---

    Block syntax, path patterns, and the variables a matched script gets.

    [Configuration →](configuration.md)

-   **It didn't fire**

    ---

    Eleven causes, ordered by how often they actually come up.

    [Troubleshooting →](troubleshooting.md)

-   **Looking something up**

    ---

    Every command, flag, environment variable, file and exit code.

    [Reference →](reference.md)

</div>

## I want to…

| | |
|---|---|
| install it and write a first block | [Getting Started](getting-started.md) |
| learn the config syntax | [Block syntax](configuration.md#block-syntax) |
| write a pattern that matches what I mean | [Path patterns](configuration.md#path-patterns) |
| know what my script can see | [What a matched script sees](configuration.md#what-a-matched-script-sees) |
| copy a working example | [Recipes](recipes.md) |
| **find out why my block didn't run** | [Troubleshooting](troubleshooting.md) |
| **stop it firing, right now** | [Turning envoke off](debugging.md#turning-envoke-off) |
| see what would fire, without running it | [`envoke debug`](debugging.md) |
| apply a config without leaving the directory | [`envoke reload`](debugging.md#applying-a-config-without-leaving-the-directory) |
| understand the approval step | [Trust Model](trust.md) |
| keep rules with the repository they belong to | [Bringing a project's own config in](configuration.md#bringing-a-projects-own-config-in) |
| split one big config into several | [The `envokerc.d` directory](configuration.md#the-envokercd-directory) |
| run blocks from a script, a Makefile or CI | [Non-interactive Use](non-interactive.md) |
| look up a command, flag, variable or exit code | [Reference](reference.md) |
| compare it with direnv | [envoke vs. direnv](vs-direnv.md) |
| remove it | [Uninstalling](uninstall.md) |

!!! warning "Status: early development"

    Everything documented here exists and is tested end to end against real
    interpreters — a `cd` into a trusted, matching directory really does run
    its `enter` block in your shell today. That covers:

    - the **matching engine** — patterns, intermediate directories, ordering;
    - **shell hooks** for bash, zsh, fish, tcsh and PowerShell;
    - the **`envokerc.d`** fragment directory, with `./`-relative patterns and
      symlinked project configs;
    - the **trust mechanism** — `allow`, `revoke`, `list`, `prune`;
    - the **off switch** — `disable`/`enable` and `ENVOKE_DISABLE`;
    - **`reload`**, non-interactive **`exec`**, and **`debug`** diagnostics;
    - **packaging** — GitHub Releases, a Homebrew tap, a Scoop bucket and
      `.deb`/`.rpm` packages, each release carrying a per-archive SBOM
      alongside cosign-signed checksums.

    What is early is the mileage, not the feature list. There is no roadmap
    section here on purpose: if `envoke help` doesn't list it, it doesn't
    exist. The [Reference](reference.md) is the complete inventory of
    commands, flags, variables, files and exit codes.

## How it compares

`envoke` is a spiritual rewrite of
[ondir](https://github.com/alecthomas/ondir) in Go — the same
`enter`/`leave`-by-path-pattern model, reaching every major shell and OS from
a single static binary. ondir is feature-complete by its own maintainer's
account and still works, but it has had no release in years.

- [envoke vs. direnv](vs-direnv.md) — path patterns covering whole trees
  instead of an `.envrc` per directory, arbitrary shell scripts instead of
  environment variables only, and when to use which (or both).
- [Design Notes](design-notes.md) — the point-by-point list of where envoke
  departs from ondir, and the principles that hold across the codebase.

## License

[MIT](https://github.com/Neirda24/envoke/blob/main/LICENSE)

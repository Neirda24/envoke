# Uninstalling

Uninstalling has two parts: removing the shell hook (so `envoke` stops
running on every `cd`) and removing the binary itself. Do both — removing
only the binary leaves a hook line in your rc file that fails on every
prompt/`cd` once `envoke` is gone.

## 1. Remove the shell hook

Delete the line you added in [Getting Started](getting-started.md#shell-integration)
from your shell's rc file, then restart your shell (or re-source the rc
file):

```sh
# bash: ~/.bashrc   zsh: ~/.zshrc
eval "$(envoke shell-init bash)"   # or zsh — delete this line
```

```fish
# fish: ~/.config/fish/config.fish
envoke shell-init fish | source    # delete this line
```

```tcsh
# tcsh: ~/.tcshrc
envoke shell-init tcsh | source /dev/stdin   # delete this line
```

```powershell
# PowerShell: $PROFILE
& envoke shell-init powershell | Out-String | Invoke-Expression   # delete this line
```

## 2. (Optional) Remove trust records and config

These are small and harmless to leave behind, but if you want a clean
removal:

```sh
# Trust records (SHA-256 hashes + approved content per config path)
rm -rf "${XDG_DATA_HOME:-$HOME/.local/share}/envoke"

# Your config file, if you don't want to keep it
rm ~/.envokerc
# or, if you used the XDG path instead:
rm "${XDG_CONFIG_HOME:-$HOME/.config}/envoke/config"
```

Leaving the config or trust records in place is safe even after the binary
is gone — nothing reads them without `envoke` itself.

## 3. Remove the binary

Pick the method matching how you installed it (see
[Getting Started](getting-started.md#installation)):

```sh
# Homebrew (macOS/Linux)
brew uninstall --cask envoke
# and, if you no longer want the tap itself:
brew untap neirda24/tap

# Scoop (Windows)
scoop uninstall envoke
# and, if you no longer want the bucket itself:
scoop bucket rm neirda24

# Go toolchain
rm "$(go env GOPATH)/bin/envoke"

# Debian/Ubuntu (.deb)
sudo dpkg -r envoke

# Fedora/RHEL (.rpm)
sudo rpm -e envoke

# Manual download / go build
# Remove the binary from wherever you placed it on PATH, e.g.:
rm /usr/local/bin/envoke
```

// Package shellinit generates the shell hook code printed by
// `envoke shell-init <shell>`, meant to be eval'd/sourced from the user's
// shell rc file (e.g. `eval "$(envoke shell-init bash)"`).
package shellinit

import "fmt"

// Generate returns the hook script for shell, or an error if shell isn't
// supported yet.
func Generate(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashHook, nil
	case "zsh":
		return zshHook, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (bash and zsh only for now; fish/tcsh/powershell land in a later MVP step)", shell)
	}
}

// bashHook polls PWD from PROMPT_COMMAND, since bash has no native "on cd"
// hook. This only appends to PROMPT_COMMAND — it never redefines cd (see
// CLAUDE.md's design principles: ondir's zsh integration overriding cd
// directly was a bug, not a pattern to repeat).
//
// `envoke shell-hook` currently never prints anything to stdout (no config
// trust mechanism exists yet — see internal/config.Locate and cmd/envoke),
// so the eval below is a safe no-op until that lands.
const bashHook = `_envoke_hook() {
  local envoke_prev="${__envoke_prev_pwd:-$PWD}"
  if [ "$envoke_prev" != "$PWD" ]; then
    eval "$(command envoke shell-hook "$envoke_prev" "$PWD")"
  fi
  __envoke_prev_pwd="$PWD"
}
# Seed the baseline at install time (not lazily on the first hook call) so
# the first real cd is compared against the shell's actual starting
# directory instead of against itself.
: "${__envoke_prev_pwd:=$PWD}"
case ";${PROMPT_COMMAND-};" in
  *";_envoke_hook;"*) ;;
  *) PROMPT_COMMAND="_envoke_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac
`

// zshHook hooks directory changes via zsh's native chpwd_functions array,
// which fires on every directory change without needing to redefine cd.
const zshHook = `_envoke_hook() {
  eval "$(command envoke shell-hook "${OLDPWD:-$PWD}" "$PWD")"
}
typeset -ag chpwd_functions
if [[ -z "${chpwd_functions[(r)_envoke_hook]}" ]]; then
  chpwd_functions+=(_envoke_hook)
fi
`

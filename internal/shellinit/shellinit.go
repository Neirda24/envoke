// Package shellinit generates the shell hook code printed by
// `envoke shell-init <shell>`, meant to be eval'd/sourced from the user's
// shell rc file (e.g. `eval "$(envoke shell-init bash)"`).
package shellinit

import "fmt"

// Generate returns the hook script for shell, or an error if shell isn't
// supported.
func Generate(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashHook, nil
	case "zsh":
		return zshHook, nil
	case "fish":
		return fishHook, nil
	case "tcsh":
		return tcshHook, nil
	case "powershell":
		return powershellHook, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: bash, zsh, fish, tcsh, powershell)", shell)
	}
}

// bashHook polls PWD from PROMPT_COMMAND, since bash has no native "on cd"
// hook. This only appends to PROMPT_COMMAND — it never redefines cd (see
// CLAUDE.md's design principles: ondir's zsh integration overriding cd
// directly was a bug, not a pattern to repeat).
//
// envoke shell-hook prints nothing to stdout unless the matched config is
// trusted (see cmd/envoke's cmdAllow/cmdShellHook), so the eval below is
// always a safe no-op against an untrusted or non-matching config.
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

// fishHook hooks directory changes via fish's --on-variable PWD event,
// which fires on every directory change without needing to redefine cd.
// Fish's own $OLDPWD support is inconsistent across versions, so — like the
// bash hook above — this tracks the previous directory itself and seeds it
// once at install time rather than lazily inside the handler, for the same
// reason: the first real cd must be compared against the shell's actual
// starting directory, not against itself.
//
// `string collect` is required (fish 3.4+) to gather envoke shell-hook's
// possibly-multi-line stdout into a single string before eval, since a bare
// command substitution in fish splits output into one list element per
// line — passed straight to eval, that would silently turn a multi-line
// script into several unrelated single-line evals.
const fishHook = `function _envoke_hook --on-variable PWD
  set -l script (command envoke shell-hook --shell fish "$__envoke_prev_pwd" "$PWD" | string collect)
  if test -n "$script"
    eval $script
  end
  set -g __envoke_prev_pwd "$PWD"
end
if not set -q __envoke_prev_pwd
  set -g __envoke_prev_pwd "$PWD"
end
`

// tcshHook hooks directory changes via tcsh's special cwdcmd alias, which
// tcsh runs automatically after every cd/pushd/popd — csh's native
// equivalent of zsh's chpwd_functions, and, like it, needs no cd override
// or manual baseline seeding: tcsh already maintains $owd/$cwd itself.
//
// Two tcsh quirks, both confirmed against a real tcsh (they cost real
// debugging time — don't undo either without re-testing end to end):
//
//  1. tcsh has no export/`VAR=value` syntax and no `$(...)`/quoted-backquote
//     construct that preserves a multi-line command substitution as one
//     string (plain and quoted backquote substitution both split on
//     newlines), so the matched blocks' text is piped into
//     `source /dev/stdin` rather than captured and eval'd — source reads
//     and runs it a line at a time, exactly as if it were a sourced file.
//  2. cwdcmd's body runs through a restricted internal execution path that
//     does *not* honor pipe/redirect syntax directly: a bare
//     `cmd | source /dev/stdin` inside the alias silently prints to the
//     terminal instead of piping, and any setenv it does run happens in a
//     context that never reaches the interactive shell (both verified with
//     `setenv FOO bar` visibly failing to persist afterward). Wrapping the
//     whole pipeline in `eval "..."` forces a fresh, full parse that does
//     honor `|`, fixing both. $owd/$cwd are single-quoted *within* that
//     eval string (not just around the whole alias body) so a directory
//     name containing a space still reaches shell-hook as one argument once
//     eval re-tokenizes the string.
//  3. tcsh has no `command` builtin (unlike bash/zsh/fish, which use it to
//     bypass a same-named user alias/function) — running one literally
//     fails with "command: Command not found." tcsh's own way to bypass
//     alias expansion for a word is a leading backslash, so this invokes
//     `\envoke` rather than `command envoke`. Caught only by driving a real
//     tcsh with a same-named alias defined ahead of the hook, not by
//     string-matching the generated script.
//
// A plain (non-merged) pipe keeps stderr going straight to the terminal, so
// an untrusted-config warning is never fed into source.
//
// cwdcmd is tcsh's only directory-change hook slot: if a .tcshrc already
// aliases cwdcmd for something else (a documented tcsh idiom, e.g. setting
// the xterm title), this claims that slot rather than chaining with it —
// fold any existing cwdcmd body into _envoke_hook by hand in that case.
const tcshHook = `alias _envoke_hook 'eval "\envoke shell-hook --shell tcsh '"'"'$owd'"'"' '"'"'$cwd'"'"' | source /dev/stdin"'
alias cwdcmd _envoke_hook
`

// powershellHook hooks directory changes by wrapping the prompt function —
// PowerShell's idiomatic customization point (the same one posh-git/
// oh-my-posh use), redrawn before every prompt, not a cd override. The
// previous prompt is saved and always called through to, so this composes
// with prompt customization installed before it rather than replacing it.
// $_envokeHookInstalled guards against wrapping the prompt more than once
// if this script is sourced again (matching the bash/zsh hooks' own
// re-source guards).
//
// `& envoke shell-hook ... | Out-String` joins the (possibly multi-line)
// captured stdout into a single string before Invoke-Expression, the same
// concern as fish's `string collect` above: PowerShell's pipeline otherwise
// hands Invoke-Expression one line at a time.
const powershellHook = `if (-not $global:_envokeHookInstalled) {
  $global:_envokeHookInstalled = $true
  $global:_envokePrevPwd = (Get-Location).Path
  $global:_envokeOriginalPrompt = $function:prompt
  function global:prompt {
    $envokeCurPwd = (Get-Location).Path
    if ($global:_envokePrevPwd -ne $envokeCurPwd) {
      $envokeScript = & envoke shell-hook --shell powershell $global:_envokePrevPwd $envokeCurPwd | Out-String
      if ($envokeScript) { Invoke-Expression $envokeScript }
      $global:_envokePrevPwd = $envokeCurPwd
    }
    & $global:_envokeOriginalPrompt
  }
}
`

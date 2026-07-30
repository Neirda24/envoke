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
//
// The hook saves and restores $? around its own work. bash sets $? to the
// last command's status before running PROMPT_COMMAND, and the extremely
// common `PROMPT_COMMAND='__status=$?; ...'` idiom (git-prompt, liquidprompt
// and most hand-rolled prompts that colour on failure) reads it there.
// Since this hook prepends itself to PROMPT_COMMAND, without the
// save/restore every such prompt would silently start reporting envoke's
// exit status instead of the user's last command — a very confusing
// regression to trace back to a directory-hook tool.
const bashHook = `_envoke_hook() {
  local __envoke_status=$?
  local envoke_prev="${__envoke_prev_pwd:-$PWD}"
  if [ "$envoke_prev" != "$PWD" ]; then
    eval "$(command envoke shell-hook -- "$envoke_prev" "$PWD")"
  fi
  __envoke_prev_pwd="$PWD"
  return $__envoke_status
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
//
// The status save/restore serves the mirror image of bash's concern (see
// bashHook): chpwd_functions run as part of the `cd` itself, so a hook that
// returned the status of whatever it last did would make `cd foo && ...`
// stop short whenever envoke or the block it ran failed.
const zshHook = `_envoke_hook() {
  local __envoke_status=$?
  eval "$(command envoke shell-hook -- "${OLDPWD:-$PWD}" "$PWD")"
  return $__envoke_status
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
// The `set -l __envoke_status $status` first line and the matching `return`
// keep the handler transparent to $status, for the same reason as the
// bash/zsh hooks above.
const fishHook = `function _envoke_hook --on-variable PWD
  set -l __envoke_status $status
  set -l script (command envoke shell-hook --shell fish -- "$__envoke_prev_pwd" "$PWD" | string collect)
  if test -n "$script"
    eval $script
  end
  set -g __envoke_prev_pwd "$PWD"
  return $__envoke_status
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
// Four tcsh quirks, all confirmed against a real tcsh (they cost real
// debugging time — don't undo any of them without re-testing end to end):
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
//     honor `|`, fixing both.
//  3. That mandatory `eval` is also why the directories are passed through
//     the *environment* (ENVOKE_FROM/ENVOKE_TO, read by `envoke shell-hook`
//     when it gets no positional arguments) instead of being interpolated
//     into the eval string as arguments. An earlier version embedded
//     '$owd'/'$cwd' inside the eval string; because eval re-parses its
//     argument, a directory whose name contained a single quote closed
//     those quotes and the rest of the name was executed as shell code —
//     `cd "a';touch /tmp/pwned;echo 'b"` was enough, with no config and no
//     `envoke allow`, so it bypassed the trust model entirely. Keeping the
//     eval string a compile-time constant makes that class of bug
//     structurally impossible: no user-controlled text ever reaches the
//     re-parse. `setenv X "$owd"` is safe by contrast — csh does not
//     re-tokenize the result of a variable substitution inside double
//     quotes. The two variables are unsetenv'd right after so they don't
//     leak into unrelated child processes.
//  4. tcsh has no `command` builtin (unlike bash/zsh/fish, which use it to
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
const tcshHook = `alias _envoke_hook 'setenv ENVOKE_FROM "$owd" ; setenv ENVOKE_TO "$cwd" ; eval "\envoke shell-hook --shell tcsh | source /dev/stdin" ; unsetenv ENVOKE_FROM ; unsetenv ENVOKE_TO'
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
//
// $LASTEXITCODE is saved on entry and restored before calling through to
// the previous prompt, the same transparency concern as the other four
// hooks: invoking `envoke` is a native command, so it overwrites
// $LASTEXITCODE, and a prompt that colours on the last command's exit code
// would report envoke's instead. Restoring it is the same thing
// starship/oh-my-posh do. `$?` cannot be restored — PowerShell makes it
// read-only — so a prompt reading `$?` rather than $LASTEXITCODE still sees
// this hook's own result; that is a PowerShell limitation, not something
// the hook can work around.
const powershellHook = `if (-not $global:_envokeHookInstalled) {
  $global:_envokeHookInstalled = $true
  $global:_envokePrevPwd = (Get-Location).Path
  $global:_envokeOriginalPrompt = $function:prompt
  function global:prompt {
    $envokeLastExitCode = $global:LASTEXITCODE
    $envokeCurPwd = (Get-Location).Path
    if ($global:_envokePrevPwd -ne $envokeCurPwd) {
      $envokeScript = & envoke shell-hook --shell powershell -- $global:_envokePrevPwd $envokeCurPwd | Out-String
      if ($envokeScript) { Invoke-Expression $envokeScript }
      $global:_envokePrevPwd = $envokeCurPwd
    }
    $global:LASTEXITCODE = $envokeLastExitCode
    & $global:_envokeOriginalPrompt
  }
}
`

// Completion returns the tab-completion script for shell, or an error if
// completion isn't available for it.
//
// This is generated by the binary for the same reason the hooks are (see
// CLAUDE.md's one-binary-generates-all-shell-integration principle): a
// completion script hand-maintained per shell drifts from the actual
// subcommand list the moment anyone adds one, and it drifts silently.
//
// Only bash, zsh and fish are covered. tcsh's completion syntax can't
// express per-subcommand argument types without a great deal of care, and
// PowerShell's Register-ArgumentCompleter is a different model again;
// shipping a half-working completion for either is worse than shipping none
// and saying so.
func Completion(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletion, nil
	case "zsh":
		return zshCompletion, nil
	case "fish":
		return fishCompletion, nil
	case "tcsh", "powershell":
		return "", fmt.Errorf("completion is not available for %q (supported: bash, zsh, fish)", shell)
	default:
		return "", fmt.Errorf("unsupported shell %q (completion supported for: bash, zsh, fish)", shell)
	}
}

// subcommands is the completion candidate list, kept next to the completion
// scripts that embed it so adding a subcommand to cmd/envoke without
// updating this is caught by TestCompletion_CoversEverySubcommand rather
// than by a user noticing tab does nothing.
var subcommands = []string{
	"allow", "completion", "debug", "disable", "enable", "exec", "help",
	"list", "prune", "reload", "revoke", "shell-hook", "shell-init",
	"version",
}

// bashCompletion completes subcommands first, then argument types per
// subcommand: shell names for shell-init, files for the path-taking trust
// commands, directories for the two that take a transition.
// _envoke_compgen exists because `mapfile` is bash 4.0+ and macOS still
// ships bash 3.2 as /bin/bash -- a completion that silently produced no
// candidates there would be a miserable thing to debug. The read loop is
// the portable equivalent, and unlike the other common workaround
// (COMPREPLY=( $(compgen ...) )) it does not rely on unquoted word
// splitting, so candidates containing spaces survive.
const bashCompletion = `_envoke_compgen() {
  COMPREPLY=()
  local _envoke_line
  while IFS= read -r _envoke_line; do
    COMPREPLY+=("$_envoke_line")
  done < <(compgen "$@")
}
_envoke_complete() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "${COMP_CWORD}" -eq 1 ]; then
    _envoke_compgen -W "allow completion debug disable enable exec help list prune reload revoke shell-hook shell-init version" -- "$cur"
    return
  fi
  case "${COMP_WORDS[1]}" in
    shell-init)
      _envoke_compgen -W "bash zsh fish tcsh powershell" -- "$cur"
      ;;
    completion)
      _envoke_compgen -W "bash zsh fish" -- "$cur"
      ;;
    allow|revoke)
      _envoke_compgen -f -- "$cur"
      ;;
    exec|debug)
      _envoke_compgen -d -- "$cur"
      ;;
    *)
      COMPREPLY=()
      ;;
  esac
}
complete -F _envoke_complete envoke
`

// zshCompletion uses compdef, so it needs compinit to have run — that's the
// standard expectation for a zsh completion and is what `autoload -Uz
// compinit && compinit` in a .zshrc provides.
const zshCompletion = `_envoke() {
  local -a _envoke_cmds
  _envoke_cmds=(
    'allow:trust a config file after reviewing it'
    'completion:print a tab-completion script'
    'debug:show which blocks would fire, without running them'
    'disable:stop running blocks in every shell until enable'
    'enable:undo disable'
    'exec:run matching blocks in subprocesses (scripts/CI)'
    'help:show usage'
    'list:list every trusted config'
    'prune:drop trust records whose config no longer exists'
    'reload:re-apply the enter blocks for the current directory'
    'revoke:withdraw trust for a config'
    'shell-hook:internal, called by the generated hook'
    'shell-init:print shell hook code to eval/source'
    'version:print version information'
  )
  if (( CURRENT == 2 )); then
    _describe -t commands 'envoke command' _envoke_cmds
    return
  fi
  case "$words[2]" in
    shell-init)
      _values 'shell' bash zsh fish tcsh powershell
      ;;
    completion)
      _values 'shell' bash zsh fish
      ;;
    allow|revoke)
      _files
      ;;
    exec|debug)
      _files -/
      ;;
  esac
}
compdef _envoke envoke
`

// fishCompletion relies on fish's own __fish_use_subcommand /
// __fish_seen_subcommand_from helpers rather than reimplementing that state
// tracking. `complete -c envoke -f` first disables the default filename
// completion, which the per-subcommand rules then re-enable with -F only
// where a path is actually expected.
const fishCompletion = `complete -c envoke -f
complete -c envoke -n __fish_use_subcommand -a allow -d 'trust a config file after reviewing it'
complete -c envoke -n __fish_use_subcommand -a completion -d 'print a tab-completion script'
complete -c envoke -n __fish_use_subcommand -a debug -d 'show which blocks would fire, without running them'
complete -c envoke -n __fish_use_subcommand -a disable -d 'stop running blocks in every shell until enable'
complete -c envoke -n __fish_use_subcommand -a enable -d 'undo disable'
complete -c envoke -n __fish_use_subcommand -a exec -d 'run matching blocks in subprocesses (scripts/CI)'
complete -c envoke -n __fish_use_subcommand -a help -d 'show usage'
complete -c envoke -n __fish_use_subcommand -a list -d 'list every trusted config'
complete -c envoke -n __fish_use_subcommand -a prune -d 'drop trust records whose config no longer exists'
complete -c envoke -n __fish_use_subcommand -a reload -d 're-apply the enter blocks for the current directory'
complete -c envoke -n __fish_use_subcommand -a revoke -d 'withdraw trust for a config'
complete -c envoke -n __fish_use_subcommand -a shell-hook -d 'internal, called by the generated hook'
complete -c envoke -n __fish_use_subcommand -a shell-init -d 'print shell hook code to eval/source'
complete -c envoke -n __fish_use_subcommand -a version -d 'print version information'
complete -c envoke -n '__fish_seen_subcommand_from shell-init' -a 'bash zsh fish tcsh powershell'
complete -c envoke -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
complete -c envoke -n '__fish_seen_subcommand_from allow revoke' -F
complete -c envoke -n '__fish_seen_subcommand_from exec debug' -F
complete -c envoke -n '__fish_seen_subcommand_from allow' -l yes -s y -d 'skip the y/N confirmation prompt'
complete -c envoke -n '__fish_seen_subcommand_from reload' -l shell -x -a 'bash zsh fish tcsh powershell' -d 'shell dialect to render for'
`

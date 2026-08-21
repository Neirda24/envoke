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
// hook. It appends rather than redefining cd.
//
// Nothing is installed in a non-interactive shell. envoke exists to react to
// a person moving around; a script that cds has not asked for that, and the
// rc file a hook lives in is not always read only by interactive shells —
// `.cshrc` and `.zshenv` are read by every shell, and bash reads `.bashrc`
// for a shell named by `BASH_ENV` and for one started by a remote shell
// daemon (`ssh host 'cmd'`). Guarding at install time rather than at fire
// time means such a shell ends up with no hook at all, which is both cheaper
// and easier to reason about. `envoke exec` is the deliberate,
// non-interactive entry point and is unaffected.
//
// The guard wraps the installation instead of returning early because this
// text is evaluated inside the caller's rc file, so aborting it aborts the rc
// file: `return` inside `eval` inside a sourced file pops the sourced file's
// own frame, skipping every later line of the user's `.bashrc` with status 0,
// and in a script that is executed rather than sourced `exit` ends the script
// outright.
//
// The baseline is seeded at install time, not lazily on the first call: a
// hook that seeds itself compares the first cd's destination against itself
// and misses it.
//
// $? is saved and restored because this hook prepends itself to
// PROMPT_COMMAND, where the common `PROMPT_COMMAND='__status=$?; ...'`
// idiom reads it. Without that, every exit-code-colouring prompt would
// start reporting envoke's status instead of the user's last command.
const bashHook = `case $- in *i*)
_envoke_hook() {
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
;; esac
`

// zshHook uses zsh's native chpwd_functions array. No baseline to seed —
// zsh maintains $OLDPWD itself.
//
// Guarded on interactivity like bash's (see bashHook), and zsh is the shell
// where it matters most after tcsh: `.zshrc` is interactive-only, but
// `.zshenv` is read by every zsh including `zsh -c`, and nothing stops a user
// putting the hook there.
//
// The status save/restore is the mirror of bash's concern (see bashHook):
// chpwd_functions run as part of the cd, so a hook returning its own status
// would make `cd foo && ...` stop short whenever a block failed.
const zshHook = `if [[ -o interactive ]]; then
_envoke_hook() {
  local __envoke_status=$?
  eval "$(command envoke shell-hook -- "${OLDPWD:-$PWD}" "$PWD")"
  return $__envoke_status
}
typeset -ag chpwd_functions
if [[ -z "${chpwd_functions[(r)_envoke_hook]}" ]]; then
  chpwd_functions+=(_envoke_hook)
fi
fi
`

// fishHook uses fish's --on-variable PWD event. Fish's $OLDPWD is
// inconsistent across versions, so this tracks the previous directory
// itself and seeds it at install time, like bash.
//
// The interactivity guard matters here: fish reads config.fish for
// non-interactive shells too, so without it every `fish -c` that cds would
// run enter/leave blocks.
//
// `string collect` (fish 3.4+) is required: a bare command substitution
// splits output into one list element per line, which would turn a
// multi-line script into several unrelated single-line evals.
//
// $status is saved and returned, as in bash and zsh.
const fishHook = `if status is-interactive
function _envoke_hook --on-variable PWD
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
end
`

// tcshHook uses tcsh's cwdcmd alias, csh's equivalent of chpwd_functions.
// tcsh maintains $owd/$cwd itself, so there is no baseline to seed.
//
// `$?prompt` is csh's interactivity test, and this is the shell the guard was
// written for: `.cshrc` is read by non-interactive tcsh — verified — so a
// hook installed there fires for every `tcsh -c` that changes directory.
//
// Four tcsh quirks, each verified against a real tcsh — don't undo one
// without re-testing end to end:
//
//  1. tcsh has no export syntax, and neither plain nor quoted backquote
//     substitution preserves a multi-line result as one string, so the
//     block text is piped into `source /dev/stdin` rather than captured
//     and eval'd.
//  2. cwdcmd's body runs through a restricted path that does not honor `|`
//     directly: the pipeline silently prints to the terminal instead, and
//     any setenv it runs never reaches the interactive shell. Wrapping it
//     in `eval "..."` forces a full re-parse that does honor it.
//  3. That mandatory eval is why the directories travel through the
//     environment instead of being interpolated into the eval string.
//     Because eval re-parses its argument, a directory name containing a
//     single quote closed the quotes and ran the rest as shell code —
//     `cd "a';touch /tmp/pwned;echo 'b"` sufficed, with no config and no
//     `envoke allow`, bypassing the trust model entirely. Keeping the eval
//     string a compile-time constant makes that class of bug
//     unexpressible: no user-controlled text reaches the re-parse.
//     `setenv X "$owd"` is safe by contrast — csh does not re-tokenize a
//     variable substitution inside double quotes.
//  4. tcsh has no `command` builtin; a leading backslash is its way to
//     bypass alias expansion, hence `\envoke`.
//
// The pipe is deliberately not merged, so an untrusted-config warning on
// stderr never gets fed into source.
//
// cwdcmd is tcsh's only directory-change slot. A .tcshrc that already
// aliases it (setting the xterm title, say) loses that alias — fold the
// existing body into _envoke_hook by hand.
const tcshHook = `if ($?prompt) then
alias _envoke_hook 'setenv ENVOKE_FROM "$owd" ; setenv ENVOKE_TO "$cwd" ; eval "\envoke shell-hook --shell tcsh | source /dev/stdin" ; unsetenv ENVOKE_FROM ; unsetenv ENVOKE_TO'
alias cwdcmd _envoke_hook
endif
`

// powershellHook wraps the prompt function, PowerShell's idiomatic
// customization point. It is the one hook with no interactivity guard, and
// deliberately: `prompt` is only ever called by an interactive host, so the
// hook point is already the guard. Adding a check would be a check on
// something else — [Environment]::UserInteractive is true for any process
// with a desktop — and would read as protection it isn't. The previous prompt is saved and always called
// through to, so this composes with anything installed before it;
// $_envokeHookInstalled prevents double-wrapping on a re-source.
//
// Out-String joins the possibly-multi-line stdout before
// Invoke-Expression — the same concern as fish's `string collect`.
//
// $LASTEXITCODE is saved and restored because invoking envoke is a native
// command and overwrites it. `$?` cannot be: PowerShell makes it read-only,
// so a prompt reading `$?` rather than $LASTEXITCODE still sees this hook's
// own result.
//
// A PowerShell location is not necessarily a filesystem path: providers
// expose drives such as HKLM:, Cert:, Env:, Function: and Variable:, and
// `Set-Location HKLM:` makes the current location `HKLM:\SOFTWARE`. That is
// not an absolute path, so `envoke shell-hook` rejects it and prints the
// rejection on stderr — from inside `prompt`, interleaved with the user's
// prompt, twice per round trip. Hence the FileSystem test, which is also
// cheaper than the process it avoids.
//
// The provider decides *whether* to fire; ProviderPath decides *what path* to
// send, and those are separate questions. Under the FileSystem provider
// .Path is still the PowerShell spelling of the location: drive-qualified for
// a user-created PSDrive (`Repos:\proj`, for `New-PSDrive -Name Repos -Root
// C:\src`) and provider-qualified for a UNC location
// (`Microsoft.PowerShell.Core\FileSystem::\\host\share`) — neither absolute,
// and a one-letter PSDrive name is worse than an error, being absolute but
// pointing somewhere that does not exist. .ProviderPath is the filesystem
// path itself with the drive mapping resolved, which is what a config's
// patterns are written against.
//
// $_envokePrevPwd is deliberately left alone while the location is off the
// filesystem, rather than updated: coming back to a filesystem path then
// reports the transition from the last filesystem directory — the one whose
// leave blocks are owed — instead of passing a provider path as `from`. For
// the same reason the install-time seed only takes a filesystem location,
// and an unseeded (empty) value suppresses the call rather than being sent
// as `from`.
const powershellHook = `if (-not $global:_envokeHookInstalled) {
  $global:_envokeHookInstalled = $true
  $global:_envokePrevPwd = $null
  if ((Get-Location).Provider.Name -eq 'FileSystem') { $global:_envokePrevPwd = (Get-Location).ProviderPath }
  $global:_envokeOriginalPrompt = $function:prompt
  function global:prompt {
    $envokeLastExitCode = $global:LASTEXITCODE
    $envokeLoc = Get-Location
    if ($envokeLoc.Provider.Name -eq 'FileSystem') {
      $envokeCurPwd = $envokeLoc.ProviderPath
      if ($global:_envokePrevPwd -and $global:_envokePrevPwd -ne $envokeCurPwd) {
        $envokeScript = & envoke shell-hook --shell powershell -- $global:_envokePrevPwd $envokeCurPwd | Out-String
        if ($envokeScript) { Invoke-Expression $envokeScript }
      }
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
// bash, zsh and fish only. tcsh's completion syntax can't express
// per-subcommand argument types without a great deal of care and
// PowerShell's Register-ArgumentCompleter is a different model again;
// shipping half a completion for either is worse than shipping none and
// saying so.
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

// subcommands is the checklist that keeps the three completion scripts
// complete. No generated script reads it: each hardcodes its own list in its own
// syntax (bash's -W word list, zsh's _envoke_cmds, one fish `complete -a` line
// per command), and TestCompletion_ListsEverySubcommand asserts every name here
// appears in all three. Adding a name here alone completes the command in no
// shell.
var subcommands = []string{
	"allow", "completion", "debug", "disable", "enable", "exec", "help",
	"list", "prune", "reload", "revoke", "shell-hook", "shell-init",
	"version",
}

// bashCompletion completes subcommands first, then argument types per
// subcommand.
//
// _envoke_compgen exists because `mapfile` is bash 4.0+ and macOS still
// ships bash 3.2 as /bin/bash, where it silently produces no candidates.
// Unlike the other common workaround, COMPREPLY=( $(compgen ...) ), the
// read loop doesn't rely on unquoted word splitting, so candidates
// containing spaces survive.
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
    'list:reconcile the configs envoke would load with the trust store'
    'prune:drop trust records whose config no longer exists'
    'reload:re-apply the enter blocks for the current directory'
    'revoke:withdraw trust for a config, or the whole set'
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
complete -c envoke -n __fish_use_subcommand -a list -d 'reconcile the configs envoke would load with the trust store'
complete -c envoke -n __fish_use_subcommand -a prune -d 'drop trust records whose config no longer exists'
complete -c envoke -n __fish_use_subcommand -a reload -d 're-apply the enter blocks for the current directory'
complete -c envoke -n __fish_use_subcommand -a revoke -d 'withdraw trust for a config, or the whole set'
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

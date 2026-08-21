// Command envoke runs shell scripts when you cd into or out of a directory.
//
// Nothing here executes a block from a config that hasn't been through
// `envoke allow` since its last edit. The two commands that can run anything,
// shell-hook and exec, each check trust first (exec by delegating to
// internal/envoke, which enforces it internally so no caller can skip it).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/configset"
	"github.com/Neirda24/envoke/internal/envoke"
	"github.com/Neirda24/envoke/internal/executor"
	"github.com/Neirda24/envoke/internal/matcher"
	"github.com/Neirda24/envoke/internal/shellinit"
	"github.com/Neirda24/envoke/internal/state"
	"github.com/Neirda24/envoke/internal/trust"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

// commit is overridden at build time via -ldflags "-X main.commit=...".
var commit = "unknown"

// date is overridden at build time via -ldflags "-X main.date=...".
var date = "unknown"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
}

// fprintf, fprintln and fprint wrap the fmt equivalents, drop the error, and
// escape every string or error argument for the terminal (see sanitize).
//
// The split that holds is format string vs argument, not trusted vs untrusted
// call site: a format string is a Go literal in this file, and every argument
// is either derived from the filesystem or harmless to escape. Deciding per
// call site leaves the line *next* to the untrusted one unescaped. So
// arguments are escaped by default and the exceptions are spelled `raw`.
//
// The dropped error is a write to the user's terminal, which a CLI can
// neither report nor recover from.
func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, escapeArgs(a)...) }
func fprintln(w io.Writer, a ...any)               { _, _ = fmt.Fprintln(w, escapeArgs(a)...) }
func fprint(w io.Writer, a ...any)                 { _, _ = fmt.Fprint(w, escapeArgs(a)...) }

// raw opts a string out of the escaping above. Two kinds only: shell code
// envoke generates for its caller to eval, and envoke's own multi-line
// literals, whose newlines are structure rather than content.
//
// Never wrap anything from a config file, a path or a directory name — that
// is the class of bug the escaping exists for. A distinct type, and short, so
// `grep raw(` lists every exception in one screen.
type raw string

// escapeArgs escapes the string and error arguments of a print call, unwraps
// raw, and leaves everything else (ints, file modes, BlockType and friends)
// to fmt.
func escapeArgs(a []any) []any {
	out := make([]any, len(a))
	for i, v := range a {
		switch v := v.(type) {
		case raw:
			out[i] = string(v)
		case string:
			out[i] = sanitize(v)
		case error:
			out[i] = sanitize(v.Error())
		default:
			out[i] = v
		}
	}
	return out
}

// run dispatches a subcommand, writing to the given streams so tests need not
// capture process output. stdin is threaded through only to allow's y/N
// confirmation, the one subcommand that reads it.
func run(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "version", "--version", "-V":
		printVersion(stdout)
		return 0
	case "shell-init":
		return cmdShellInit(args[1:], stdout, stderr)
	case "completion":
		return cmdCompletion(args[1:], stdout, stderr)
	case "shell-hook":
		return cmdShellHook(args[1:], stdout, stderr)
	case "allow":
		return cmdAllow(args[1:], stdout, stderr, stdin)
	case "revoke":
		return cmdRevoke(args[1:], stdout, stderr)
	case "list":
		return cmdList(args[1:], stdout, stderr)
	case "prune":
		return cmdPrune(args[1:], stdout, stderr)
	case "disable":
		return cmdSwitch(args[1:], stdout, stderr, false)
	case "enable":
		return cmdSwitch(args[1:], stdout, stderr, true)
	case "reload":
		return cmdReload(args[1:], stdout, stderr)
	case "exec":
		return cmdExec(args[1:], stderr)
	case "debug":
		return cmdDebug(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fprintf(stderr, "envoke: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

// newFlagSet builds a per-subcommand flag set that never exits the process
// and never prints Go's default usage dump, so run stays a plain function
// returning an exit code and every subcommand prints its own one-line usage.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("envoke "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseFlags parses args, printing usage on stderr and reporting ok=false
// when they're malformed. `-h`/`--help` also returns ok=false so the caller
// stops, but with code 0.
func parseFlags(fs *flag.FlagSet, args []string, usage string, stderr io.Writer) (ok bool, code int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fprintln(stderr, "usage:", usage)
			return false, 0
		}
		fprintf(stderr, "envoke: %v\nusage: %s\n", err, usage)
		return false, 2
	}
	return true, 0
}

// printVersion prints the ldflags-injected version, commit and build date
// (see .goreleaser.yaml; "dev"/"unknown" for a local build) plus the Go
// toolchain and OS/arch.
func printVersion(stdout io.Writer) {
	fprintf(stdout, "envoke %s (commit %s, built %s)\n", version, commit, date)
	fprintf(stdout, "%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

const allowUsage = "envoke allow [--yes|-y] [path]"

const shellInitUsage = "envoke shell-init [bash|zsh|fish|tcsh|powershell]  (defaults to the shell named by $SHELL)"

// detectShell maps a $SHELL value to one of the supported shell names, by
// basename so a shell installed anywhere works. It deliberately has no
// default: emitting a bash hook for an unrecognised shell would produce a
// broken rc file whose breakage surfaces much later than a message here.
func detectShell(shellPath string) (string, error) {
	if shellPath == "" {
		return "", errors.New("$SHELL is not set, so the shell can't be detected -- name it explicitly")
	}

	name := filepath.Base(shellPath)
	// Login shells are conventionally listed in /etc/shells with a leading
	// dash when invoked as one; strip it before matching.
	name = strings.TrimPrefix(name, "-")
	switch name {
	case "bash", "zsh", "fish", "tcsh":
		return name, nil
	case "csh":
		// tcsh is very commonly installed as (or symlinked from) csh.
		return "tcsh", nil
	case "pwsh", "powershell", "pwsh.exe", "powershell.exe":
		return "powershell", nil
	default:
		return "", fmt.Errorf("can't tell which shell %q is -- name it explicitly", shellPath)
	}
}

const completionUsage = "envoke completion [bash|zsh|fish]  (defaults to the shell named by $SHELL)"

// cmdCompletion prints a tab-completion script, generated by the binary for
// the same reason the hooks are: a hand-maintained one drifts from the real
// subcommand list the moment anyone adds one, silently.
func cmdCompletion(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("completion")
	if ok, code := parseFlags(flags, args, completionUsage, stderr); !ok {
		return code
	}

	var shell string
	switch flags.NArg() {
	case 0:
		var err error
		shell, err = detectShell(os.Getenv("SHELL"))
		if err != nil {
			fprintln(stderr, "envoke:", err)
			fprintln(stderr, "usage:", completionUsage)
			return 2
		}
	case 1:
		shell = flags.Arg(0)
	default:
		fprintln(stderr, "usage:", completionUsage)
		return 2
	}

	script, err := shellinit.Completion(shell)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	fprint(stdout, raw(script))
	return 0
}

func cmdShellInit(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("shell-init")
	if ok, code := parseFlags(flags, args, shellInitUsage, stderr); !ok {
		return code
	}

	var shell string
	switch flags.NArg() {
	case 0:
		// Removes the commonest install mistake -- pasting the bash line into
		// a zsh rc -- and costs nothing: an unrecognised guess produces the
		// same error as typing the name wrong.
		var err error
		shell, err = detectShell(os.Getenv("SHELL"))
		if err != nil {
			fprintln(stderr, "envoke:", err)
			fprintln(stderr, "usage:", shellInitUsage)
			return 2
		}
	case 1:
		shell = flags.Arg(0)
	default:
		fprintln(stderr, "usage:", shellInitUsage)
		return 2
	}

	script, err := shellinit.Generate(shell)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	fprint(stdout, raw(script))
	return 0
}

// cmdShellHook is invoked by the generated hook on every directory change.
// If the matched config is trusted it prints executor.Render's output for the
// shell to eval; otherwise it reports the match on stderr and prints nothing,
// so evaluating the empty output stays a safe no-op. Omitting --shell
// defaults to the POSIX profile, which bash's and zsh's hooks rely on.
//
// The directories come as positional arguments, or from ENVOKE_FROM/ENVOKE_TO.
// The environment form exists for the tcsh hook, whose only route into
// `source` is an `eval`: interpolating directory names into a string that gets
// re-parsed is a command-injection hole (see internal/shellinit's tcshHook).
const shellHookUsage = "envoke shell-hook [--shell <name>] [--] <from> <to>  (or set ENVOKE_FROM/ENVOKE_TO)"

func cmdShellHook(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("shell-hook")
	shell := flags.String("shell", "", "shell dialect to render for (bash, zsh, fish, tcsh, powershell)")
	if ok, code := parseFlags(flags, args, shellHookUsage, stderr); !ok {
		return code
	}
	if !executor.IsKnownShell(*shell) {
		fprintf(stderr, "envoke: unknown shell %q (supported: bash, zsh, fish, tcsh, powershell)\n", *shell)
		return 2
	}

	// Silently, and before anything else: this runs on every directory
	// change, so a switched-off envoke costs one stat and says nothing.
	// `envoke debug` is where the state is reported.
	if disabled, _, err := state.Disabled(); err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	} else if disabled {
		return 0
	}

	var from, to string
	switch flags.NArg() {
	case 2:
		from, to = flags.Arg(0), flags.Arg(1)
	case 0:
		var ok bool
		from, ok = os.LookupEnv("ENVOKE_FROM")
		if ok {
			to, ok = os.LookupEnv("ENVOKE_TO")
		}
		if !ok {
			fprintln(stderr, "usage:", shellHookUsage)
			return 2
		}
	default:
		fprintln(stderr, "usage:", shellHookUsage)
		return 2
	}

	entries, err := loadConfigSet()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	code := reportLoadFailures(stderr, entries)

	leaves, enters, err := matcher.Resolve(configset.Configs(entries), from, to)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if len(leaves)+len(enters) == 0 {
		return code
	}

	// On the hot path too, not only in the typed commands: a writable store
	// lets someone forge an approval outright, which is the one warning that
	// has to reach a user who never runs anything but `cd`. Below the
	// early-out, because a forged approval can only take effect where a block
	// would run, and every `cd` that matches nothing would otherwise stat the
	// store's ancestors for a warning about nothing.
	warnUnsafeStore(stderr)

	kept, _ := runnable(stderr, entries, fmt.Sprintf("for %s -> %s", from, to), leaves, enters)
	fprint(stdout, raw(executor.Render(*shell, kept[0], kept[1])))
	return code
}

// locateConfigs resolves where envoke's configs live: the central config and
// the fragment directory, each "" when there isn't one. Either half being
// absent is ordinary.
//
// Split out so the only difference between its two callers is the one that
// matters: loadConfigSet reads the files, configPaths deliberately doesn't.
func locateConfigs() (globalPath, fragmentDir string, err error) {
	path, found, err := config.Locate()
	if err != nil {
		return "", "", err
	}
	if found {
		globalPath = path
	}

	dir, found, err := config.LocateDir()
	if err != nil {
		return "", "", err
	}
	if found {
		fragmentDir = dir
	}
	return globalPath, fragmentDir, nil
}

// emptySetReason says why envoke has nothing to act on, for the commands a
// human typed and is waiting on an answer from.
//
// "You have no envokerc.d directory" and "you have one and every file in it was
// skipped" are the same empty set and want opposite next moves — create the
// directory, or look at what is in it — so a message that only ever claims the
// first sends half its readers looking in the wrong place. Reached only when
// the set is empty, which is why it can afford to locate a second time rather
// than widen what loadConfigSet returns.
func emptySetReason() string {
	_, dir, err := locateConfigs()
	if err != nil || dir == "" {
		return "no central config, and no " + config.DirName + " directory"
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "no central config, and no directory at " + dir
	}
	// The set being empty with the directory present means nothing in it
	// counted as a fragment, and the skip rules are the only way that happens
	// short of it being empty (see config.Fragments).
	return "no central config, and " + dir + " holds no config files -- names starting with \".\" or ending in \"~\", and links to directories, are skipped"
}

// loadConfigSet locates and loads everything envoke acts on.
func loadConfigSet() ([]configset.Entry, error) {
	globalPath, fragmentDir, err := locateConfigs()
	if err != nil {
		return nil, err
	}
	return configset.Load(globalPath, fragmentDir), nil
}

// reportLoadFailures prints the configs in the set that couldn't be read or
// parsed, and returns the exit code to use.
//
// A missing *central* config is silent: $ENVOKERC is honoured verbatim, so
// pointing it at a file you haven't written yet is ordinary. A missing
// *fragment* is not: the directory scan just listed it, so it is a broken
// symlink or a file that vanished.
//
// Nothing here stops the other configs in the set from working — one fragment
// a `git pull` just rewrote must not disable the whole set.
func reportLoadFailures(stderr io.Writer, entries []configset.Entry) int {
	code := 0
	for _, e := range entries {
		if e.Err == nil {
			continue
		}
		if !e.Fragment && errors.Is(e.Err, fs.ErrNotExist) {
			continue
		}
		fprintln(stderr, "envoke:", e.Err)
		code = 1
	}
	return code
}

// runnable filters matches down to the ones envoke may act on, reporting each
// skipped config exactly once however many of its blocks matched. Groups are
// decided in the order given, so reports arrive in the order the blocks would
// have run. refused is for the commands where being held back has to be an
// error rather than a quiet no-op.
func runnable(stderr io.Writer, entries []configset.Entry, context string, groups ...[]matcher.Match) (kept [][]matcher.Match, refused bool) {
	counts := make(map[*config.Config]int)
	for _, g := range groups {
		for _, m := range g {
			counts[m.Config]++
		}
	}

	byConfig := configset.ByConfig(entries)
	allowed := make(map[*config.Config]bool, len(counts))
	decided := make(map[*config.Config]bool, len(counts))

	kept = make([][]matcher.Match, len(groups))
	for i, g := range groups {
		for _, m := range g {
			if !decided[m.Config] {
				decided[m.Config] = true
				allowed[m.Config] = mayRun(stderr, byConfig[m.Config], counts[m.Config], context)
				if !allowed[m.Config] {
					refused = true
				}
			}
			if allowed[m.Config] {
				kept[i] = append(kept[i], m)
			}
		}
	}
	return kept, refused
}

// mayRun answers whether one config's matched blocks may run, and says so on
// stderr when they may not. Nothing here prompts: the answer is either already
// recorded or is `envoke allow` away.
func mayRun(stderr io.Writer, e configset.Entry, matched int, context string) bool {
	decision, err := configset.Decide(e)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return false
	}
	if decision == configset.Run {
		warnUnsafeConfig(stderr, e.Path)
		return true
	}
	if decision == configset.Untrusted {
		warnUnsafeConfig(stderr, e.Path)
		shown := e.Path
		fprintf(stderr, "envoke: %d block(s) matched %s but %s is not trusted: run `envoke allow %s`\n",
			matched, context, shown, shown)
	}
	return false
}

// cmdAllow trusts a config's current content, so shell-hook will run blocks
// matched against it until it is edited again.
//
// With no path it covers the whole set, because that is the unit a user
// thinks in: splitting rules across files is an organisational choice, not a
// decision to approve them one at a time.
//
// A path targets exactly that file — the file, though, not the spelling: any
// name reaching a config the set already holds approves it under the set's own
// name for it, because that is the name the hook looks up.
//
// Review output depends on whether there is a prior approval to compare
// against: a full block dump for a first-time trust, a +/- diff when the file
// changed, nothing when it didn't. One y/N confirmation covers all of it, and
// --yes/-y skips it.
func cmdAllow(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	flags := newFlagSet("allow")
	yes := flags.Bool("yes", false, "skip the y/N confirmation prompt")
	flags.BoolVar(yes, "y", false, "shorthand for --yes")
	if ok, code := parseFlags(flags, args, allowUsage, stderr); !ok {
		return code
	}

	// stdlib flag stops at the first non-flag argument, so `envoke allow
	// <path> --yes` would read --yes as a second path. Picking out this one
	// boolean rather than reimplementing flags-anywhere keeps a config
	// genuinely named `--yes` approvable as `./--yes`.
	var positional []string
	for _, a := range flags.Args() {
		if a == "--yes" || a == "-y" {
			*yes = true
			continue
		}
		positional = append(positional, a)
	}

	paths, derived, code, ok := configTargets(positional, "allow", allowUsage, stderr)
	if !ok {
		return code
	}
	warnUnsafeStore(stderr)

	pending, failed := reviewForApproval(stdout, stderr, paths, derived)
	if len(pending) == 0 {
		if failed {
			return 1
		}
		return 0
	}

	if !*yes {
		fprint(stdout, "envoke: trust and run these blocks on every matching cd? [y/N] ")
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			fprintln(stderr, "envoke: aborted, not trusted")
			return 1
		}
	}

	for _, c := range pending {
		if err := trust.Allow(c.path, c.content); err != nil {
			fprintln(stderr, "envoke:", err)
			return 1
		}
		fprintf(stdout, "envoke: trusted %s\n", c.path)
	}
	// allow is a child process and cannot export into the shell that ran it,
	// so what was just approved applies from the next cd on.
	fprintln(stdout, `envoke: to apply it to this shell without leaving the directory: eval "$(envoke reload)"`)
	if failed {
		return 1
	}
	return 0
}

// candidate is one config reviewed and waiting on the confirmation. The
// content travels with it because it must be the same bytes that were shown:
// re-reading after the y/N answer would record a hash for content the user
// never saw.
type candidate struct {
	path    string
	content []byte
}

// configTargets resolves what `envoke allow` or `envoke revoke` should act on:
// the given path, or every config envoke would load when none is given.
//
// Shared by the two so they cannot drift on what "no path" means: a set that one
// command approves has to be a set the other can withdraw, or the whole-set
// default has no inverse.
//
// derived says which of the two happened, because "this file is not there" is
// not the same report in both. A derived path is one envoke chose, and a central
// config that hasn't been written yet is a state the hot path already treats as
// ordinary; a path the user typed and that isn't there is a typo.
func configTargets(positional []string, cmd, usage string, stderr io.Writer) (paths []string, derived bool, code int, ok bool) {
	switch len(positional) {
	case 1:
		return positional, false, 0, true
	case 0:
		paths, err := configPaths()
		if err != nil {
			fprintln(stderr, "envoke:", err)
			return nil, true, 1, false
		}
		if len(paths) == 0 {
			fprintf(stderr, "envoke: no config found; pass a path explicitly: envoke %s <path>\n", cmd)
			return nil, true, 1, false
		}
		return paths, true, 0, true
	default:
		fprintln(stderr, "usage:", usage)
		return nil, false, 2, false
	}
}

// reviewForApproval prints what each path would have envoke run and returns
// the ones still needing approval. failed reports that some path couldn't be
// read or parsed — that one is skipped rather than aborting the others, since
// with a set of fragments the broken one may not be the one being approved.
//
// Each path is reviewed the way the set will actually load it, which for a
// fragment means through the symlink: the file that gets parsed, the directory
// its "./" patterns resolve against, and the bound its blocks are held to are
// all properties of the target rather than of the link. Approving content whose
// displayed meaning differs from its effective one approves nothing.
func reviewForApproval(stdout, stderr io.Writer, paths []string, derived bool) (pending []candidate, failed bool) {
	// The set is the only thing that can answer "is this config confined, and
	// to what": that decision needs the config directory's resolved root, which
	// no single file carries. Its entries also hold the bytes configset has
	// already read, so a path in the set is not read a second time here.
	entries, err := loadConfigSet()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return nil, true
	}

	reviewed := make(map[string]bool, len(paths))
	for _, target := range paths {
		// Whichever branch supplies it, one read feeds the parse, the review
		// and the trust record alike.
		e, inSet := entryFor(entries, target)
		if !inSet {
			// A config the set does not hold: a path named explicitly, or one
			// the set's dedup dropped. Nothing will ever load it as a fragment,
			// so this is the matching loader, and it reads once as well.
			cfg, content, loadErr := config.LoadFile(target)
			e = configset.Entry{Path: target, Config: cfg, Content: content, Err: loadErr}
		}
		// Nothing below names the argument again. The entry's own path is the
		// file the set loaded and the name the hook will look up, so it is the
		// one the review has to describe and the one the record has to be keyed
		// on; approving under any other spelling records trust nothing reads.
		path := e.Path
		if reviewed[path] {
			// Two names for one file — configPaths lists both where the set's
			// dedup kept one entry — would otherwise dump the same blocks twice
			// under one confirmation.
			continue
		}
		reviewed[path] = true

		warnUnsafeConfigAndDir(stderr, path)

		if e.Err != nil {
			if derived && !e.Fragment && errors.Is(e.Err, fs.ErrNotExist) {
				// reportLoadFailures makes the same exemption for the same
				// reason: $ENVOKERC is honoured verbatim, so a central config
				// nobody has written yet is an ordinary state rather than a
				// failure, and the whole-set form did not name that file. It has
				// to stay a success too, because a dotfiles bootstrap runs
				// `envoke allow --yes` under set -e and the fragments it does
				// hold approve cleanly. A path typed by hand keeps failing.
				fprintf(stdout, "envoke: %s does not exist yet -- nothing to review\n", path)
				continue
			}
			fprintln(stderr, "envoke:", e.Err)
			failed = true
			continue
		}
		current := e.Content

		previous, hadPrevious, err := trust.PreviousContent(path)
		if err != nil {
			fprintln(stderr, "envoke:", err)
			failed = true
			continue
		}
		alreadyTrusted, err := trust.IsTrusted(path, current)
		if err != nil {
			fprintln(stderr, "envoke:", err)
			failed = true
			continue
		}

		if hadPrevious && alreadyTrusted && previous == string(current) {
			// alreadyTrusted is tested on top of the content comparison so a
			// half-written record (content copy landed, hash record didn't)
			// can't report an untrusted config as trusted forever. In that
			// state it falls through and gets re-approved.
			fprintf(stdout, "envoke: %s is unchanged since it was last trusted -- nothing to review\n", path)
			continue
		}

		if hadPrevious && canDiff(previous, string(current)) {
			printDiff(stdout, path, previous, string(current))
		} else {
			fprintf(stdout, "envoke: about to trust %s -- review each block below before confirming:\n\n", path)
			printBlocksForReview(stdout, e.Config.Blocks)
		}
		if printConfigBound(stdout, "  note: ", e) {
			fprintln(stdout)
		}
		pending = append(pending, candidate{path: path, content: current})
	}
	return pending, failed
}

// entryFor finds the loaded entry for a path, so a config `envoke allow` was
// pointed at is reviewed as the set will load it — content, parse base and
// confinement alike — rather than as a standalone file, and so its approval is
// recorded under the name the hook will look up.
//
// Matched on the file, not on the spelling. The set reaches a fragment through
// the resolved config directory (config.Fragments resolves before walking it),
// while a user reaches it by whatever spelling their home layout gives them —
// and in the ordinary dotfiles layout, which is also what tab completion hands
// them, those are two different strings for one file.
//
// Two passes, spelling before inode. A textual match is an identity match as
// well, so the second pass only ever answers for a target the first did not, and
// where one file is in the set under two names — a hard-linked fragment; the
// dedup's key is a resolved path and cannot collapse those — the target's own
// name in the set wins over another name's inode, which is the name the hook
// looks up for that entry. What the order is for is cost: the whole-set form
// feeds this configPaths' names, which are the entries' own, so every target
// settles above and nothing is stat'd at all, where testing both per candidate
// stats both sides of every earlier entry for every target — n(n-1) syscalls
// across a set, for an answer string equality already had.
func entryFor(entries []configset.Entry, path string) (configset.Entry, bool) {
	for _, e := range entries {
		if sameName(path, e.Path) {
			return e, true
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return configset.Entry{}, false
	}
	for _, e := range entries {
		if sameFileAs(info, e.Path) {
			return e, true
		}
	}
	return configset.Entry{}, false
}

// sameName reports whether two paths are one string once made absolute.
//
// It is the only test that can answer for a file that isn't there — `envoke
// allow` on an $ENVOKERC nobody has written yet is an ordinary state, and two
// spellings of a file that does not exist cannot be told apart by anything
// stronger — which is why both scans below exhaust it before statting.
func sameName(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// sameFileAs reports whether path is the file info describes — the file, not the
// spelling, and it needs no assumption about which symlinks either side
// followed. A path that cannot be stat'd is not a match: a file nothing can read
// is not evidence about which file was meant.
func sameFileAs(info os.FileInfo, path string) bool {
	other, err := os.Stat(path)
	return err == nil && os.SameFile(info, other)
}

// setSpelling is the name the config set reaches path's file by — the name its
// trust record is keyed on — given the set's names in configPaths order. A path
// none of them names comes back unchanged, so a record for a config outside the
// set, which is what pointing $ENVOKERC somewhere else leaves behind, stays
// revocable by the name it was approved under.
//
// Order matters: it is configPaths' order, which is the set's, so a file two
// names reach resolves to whichever of them configset.Load's dedup kept. Textual
// before physical for the reason entryFor is, and the target is stat'd once: a
// name nothing in the set spells and nothing on disk answers for — `envoke
// revoke` on a record left by a config since deleted — comes back from that one
// syscall rather than from two per name in the set.
//
// Only the names are needed, so nothing here reads a config. Withdrawing a
// record is a decision about a path, not about content.
func setSpelling(known []string, path string) string {
	for _, p := range known {
		if sameName(path, p) {
			return p
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return path
	}
	for _, p := range known {
		if sameFileAs(info, p) {
			return p
		}
	}
	return path
}

// printConfigBound reports the two things that decide whether a config's blocks
// can fire at all and that no other output carries: the file a symlinked config
// leads to, and — for a confined one — the directory matcher.NewMatch keeps
// every one of its blocks inside, whatever their patterns say. Without it a
// project fragment whose pattern points out of its own tree is approved, lists
// as trusted, shows as loaded, and then never fires.
//
// Only the commands a human is reading call this. It costs a readlink per
// config to report something the hook could not act on anyway, which is the
// same reason warnUnsafeConfigAndDir is kept off that path.
//
// lead prefixes every line: debug indents these under a config's status line,
// where the nesting says what they describe, while allow's review has nothing
// to nest under and needs each line marked as a note rather than read as one
// more block of the config being approved.
//
// noted says whether anything was printed, for a caller that follows it with a
// blank line.
func printConfigBound(w io.Writer, lead string, e configset.Entry) (noted bool) {
	if target, ok := linkTarget(e.Path); ok {
		fprintf(w, "%ssymlink to %s\n", lead, target)
		noted = true
	}
	if e.Config == nil || !e.Config.Local {
		return noted
	}
	fprintf(w, "%sconfined to %s -- its blocks cannot match outside that directory, whatever their patterns say\n",
		lead, e.Config.Dir)
	if e.Config.DirUnresolved {
		fprintf(w, "%sits symlink could not be followed, so that bound is the link's own directory\n", lead)
	}
	return true
}

// linkTarget is the file a config symlink leads to, every link in the chain
// followed: that is the file config.LoadFragmentResolved parses and the
// directory a confined config is bounded to, so reporting the link's own text
// would name neither. ok is false for a config that is not a symlink.
//
// The text is the fallback for a chain that can't be followed, since it is then
// all there is to report — the state Config.DirUnresolved describes.
func linkTarget(path string) (string, bool) {
	text, err := os.Readlink(path)
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, true
	}
	if !filepath.IsAbs(text) {
		text = filepath.Join(filepath.Dir(path), text)
	}
	return text, true
}

// configPaths lists every config file envoke would load, in set order,
// without reading any of them: `envoke allow` needs the names before the
// contents, and loading here would read every file twice.
func configPaths() ([]string, error) {
	globalPath, fragmentDir, err := locateConfigs()
	if err != nil {
		return nil, err
	}

	var paths []string
	if globalPath != "" {
		paths = append(paths, globalPath)
	}
	if fragmentDir == "" {
		return paths, nil
	}

	_, fragments, err := config.Fragments(fragmentDir)
	if err != nil {
		return nil, err
	}
	return append(paths, fragments...), nil
}

// printBlocksForReview dumps every block's pattern, line and script body, so
// approving a config means seeing the code that will run rather than a hash
// being recorded silently. First-time trust only; see printDiff.
func printBlocksForReview(w io.Writer, blocks []config.Block) {
	if len(blocks) == 0 {
		fprintln(w, "  (no blocks defined)")
		fprintln(w)
		return
	}
	for _, b := range blocks {
		fprintf(w, "  %s %s (line %d)\n", b.Type, b.RawPattern, b.Line)
		for _, line := range strings.Split(b.Script, "\n") {
			fprintf(w, "    %s\n", line)
		}
		fprintln(w)
	}
}

// sanitize escapes the characters that let text redraw a terminal rather than
// appear in it. Applied to every print argument by fprintf and friends.
//
// An untrusted config gets dumped for review, a fragment can be a symlink into
// a repository, and a directory name comes from whatever was cloned. An escape
// sequence in any of them could scroll the review out of sight, colour a
// convincing "already trusted" line over the real one, or move the cursor back
// over the y/N question.
func sanitize(s string) string {
	if !needsEscaping(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			// Filenames on Unix are arbitrary bytes, so this is reachable
			// from any path envoke prints. Showing the byte beats letting the
			// terminal guess at its encoding.
			_, _ = fmt.Fprintf(&b, `\x%02x`, s[i])
		case !isDisplayUnsafe(r):
			b.WriteString(s[i : i+size])
		case r < 0x100:
			_, _ = fmt.Fprintf(&b, `\x%02x`, r)
		default:
			_, _ = fmt.Fprintf(&b, `\u%04x`, r)
		}
		i += size
	}
	return b.String()
}

// needsEscaping is the fast path: anything outside printable ASCII (minus
// tab) sends sanitize down the per-rune path. Deliberately pessimistic about
// non-ASCII — an accented directory name costs one extra pass and comes out
// unchanged.
func needsEscaping(s string) bool {
	for i := 0; i < len(s); i++ {
		if (s[i] < 0x20 && s[i] != '\t') || s[i] >= 0x7f {
			return true
		}
	}
	return false
}

// isDisplayUnsafe reports whether a rune can change what a terminal shows
// rather than adding to it:
//
//   - C0 (newline included — a message is one line, so a newline in a value is
//     a forged second line), DEL and C1, which carry ESC and with it cursor
//     movement, colour and erase-display.
//   - The bidi controls (202A-202E, 2066-2069, 200E/200F, 061C), which reorder
//     text without changing a byte of it.
//   - Zero-width and invisible characters (200B-200D, FEFF), which let two
//     different paths render identically, and 2028/2029, which some terminals
//     break lines on.
func isDisplayUnsafe(r rune) bool {
	switch {
	case r == '\t':
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r == 0x061c, r >= 0x200b && r <= 0x200f, r >= 0x2028 && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xfeff:
		return true
	default:
		return false
	}
}

// printDiff replaces printBlocksForReview's full dump when there is a prior
// approval to compare against, so re-approving after a small edit doesn't
// mean re-reading the whole file.
func printDiff(stdout io.Writer, path, previous, current string) {
	fprintf(stdout, "envoke: %s changed since it was last trusted -- here's what's different:\n\n", path)
	for _, line := range diffLines(splitLines(previous), splitLines(current)) {
		fprintln(stdout, line)
	}
	fprintln(stdout)
}

// splitLines drops a trailing carriage return the way the parser's scanner
// does, so a CRLF config doesn't print stray \r at every line end.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// diffLines returns only the lines that differ between old and new, prefixed
// "- " and "+ ". Common lines are aligned via a longest common subsequence and
// omitted rather than printed as context, so an edit touching one block
// doesn't drag the rest of the config into the output.
//
// A plain O(len(old)*len(new)) DP table rather than Myers: diffCap keeps the
// inputs small, and this needs nothing beyond the stdlib.
func diffLines(oldLines, newLines []string) []string {
	n, m := len(oldLines), len(newLines)

	// The (n+1)x(m+1) table flattened into one allocation: lcs[i*stride+j] is
	// the LCS length of oldLines[i:] and newLines[j:]. int32 is safe because
	// the values are bounded by the line count, which diffCap bounds.
	stride := m + 1
	lcs := make([]int32, (n+1)*stride)
	for i := n - 1; i >= 0; i-- {
		row, next := i*stride, (i+1)*stride
		for j := m - 1; j >= 0; j-- {
			switch {
			case oldLines[i] == newLines[j]:
				lcs[row+j] = lcs[next+j+1] + 1
			case lcs[next+j] >= lcs[row+j+1]:
				lcs[row+j] = lcs[next+j]
			default:
				lcs[row+j] = lcs[row+j+1]
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			i++
			j++
		case lcs[(i+1)*stride+j] >= lcs[i*stride+j+1]:
			out = append(out, "- "+oldLines[i])
			i++
		default:
			out = append(out, "+ "+newLines[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+oldLines[i])
	}
	for ; j < m; j++ {
		out = append(out, "+ "+newLines[j])
	}
	return out
}

// diffCap bounds the LCS table, which is O(n*m) in time and memory and would
// otherwise grow with a generated or appended-to config the parser happily
// accepts. At this cap the table is 2000*2000 int32 = 16 MiB. Beyond it,
// cmdAllow shows the full block dump instead: less convenient, but bounded.
const diffCap = 2000

// canDiff reports whether a line-level diff is worth attempting between two
// config versions, given diffCap.
func canDiff(previous, current string) bool {
	return strings.Count(previous, "\n") < diffCap && strings.Count(current, "\n") < diffCap
}

const revokeUsage = "envoke revoke [path]"

// cmdRevoke withdraws trust for a config. Without it the only ways back are
// editing the config, which revokes trust as a side effect, or deleting a
// sha256-named file out of the data home by hand.
//
// With no path it covers the whole set, for the same reason `envoke allow`
// does and because that is what makes the two inverses: a set approved by one
// command has to be withdrawable by one command. A path targets exactly that
// file, whether or not it is in the set.
//
// It names every file it acted on and exits 0 whether it removed three records
// or none: the end state asked for is "none of these is trusted", and a config
// that was never approved already satisfies it. Deliberately unlike `reload`,
// which exits non-zero when it was refused, because there doing nothing means
// the blocks the user typed for never ran.
//
// A record left behind by a config that has since left the set is not this
// command's: `list` reconciles the set against the store, and `prune` drops the
// records whose config file no longer exists. Nothing here widens revoke into
// either.
func cmdRevoke(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("revoke")
	if ok, code := parseFlags(flags, args, revokeUsage, stderr); !ok {
		return code
	}

	paths, _, code, ok := configTargets(flags.Args(), "revoke", revokeUsage, stderr)
	if !ok {
		return code
	}

	// The set's own names, so a target can be matched to the file it is rather
	// than to the string it was typed as. Listed once for the whole loop, and the
	// error dropped: no names means every target stays spelled as given, which is
	// what a path outside the set gets anyway — and revoking one record must not
	// become impossible because something else in $ENVOKERC_D is broken.
	known, _ := configPaths()

	done := make(map[string]bool, len(paths))
	for _, target := range paths {
		// The record `allow` wrote is keyed on the set's spelling, so that is
		// what has to be removed however the user reached the file.
		path := setSpelling(known, target)
		if done[path] {
			// Two names for one file, which is what the set's dedup collapses:
			// reporting it twice would print a revocation and a "was not trusted"
			// for the same file.
			continue
		}
		done[path] = true

		// Both spellings, where the user's differs from the set's. Trust is keyed
		// on the path text, so a file approved before this command resolved
		// identity has a record under whatever was typed then — inert, but still a
		// plaintext copy of the config in the store, and no other command can name
		// it. The end state asked for is that this file is not trusted.
		keys := []string{path}
		if target != path {
			keys = append(keys, target)
		}

		found := false
		for _, key := range keys {
			removed, err := trust.Revoke(key)
			if err != nil {
				fprintln(stderr, "envoke:", err)
				return 1
			}
			found = found || removed
		}
		if !found {
			fprintf(stdout, "envoke: %s was not trusted -- nothing to revoke\n", path)
			continue
		}
		fprintf(stdout, "envoke: revoked trust for %s\n", path)
	}
	return 0
}

const listUsage = "envoke list"

// cmdList reconciles two different questions: what envoke would load, and
// what the store has recorded. A record can outlive the config it was written
// for, and a config can be in the set with no record at all — a file being
// skipped on every cd. So the set comes first, with the status each file would
// get on the next `cd`, then whatever records are left over.
//
// It is also the only place a user would notice that the store keeps a
// plaintext copy of every approved config, which routinely means secrets.
func cmdList(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("list")
	if ok, code := parseFlags(flags, args, listUsage, stderr); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintln(stderr, "usage:", listUsage)
		return 2
	}

	entries, err := loadConfigSet()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	records, err := trust.List()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	if len(entries) == 0 && len(records) == 0 {
		fprintln(stdout, "envoke: no configs, and nothing in the trust store")
		return 0
	}

	inSet := make(map[string]bool, len(entries))
	if len(entries) > 0 {
		fprintln(stdout, "envoke: configs envoke would load")
		for _, e := range entries {
			if abs, err := filepath.Abs(e.Path); err == nil {
				inSet[abs] = true
			}
			fprintf(stdout, "  %-9s %-9s %s\n", entryStatus(e), entryKind(e), e.Path)
		}
	}

	var leftover []trust.Record
	for _, r := range records {
		if r.ConfigPath == "" || !inSet[r.ConfigPath] {
			leftover = append(leftover, r)
		}
	}
	if len(leftover) == 0 {
		return 0
	}

	if len(entries) > 0 {
		fprintln(stdout)
	}
	// Not an error, and not necessarily stale: a record for a config kept
	// under a different $ENVOKERC, or one since split into fragments, is
	// ordinary.
	fprintln(stdout, "envoke: other trust records (not in the current config set)")
	for _, r := range leftover {
		if r.ConfigPath == "" {
			fprintf(stdout, "  %-9s %-9s <unknown path, approved by an older envoke> (%s)\n", "unknown", "", r.StorePath)
			continue
		}
		fprintf(stdout, "  %-9s %-9s %s\n", listStatus(r), "", r.ConfigPath)
	}
	return 0
}

func entryKind(e configset.Entry) string {
	if e.Fragment {
		return "fragment"
	}
	return "central"
}

// entryStatus is what would happen to a config in the set on the next `cd`.
// It distinguishes never-approved from approved-then-edited, which the trust
// decision does not — both are "untrusted" to anything that executes blocks,
// but they need different things from the user.
func entryStatus(e configset.Entry) string {
	if e.Err != nil {
		if errors.Is(e.Err, fs.ErrNotExist) {
			return "missing"
		}
		return "unreadable"
	}

	decision, err := configset.Decide(e)
	if err != nil {
		return "unreadable"
	}
	if decision == configset.Run {
		return "trusted"
	}
	if _, hadPrevious, err := trust.PreviousContent(e.Path); err == nil && hadPrevious {
		return "changed"
	}
	return "untrusted"
}

// listStatus classifies a record whose config is not in the set, against the
// file as it exists now.
//
// The one place that reads a config outside config.LoadFile. The read-once
// rule exists so the bytes *executed* are the bytes approved; list executes
// nothing, so the worst a file changing mid-listing can do is print a status
// that was true a moment earlier. Parsing would be worse: an unparseable
// config is still a trusted record worth listing.
func listStatus(r trust.Record) string {
	content, err := os.ReadFile(r.ConfigPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "missing"
		}
		return "unreadable"
	}
	trusted, err := trust.IsTrusted(r.ConfigPath, content)
	if err != nil {
		return "unreadable"
	}
	if !trusted {
		return "changed"
	}
	return "trusted"
}

const pruneUsage = "envoke prune"

// cmdPrune drops records whose config no longer exists. Each one keeps a
// plaintext copy of a config the user has already deleted, which is the part
// that matters.
func cmdPrune(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("prune")
	if ok, code := parseFlags(flags, args, pruneUsage, stderr); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintln(stderr, "usage:", pruneUsage)
		return 2
	}

	removed, skipped, err := trust.Prune()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	for _, r := range removed {
		fprintf(stdout, "envoke: removed the trust record for %s (config no longer exists)\n", r.ConfigPath)
	}
	if len(removed) == 0 {
		fprintln(stdout, "envoke: nothing to prune")
	}
	if len(skipped) > 0 {
		fprintf(stderr, "envoke: %d record(s) approved by an older envoke have no recorded path and were left alone; remove them by hand or re-run `envoke allow` on those configs\n", len(skipped))
	}
	return 0
}

const reloadUsage = `envoke reload [--shell <name>]  (used as: eval "$(envoke reload)")`

// cmdReload re-applies the enter blocks for the current directory, for the
// case `envoke allow` cannot cover: allow runs as a child of your shell, so a
// freshly approved config only takes effect on the next cd. This prints the
// same shell text the hook does, for the caller to eval.
//
// Only enter blocks, and nothing is unwound: nothing has been left, and envoke
// does not snapshot state to restore later.
func cmdReload(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("reload")
	shell := flags.String("shell", "", "shell dialect to render for (bash, zsh, fish, tcsh, powershell)")
	if ok, code := parseFlags(flags, args, reloadUsage, stderr); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintln(stderr, "usage:", reloadUsage)
		return 2
	}
	if !executor.IsKnownShell(*shell) {
		fprintf(stderr, "envoke: unknown shell %q (supported: bash, zsh, fish, tcsh, powershell)\n", *shell)
		return 2
	}

	if disabled, source, err := state.Disabled(); err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	} else if disabled {
		fprintf(stderr, "envoke: disabled by %s -- nothing was applied\n", source)
		return 0
	}

	dir, err := currentDir()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	entries, err := loadConfigSet()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	code := reportLoadFailures(stderr, entries)
	if len(entries) == 0 {
		fprintln(stderr, "envoke: no config found -- nothing to reload")
		return 1
	}
	warnUnsafeStore(stderr)

	enters, err := matcher.Enters(configset.Configs(entries), dir)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	kept, refused := runnable(stderr, entries, "for the current directory", enters)
	fprint(stdout, raw(executor.Render(*shell, nil, kept[0])))

	// Louder than shell-hook's equivalent: reload was typed, so a config that
	// didn't get applied has to be an error rather than a silent no-op.
	if refused {
		return 1
	}
	return code
}

// currentDir prefers $PWD over os.Getwd for the same reason the hooks pass the
// shell's own $PWD: through a symlinked directory the two disagree, and the
// patterns a user writes describe the path they cd'd through.
func currentDir() (string, error) {
	if pwd := os.Getenv("PWD"); filepath.IsAbs(pwd) {
		return pwd, nil
	}
	return os.Getwd()
}

const (
	disableUsage = "envoke disable"
	enableUsage  = "envoke enable"
)

// cmdSwitch backs both `envoke disable` and `envoke enable`. Splitting them
// would duplicate the part that matters: telling the user when
// $ENVOKE_DISABLE is going to override what they just asked for, so a
// `disable` that appears to do nothing has a visible reason.
func cmdSwitch(args []string, stdout, stderr io.Writer, enable bool) int {
	usage := disableUsage
	if enable {
		usage = enableUsage
	}

	flags := newFlagSet(strings.TrimPrefix(usage, "envoke "))
	if ok, code := parseFlags(flags, args, usage, stderr); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintln(stderr, "usage:", usage)
		return 2
	}

	apply, verb := state.Disable, "disabled"
	if enable {
		apply, verb = state.Enable, "enabled"
	}
	if err := apply(); err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	fprintf(stdout, "envoke: %s for every shell\n", verb)

	disabled, source, err := state.Disabled()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if source == state.Env && disabled == enable {
		fprintf(stderr, "envoke: warning: %s is set in this shell and overrides that, so envoke stays %s here until you unset it\n",
			state.DisableEnv, enabledWord(disabled))
	}
	return 0
}

func enabledWord(disabled bool) string {
	if disabled {
		return "off"
	}
	return "on"
}

// transitionArgs resolves the <from> <to> pair shared by exec and debug.
// Both are typed by a human, unlike shell-hook which only ever receives
// generated arguments, so both accept relative paths and infer what they can.
//
// The two halves are not equally inferable, which is why <to> may be omitted
// on its own: envoke can always work out where you are, and can only be told
// where you came from. OLDPWD is a POSIX shell convention that PowerShell has
// no counterpart to (its hook tracks the previous directory in a shell
// variable of its own), so the no-argument form is a POSIX convenience and
// `envoke <cmd> <from>` is the form that works in every shell.
//
// cmd names the caller so the error can print a command a user can retype;
// the two callers are the only place the name is known.
func transitionArgs(cmd string, args []string) (from, to string, err error) {
	var inferTo bool
	switch len(args) {
	case 0:
		from, inferTo = os.Getenv("OLDPWD"), true
		if from == "" {
			return "", "", fmt.Errorf("this shell doesn't export OLDPWD (PowerShell never does), so there's no previous directory to infer -- name the one you came from: envoke %s <from>", cmd)
		}
	case 1:
		from, inferTo = args[0], true
	case 2:
		from, to = args[0], args[1]
	default:
		return "", "", fmt.Errorf("expected at most two arguments, got %d", len(args))
	}
	if inferTo {
		if to, err = currentDir(); err != nil {
			return "", "", err
		}
	}

	if from, err = filepath.Abs(from); err != nil {
		return "", "", err
	}
	if to, err = filepath.Abs(to); err != nil {
		return "", "", err
	}
	return from, to, nil
}

const execUsage = "envoke exec [<from> [<to>]]  (<to> defaults to the current directory, <from> to $OLDPWD)"

// cmdExec runs the matching blocks for non-interactive callers — scripts,
// Makefiles, CI — with no interactive shell to hook into.
//
// Each block runs in its own `sh -c`, so export, source and cd inside one
// affect that subprocess and nothing else. Anything meant to change the
// caller's own shell needs the generated hook instead.
//
// Trust is enforced inside envoke.Transition, so no future caller of that
// package can forget it.

func cmdExec(args []string, stderr io.Writer) int {
	flags := newFlagSet("exec")
	if ok, code := parseFlags(flags, args, execUsage, stderr); !ok {
		return code
	}
	from, to, err := transitionArgs("exec", flags.Args())
	if err != nil {
		fprintln(stderr, "envoke:", err)
		fprintln(stderr, "usage:", execUsage)
		return 2
	}

	// One argument names <from> and leaves <to> inferred, which can be read as
	// "run the blocks for this directory" -- the opposite direction, and here
	// that means running the named directory's leave blocks. debug leads with
	// the pair it resolved, so the misreading corrects itself there; this is
	// that line for exec. Scoped to this form alone: the two-argument form
	// states the pair already, and the no-argument form is what scripts use,
	// which must keep printing nothing. On stderr, which is also the only
	// stream exec is given -- its stdout belongs to the blocks it runs, and a
	// caller capturing them must not find a diagnostic mixed in.
	if len(flags.Args()) == 1 {
		fprintf(stderr, "envoke exec: %s -> %s\n", from, to)
	}

	// Unlike shell-hook, out loud: exec is called deliberately, and silently
	// running nothing is how a build ends up missing half its environment.
	// Still exit 0 -- being switched off is not a failure.
	if disabled, source, err := state.Disabled(); err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	} else if disabled {
		fprintf(stderr, "envoke: disabled by %s -- no blocks were run\n", source)
		return 0
	}

	entries, err := loadConfigSet()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	warnUnsafeStore(stderr)

	// Go's default handling would terminate envoke on the spot and leave the
	// block's `sh` running. Cancelling the context instead interrupts the
	// script and gives it killGrace to clean up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := envoke.Transition(ctx, entries, from, to); err != nil {
		if ctx.Err() != nil {
			// Conventional status for "died from a signal", which is not news
			// to whoever sent it.
			return 130
		}
		for _, line := range strings.Split(err.Error(), "\n") {
			fprintln(stderr, "envoke:", line)
		}
		if errors.Is(err, envoke.ErrNoConfig) {
			fprintf(stderr, "envoke: %s\n", emptySetReason())
		}
		if errors.Is(err, envoke.ErrUntrusted) {
			fprintln(stderr, "envoke: approve a config with `envoke allow` before it will run here")
		}
		if errors.Is(err, executor.ErrNoShell) {
			fprintln(stderr, "envoke: exec runs each block through `sh`; install a POSIX shell (Git for Windows, MSYS2 or WSL each provide one) or use the shell hook, which runs blocks in the shell you already have")
		}
		return 1
	}
	return 0
}

// cmdDebug prints which enter/leave blocks would fire for a directory
// transition, without running them and regardless of trust. It notes whether
// each config is trusted, since that decides whether shell-hook would actually
// run what is listed.
const debugUsage = "envoke debug [<from> [<to>]]  (<to> defaults to the current directory, <from> to $OLDPWD)"

func cmdDebug(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("debug")
	if ok, code := parseFlags(flags, args, debugUsage, stderr); !ok {
		return code
	}
	from, to, err := transitionArgs("debug", flags.Args())
	if err != nil {
		fprintln(stderr, "envoke:", err)
		fprintln(stderr, "usage:", debugUsage)
		return 2
	}

	entries, err := loadConfigSet()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if len(entries) == 0 {
		fprintf(stderr, "envoke: no config found (%s)\n", emptySetReason())
		return 1
	}
	code := reportLoadFailures(stderr, entries)

	warnUnsafeStore(stderr)
	for _, e := range entries {
		if e.Err == nil {
			warnUnsafeConfigAndDir(stderr, e.Path)
		}
	}

	leaves, enters, err := matcher.Resolve(configset.Configs(entries), from, to)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	fprintf(stdout, "envoke debug: %s -> %s\n", from, to)
	for _, e := range entries {
		fprintf(stdout, "  config %s (%s)\n", e.Path, debugStatus(e))
		printConfigBound(stdout, "    ", e)
	}

	// debug never executes anything, so being switched off doesn't stop it --
	// but it changes whether what follows would happen for real.
	disabled, source, err := state.Disabled()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if disabled {
		fprintf(stdout, "  envoke is disabled by %s -- nothing below would run; re-enable with `envoke enable`\n", source)
	}

	if len(leaves)+len(enters) == 0 {
		fprintln(stdout, "  no blocks would fire")
		return code
	}
	printWorkingDirNote(stdout, to, leaves, enters)
	for _, matches := range [][]matcher.Match{leaves, enters} {
		for _, m := range matches {
			fprintf(stdout, "  %s %s (line %d of %s: %s)\n",
				m.Block.Type, m.Dir, m.Block.Line, m.Config.Path, m.Block.RawPattern)
			printIndentedScript(stdout, m.Block.Script)
		}
	}
	return code
}

// debugStatus explains, for one config in the set, whether what `envoke
// debug` is about to list would actually run — and what to type if not.
func debugStatus(e configset.Entry) string {
	if e.Err != nil {
		return "failed to load: " + e.Err.Error()
	}
	decision, err := configset.Decide(e)
	if err != nil {
		return "trust record unreadable: " + err.Error()
	}
	if decision == configset.Run {
		return "trusted"
	}
	return fmt.Sprintf("NOT trusted -- run `envoke allow %s` before these would actually run", e.Path)
}

// printWorkingDirNote spells out where a matched script actually runs, when
// that is not the directory it matched. debug lists the matched directory next
// to each block, which reads as though relative paths resolve from there: true
// for exec, false for the hook, which eval's the block in a shell that has
// already landed somewhere else.
func printWorkingDirNote(stdout io.Writer, to string, matches ...[]matcher.Match) {
	differs := false
	for _, ms := range matches {
		for _, m := range ms {
			if m.Dir != to {
				differs = true
			}
		}
	}
	if !differs {
		return
	}
	fprintf(stdout, "  note: via the shell hook these run in %s, where your shell lands;\n", to)
	fprintln(stdout, "        via `envoke exec` each runs in the directory it matched.")
	fprintln(stdout, "        $ENVOKE_DIR always names the matched directory -- use it for relative paths.")
}

// printIndentedScript prints a block's script body under its summary line, so
// `envoke debug` shows the code that would run and not just metadata.
func printIndentedScript(stdout io.Writer, script string) {
	for _, line := range strings.Split(script, "\n") {
		fprintf(stdout, "    %s\n", line)
	}
}

// warnUnsafeConfig warns, never blocks, when a config is writable by group or
// other: content-hash revocation stops a silently-modified config from
// running, this flags the permissions that make the modification possible. A
// Stat failure is ignored — the caller's own load handles a missing file.
//
// Per file, unlike the store warning, since there is a set of configs.
func warnUnsafeConfig(stderr io.Writer, path string) {
	if unsafe, mode, err := config.UnsafePermissions(path); err == nil && unsafe {
		fprintf(stderr, "envoke: warning: %s is writable by group/other (mode %o) -- consider tightening its permissions\n", path, mode)
	}
}

// warnUnsafeConfigAndDir adds the containing directory's permissions to the
// file's own: whoever can write the directory can replace the config
// wholesale, the stronger of the two signals.
//
// Deliberately not used by the shell hook — a second stat per config on the
// path every `cd` takes. `allow` and `debug` are where it earns the syscall.
func warnUnsafeConfigAndDir(stderr io.Writer, path string) {
	warnUnsafeConfig(stderr, path)
	if unsafe, mode, dir, err := config.UnsafeDirPermissions(path); err == nil && unsafe {
		fprintf(stderr, "envoke: warning: the directory %s is writable by group/other (mode %o) -- anyone who can write it can replace %s outright; run `chmod go-w %s`\n",
			dir, mode, filepath.Base(path), dir)
	}
}

// warnUnsafeStore warns when the trust store itself is group/other-writable:
// that lets someone forge an approval outright, rather than merely tamper with
// a config whose next edit would revoke its own trust. Allow's 0o700 doesn't
// cover it — os.MkdirAll only applies its mode to directories it creates.
func warnUnsafeStore(stderr io.Writer) {
	if unsafe, mode, dir, err := trust.UnsafeStorePermissions(); err == nil && unsafe {
		fprintf(stderr, "envoke: warning: the trust store %s is writable by group/other (mode %o) -- anyone who can write there can forge an approval; run `chmod go-w %s`\n", dir, mode, dir)
	}
}

func usage(w io.Writer) {
	fprintln(w, raw(`envoke - run shell scripts when you cd into or out of a directory

Usage:
  envoke version                                     print version, commit, build date, and Go/OS/arch info, then exit
  envoke help                                        print this usage text and exit
  envoke shell-init [<shell>]                        print shell hook code to eval/source (bash|zsh|fish|tcsh|powershell; guessed from $SHELL if omitted)
  envoke completion [<shell>]                        print a tab-completion script (bash|zsh|fish; guessed from $SHELL if omitted)
  envoke allow [--yes|-y] [path]                     trust a config after reviewing and confirming it (default: every config envoke would load; --yes/-y skips the y/N prompt)
  envoke revoke [path]                               withdraw trust for a config (default: every config envoke would load)
  envoke list                                        reconcile the configs envoke would load with the trust store: each one's status, then any records left over
  envoke prune                                       drop trust records whose config no longer exists
  envoke disable                                     stop running blocks, in every shell, until enable
  envoke enable                                      undo disable (set ENVOKE_DISABLE=1 or =0 to override either one for a single shell)
  envoke reload [--shell <name>]                     re-apply the enter blocks for the current directory: eval "$(envoke reload)"
  envoke exec [<from> [<to>]]                        run the blocks matching a directory change, each in its own subprocess (for scripts/CI, not your interactive shell)
  envoke debug [<from> [<to>]]                       print which blocks would fire for a directory change, without running them
  envoke shell-hook [--shell <name>] <from> <to>     run blocks matching a directory change (internal, called by the shell hook; <from>/<to> may also come from $ENVOKE_FROM/$ENVOKE_TO)

exec and debug accept relative paths. <to> defaults to the directory you are
in; <from> defaults to $OLDPWD, which only POSIX shells set -- in PowerShell,
name the directory you came from: envoke debug <from>

Blocks come from your central config ($ENVOKERC, ~/.envokerc or
$XDG_CONFIG_HOME/envoke/config) plus every file in the envokerc.d directory
($ENVOKERC_D, ~/.envokerc.d or $XDG_CONFIG_HOME/envoke/envokerc.d), applied in
order of each file's path relative to that directory. A fragment may be a
symlink to a config committed inside a
project: its "./"-relative patterns then resolve against that project, and it
may only match inside it. Each file is trusted separately.`))
}

// Command envoke runs shell scripts when you cd into or out of a directory.
//
// Nothing here executes a block from a config that hasn't been through
// `envoke allow` since its last edit — see CLAUDE.md's non-negotiable
// trust-before-execution principle. The two commands that can run anything,
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

	"github.com/Neirda24/envoke/internal/config"
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

// fprintf, fprintln and fprint wrap the fmt equivalents and drop the error.
// Every write here goes to the user's terminal, where a failed write is not
// something a CLI can report or recover from; without them errcheck demands
// `_, _ =` on all hundred-odd call sites, which buries the logic.
func fprintf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
func fprintln(w io.Writer, a ...any)               { _, _ = fmt.Fprintln(w, a...) }
func fprint(w io.Writer, a ...any)                 { _, _ = fmt.Fprint(w, a...) }

// run dispatches a subcommand, writing to the given streams instead of
// os.Stdout/os.Stderr directly so it can be exercised in tests without
// capturing process output. stdin is threaded through for the one
// subcommand that reads it (allow's confirmation prompt) rather than having
// every subcommand accept a reader it ignores.
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
// (flag.ContinueOnError) and never prints Go's default usage dump, so run
// stays a plain function returning an exit code and every subcommand can
// print its own one-line usage in envoke's own voice.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("envoke "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseFlags parses args, printing usage on stderr and reporting ok=false
// when they're malformed. `-h`/`--help` is treated as a request, not an
// error, but still returns ok=false so the caller stops -- the exit code
// differs, which is why help is reported separately.
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

// printVersion prints the version, commit, and build date (as injected by
// goreleaser's ldflags -- see .goreleaser.yaml -- or "dev"/"unknown" for a
// local `go build`/`go test`, which never sets them) plus the Go toolchain
// and OS/arch the binary was built with, on two lines.
func printVersion(stdout io.Writer) {
	fprintf(stdout, "envoke %s (commit %s, built %s)\n", version, commit, date)
	fprintf(stdout, "%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

const allowUsage = "envoke allow [--yes|-y] [path]"

const shellInitUsage = "envoke shell-init [bash|zsh|fish|tcsh|powershell]  (defaults to the shell named by $SHELL)"

// detectShell maps a $SHELL value (a path like /usr/local/bin/fish) to one
// of the supported shell names. It only ever looks at the basename, so a
// shell installed anywhere works, and it deliberately does not fall back to
// a default: silently emitting a bash hook for an unrecognised shell would
// produce a broken rc file whose breakage surfaces much later, and much
// less clearly, than a message right here.
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
// the same reason the hooks are: a per-shell script maintained by hand
// drifts from the real subcommand list the moment anyone adds one, silently.
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
	fprint(stdout, script)
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
		// Guessing from $SHELL removes the single most common install
		// mistake -- pasting the bash line into a zsh rc -- and costs
		// nothing, since an unrecognised guess still produces the same
		// explicit error as typing the name wrong.
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
	fprint(stdout, script)
	return 0
}

// cmdShellHook is invoked by the generated hook on every directory change.
// If the matched config is trusted it prints executor.Render's output for
// the shell to eval; otherwise it reports the match on stderr and prints
// nothing, so evaluating the empty output stays a safe no-op.
//
// Omitting --shell defaults to the POSIX profile, which is what bash's and
// zsh's hooks rely on.
//
// The directories come as positional arguments, or from ENVOKE_FROM and
// ENVOKE_TO when there are none. The environment form exists for the tcsh
// hook, whose only route into `source` is an `eval`: interpolating directory
// names into a string that gets re-parsed is a command-injection hole (see
// internal/shellinit's tcshHook).
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

	// Checked before anything else, and silently: this runs on every
	// directory change, so a switched-off envoke has to cost one stat and
	// say nothing at all. `envoke debug` is where the state is reported.
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

	path, found, err := config.Locate()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		return 0
	}

	warnUnsafePermissions(stderr, path)

	// One read, reused for both the parse and the trust check: the bytes
	// that get rendered into the caller's shell must be the same bytes the
	// hash was computed over (see config.LoadFile).
	cfg, content, err := config.LoadFile(path)
	if err != nil {
		// $ENVOKERC is honoured verbatim, so pointing it at a file you
		// haven't written yet is ordinary — and this runs on every cd, so
		// reporting it would print on every one until the file appears.
		// Anything else stays loud: a config that exists and doesn't parse
		// is not doing what its owner thinks it is.
		if errors.Is(err, fs.ErrNotExist) {
			return 0
		}
		fprintln(stderr, "envoke:", err)
		return 1
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	total := len(leaves) + len(enters)
	if total == 0 {
		return 0
	}

	trusted, err := trust.IsTrusted(path, content)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !trusted {
		fprintf(stderr, "envoke: %d block(s) matched for %s -> %s but %s is not trusted: run `envoke allow %s`\n", total, from, to, path, path)
		return 0
	}

	fprint(stdout, executor.Render(*shell, leaves, enters))
	return 0
}

// resolveConfigPath turns a subcommand's leftover positional arguments into
// the config path to act on: the one given, or the located config when none
// is. Shared by allow and revoke, which offer the same "default to the
// config envoke would use" convenience and must agree on what that means.
func resolveConfigPath(positional []string, cmd, usage string, stderr io.Writer) (path string, code int, ok bool) {
	switch len(positional) {
	case 0:
		p, found, err := config.Locate()
		if err != nil {
			fprintln(stderr, "envoke:", err)
			return "", 1, false
		}
		if !found {
			fprintf(stderr, "envoke: no config found (looked for %s); pass a path explicitly: envoke %s <path>\n", p, cmd)
			return "", 1, false
		}
		return p, 0, true
	case 1:
		return positional[0], 0, true
	default:
		fprintln(stderr, "usage:", usage)
		return "", 2, false
	}
}

// cmdAllow trusts a config file's current content, so shell-hook will run
// blocks matched against it until it is edited again.
//
// What it shows for review depends on whether there is a prior approval to
// compare against: the full block dump for a first-time trust, a +/- line
// diff when the file changed since the last one, and nothing at all when it
// didn't. The first two then ask for a y/N confirmation on stdin, which
// --yes/-y skips.
func cmdAllow(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	flags := newFlagSet("allow")
	yes := flags.Bool("yes", false, "skip the y/N confirmation prompt")
	flags.BoolVar(yes, "y", false, "shorthand for --yes")
	if ok, code := parseFlags(flags, args, allowUsage, stderr); !ok {
		return code
	}

	// stdlib flag stops at the first non-flag argument, so `envoke allow
	// <path> --yes` would otherwise read --yes as a second path. Picking out
	// this one boolean rather than reimplementing flags-anywhere keeps a
	// config genuinely named `--yes` approvable as `./--yes`.
	var positional []string
	for _, a := range flags.Args() {
		if a == "--yes" || a == "-y" {
			*yes = true
			continue
		}
		positional = append(positional, a)
	}

	path, code, ok := resolveConfigPath(positional, "allow", allowUsage, stderr)
	if !ok {
		return code
	}

	warnUnsafePermissions(stderr, path)

	// One read feeds the parse, the review and the trust record alike.
	// Reading again at any of those steps would record a hash for content
	// the user was never shown, so an edit landing between the diff and the
	// y/N answer would be approved sight-unseen (see config.LoadFile).
	cfg, current, err := config.LoadFile(path)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	previous, hadPrevious, err := trust.PreviousContent(path)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	alreadyTrusted, err := trust.IsTrusted(path, current)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	if hadPrevious && alreadyTrusted && previous == string(current) {
		// Nothing changed, so there is nothing to review: asking the user to
		// confirm a config they already approved verbatim is busywork, and
		// --yes has no prompt to skip here either.
		//
		// alreadyTrusted is tested on top of the content comparison so a
		// half-written record (content copy landed, hash record didn't) can't
		// wedge this into reporting an untrusted config as trusted forever.
		// In that state it falls through and gets re-approved normally.
		fprintf(stdout, "envoke: %s is unchanged since it was last trusted -- nothing to review\n", path)
		return 0
	}

	if hadPrevious && canDiff(previous, string(current)) {
		printDiff(stdout, path, previous, string(current))
	} else {
		printBlocksForReview(stdout, path, cfg.Blocks)
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

	if err := trust.Allow(path, current); err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	fprintf(stdout, "envoke: trusted %s\n", path)
	// allow is a child process and cannot export into the shell that ran it,
	// so what was just approved applies from the next cd on. Naming the way
	// out here is the only place a user is guaranteed to be looking.
	fprintln(stdout, `envoke: to apply it to this shell without leaving the directory: eval "$(envoke reload)"`)
	return 0
}

// printBlocksForReview dumps every block's pattern, line and script body
// before it is trusted, so approving a config means seeing the code that
// will run on every matching cd rather than a hash being recorded silently.
// Used for a first-time trust; see printDiff for the re-approval case.
func printBlocksForReview(stdout io.Writer, path string, blocks []config.Block) {
	fprintf(stdout, "envoke: about to trust %s -- review each block below before confirming:\n\n", path)
	if len(blocks) == 0 {
		fprintln(stdout, "  (no blocks defined)")
		fprintln(stdout)
		return
	}
	for _, b := range blocks {
		fprintf(stdout, "  %s %s (line %d)\n", b.Type, b.RawPattern, b.Line)
		for _, line := range strings.Split(b.Script, "\n") {
			fprintf(stdout, "    %s\n", line)
		}
		fprintln(stdout)
	}
}

// printDiff replaces printBlocksForReview's full dump when there is a prior
// approval to compare against, so re-approving after a small edit doesn't
// mean re-reading the whole file to spot what changed.
func printDiff(stdout io.Writer, path, previous, current string) {
	fprintf(stdout, "envoke: %s changed since it was last trusted -- here's what's different:\n\n", path)
	for _, line := range diffLines(splitLines(previous), splitLines(current)) {
		fprintln(stdout, line)
	}
	fprintln(stdout)
}

// splitLines drops a trailing carriage return the way the parser's scanner
// does, so the diff shows the same lines the review dump does and a CRLF
// config doesn't print stray \r at every line end.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// diffLines returns only the lines that differ between old and new, prefixed
// "- " and "+ " as `diff -u` does. Common lines are aligned via a longest
// common subsequence and omitted rather than printed as context, so an edit
// touching one block doesn't drag the rest of the config into the output.
//
// A plain O(len(old)*len(new)) DP table rather than Myers: diffCap keeps the
// inputs small, and this needs nothing beyond the stdlib.
func diffLines(oldLines, newLines []string) []string {
	n, m := len(oldLines), len(newLines)

	// lcs is the (n+1)x(m+1) table flattened into one allocation:
	// lcs[i*stride+j] is the LCS length of oldLines[i:] and newLines[j:].
	// One contiguous block keeps the inner loop walking memory in order, and
	// int32 halves its footprint -- the values are bounded by the line count,
	// which diffCap keeps well inside it.
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

// diffCap bounds the LCS table. The algorithm is O(n*m) in both time and
// memory, and while "config files are small" is true of every config anyone
// would write by hand, nothing enforces it -- the parser accepts any number
// of lines, and a config that grew a large generated block (or simply had
// something appended to it) would make `envoke allow` allocate a table
// quadratic in its size. At this cap the table is 2000*2000 int32 = 16 MiB,
// which is the most an interactive approval prompt has any business
// reserving. Beyond it, cmdAllow shows the full block dump instead: less
// convenient, but bounded and never wrong.
const diffCap = 2000

// canDiff reports whether a line-level diff is worth attempting between two
// config versions, given diffCap.
func canDiff(previous, current string) bool {
	return strings.Count(previous, "\n") < diffCap && strings.Count(current, "\n") < diffCap
}

const revokeUsage = "envoke revoke [path]"

// cmdRevoke withdraws trust for a config. Without it the only ways back are
// editing the config, which revokes trust as a side effect of something
// else, or deleting a sha256-named file out of the data home by hand.
func cmdRevoke(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("revoke")
	if ok, code := parseFlags(flags, args, revokeUsage, stderr); !ok {
		return code
	}

	path, code, ok := resolveConfigPath(flags.Args(), "revoke", revokeUsage, stderr)
	if !ok {
		return code
	}

	found, err := trust.Revoke(path)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		// Not an error: the requested end state already holds.
		fprintf(stdout, "envoke: %s was not trusted -- nothing to revoke\n", path)
		return 0
	}
	fprintf(stdout, "envoke: revoked trust for %s\n", path)
	return 0
}

const listUsage = "envoke list"

// cmdList shows what the trust store actually holds. Beyond "which configs
// have I approved", it is the only way to notice that the store keeps a
// plaintext copy of every approved config — which routinely means secrets,
// since exporting project-scoped API keys is one of envoke's headline uses.
func cmdList(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("list")
	if ok, code := parseFlags(flags, args, listUsage, stderr); !ok {
		return code
	}
	if flags.NArg() != 0 {
		fprintln(stderr, "usage:", listUsage)
		return 2
	}

	records, err := trust.List()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if len(records) == 0 {
		fprintln(stdout, "envoke: no configs are trusted")
		return 0
	}

	for _, r := range records {
		if r.ConfigPath == "" {
			fprintf(stdout, "  %-9s <unknown path, approved by an older envoke> (%s)\n", "unknown", r.StorePath)
			continue
		}
		fprintf(stdout, "  %-9s %s\n", listStatus(r), r.ConfigPath)
	}
	return 0
}

// listStatus classifies a record against the config as it exists now:
// whether shell-hook would currently act on it, and if not, why.
//
// This is the one place that reads a config outside config.LoadFile, and
// the read-once rule it appears to break does not apply here. That rule
// exists so the bytes that get *executed* are the bytes that were approved;
// list executes nothing and renders nothing for a shell to eval. The worst
// a file changing mid-listing can do is print a status that was true a
// moment earlier — which is all any status report can ever promise.
// Parsing the file would be strictly worse: an unparseable config is still
// a trusted record worth listing.
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

// cmdPrune drops records whose config no longer exists. Those records are
// dead weight that also keeps a plaintext copy of a config the user has
// already deleted, which is the part that actually matters.
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
// case `envoke allow` cannot cover: allow runs as a child of your shell and
// cannot export anything into it, so a freshly approved config only takes
// effect on the next cd. Rather than cd .. && cd -, this prints the same
// shell text the hook does, for the caller to eval.
//
// Only enter blocks, and no unwinding of what a previous version of the
// config set: nothing has been left, and envoke does not snapshot state to
// restore later.
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

	path, found, err := config.Locate()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		fprintf(stderr, "envoke: no config found (looked for %s)\n", path)
		return 1
	}

	warnUnsafePermissions(stderr, path)

	cfg, content, err := config.LoadFile(path)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	trusted, err := trust.IsTrusted(path, content)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !trusted {
		// Louder than shell-hook's equivalent, which merely notes it: this
		// was typed, so doing nothing has to be an error rather than a
		// no-op the user might not notice.
		fprintf(stderr, "envoke: %s is not trusted: run `envoke allow %s`\n", path, path)
		return 1
	}

	enters, err := matcher.Enters(cfg, dir)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	fprint(stdout, executor.Render(*shell, nil, enters))
	return 0
}

// currentDir prefers $PWD over os.Getwd for the same reason the hooks pass
// the shell's own $PWD: through a symlinked directory the two disagree, and
// the patterns a user writes describe the path they cd'd through.
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

// cmdSwitch backs both `envoke disable` and `envoke enable`. The two are the
// same operation with opposite arguments, and splitting them into separate
// functions would mean duplicating the part that actually matters: telling
// the user when $ENVOKE_DISABLE is going to override what they just asked
// for, so a `disable` that appears to do nothing has a visible reason.
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
// generated arguments, so both accept relative paths and default to the
// shell's own last transition.
func transitionArgs(args []string) (from, to string, err error) {
	switch len(args) {
	case 0:
		from, to = os.Getenv("OLDPWD"), os.Getenv("PWD")
		if from == "" || to == "" {
			return "", "", errors.New("$OLDPWD/$PWD are not both set, so there's no transition to infer -- pass <from> and <to>")
		}
	case 2:
		from, to = args[0], args[1]
	default:
		return "", "", fmt.Errorf("expected either no arguments or exactly two, got %d", len(args))
	}

	if from, err = filepath.Abs(from); err != nil {
		return "", "", err
	}
	if to, err = filepath.Abs(to); err != nil {
		return "", "", err
	}
	return from, to, nil
}

const execUsage = "envoke exec [<from> <to>]  (defaults to $OLDPWD -> $PWD)"

// cmdExec runs the matching blocks for non-interactive callers — scripts,
// Makefiles, CI — that want a project's enter hooks to have run without an
// interactive shell to hook into.
//
// Each block runs in its own `sh -c`, so export, source and cd inside one
// affect that subprocess and nothing else. Anything meant to change the
// caller's own shell needs the generated hook instead.
//
// Trust is enforced inside envoke.Transition rather than here, so no future
// caller of that package can forget it.

func cmdExec(args []string, stderr io.Writer) int {
	flags := newFlagSet("exec")
	if ok, code := parseFlags(flags, args, execUsage, stderr); !ok {
		return code
	}
	from, to, err := transitionArgs(flags.Args())
	if err != nil {
		fprintln(stderr, "envoke:", err)
		fprintln(stderr, "usage:", execUsage)
		return 2
	}

	// Unlike shell-hook, this says so out loud. exec is called deliberately,
	// usually from a script, and silently running nothing is how a build
	// ends up mysteriously missing half its environment. Still exit 0: the
	// user asked for envoke to be off, that isn't a failure.
	if disabled, source, err := state.Disabled(); err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	} else if disabled {
		fprintf(stderr, "envoke: disabled by %s -- no blocks were run\n", source)
		return 0
	}

	path, found, err := config.Locate()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		fprintf(stderr, "envoke: no config found (looked for %s)\n", path)
		return 1
	}

	warnUnsafePermissions(stderr, path)

	// Without this, Go's default handling terminates envoke on the spot and
	// leaves the block's `sh` running — visible when exec is driven from a
	// script or CI runner that sends SIGTERM. Cancelling the context instead
	// interrupts the script and gives it killGrace to clean up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := envoke.Transition(ctx, path, from, to); err != nil {
		if ctx.Err() != nil {
			// Conventional exit status for "died from a signal", and the
			// signal is not news to whoever sent it.
			return 130
		}
		if errors.Is(err, envoke.ErrUntrusted) {
			fprintf(stderr, "envoke: %v: run `envoke allow %s`\n", err, path)
			return 1
		}
		fprintln(stderr, "envoke:", err)
		return 1
	}
	return 0
}

// cmdDebug prints which enter/leave blocks would fire for a directory
// transition, without running them and regardless of trust — a dry-run
// diagnostic for developing a config without surprises (see README's
// Diagnostics section). It does note whether the config is currently
// trusted, since that determines whether shell-hook would actually run
// what's listed here.
const debugUsage = "envoke debug [<from> <to>]  (defaults to $OLDPWD -> $PWD)"

func cmdDebug(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("debug")
	if ok, code := parseFlags(flags, args, debugUsage, stderr); !ok {
		return code
	}
	from, to, err := transitionArgs(flags.Args())
	if err != nil {
		fprintln(stderr, "envoke:", err)
		fprintln(stderr, "usage:", debugUsage)
		return 2
	}

	path, found, err := config.Locate()
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		fprintf(stderr, "envoke: no config found (looked for %s)\n", path)
		return 1
	}

	warnUnsafePermissions(stderr, path)

	cfg, content, err := config.LoadFile(path)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}

	trusted, err := trust.IsTrusted(path, content)
	if err != nil {
		fprintln(stderr, "envoke:", err)
		return 1
	}
	trustNote := "trusted"
	if !trusted {
		trustNote = fmt.Sprintf("NOT trusted -- run `envoke allow %s` before these would actually run", path)
	}

	fprintf(stdout, "envoke debug: %s -> %s using %s (%s)\n", from, to, path, trustNote)

	// debug never executes anything, so being switched off doesn't stop it —
	// but it does change whether what follows would happen for real, exactly
	// like the trust state above.
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
		return 0
	}
	printWorkingDirNote(stdout, to, leaves, enters)
	for _, m := range leaves {
		fprintf(stdout, "  %s %s (line %d: %s)\n", m.Block.Type, m.Dir, m.Block.Line, m.Block.RawPattern)
		printIndentedScript(stdout, m.Block.Script)
	}
	for _, m := range enters {
		fprintf(stdout, "  %s %s (line %d: %s)\n", m.Block.Type, m.Dir, m.Block.Line, m.Block.RawPattern)
		printIndentedScript(stdout, m.Block.Script)
	}
	return 0
}

// printWorkingDirNote spells out where a matched script actually runs,
// whenever that is not the directory it matched.
//
// debug lists the matched directory next to each block, which reads as
// though relative paths in the script resolve from there. That holds for
// exec, which sets the working directory to the match, and not for the
// hook, which eval's the block in the shell that has already landed
// somewhere else.
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

// printIndentedScript prints a block's script body indented further than the
// summary line above it, so `envoke debug` shows the actual code that would
// run, not just metadata about the match.
func printIndentedScript(stdout io.Writer, script string) {
	for _, line := range strings.Split(script, "\n") {
		fprintf(stdout, "    %s\n", line)
	}
}

// warnUnsafePermissions warns, without ever blocking, when a config is
// writable by group or other. Content-hash revocation already stops a
// silently-modified config from running unapproved; this flags the
// permissions that make such tampering possible to begin with. A Stat
// failure is ignored: the caller's own load already handles a missing or
// unreadable file.
func warnUnsafePermissions(stderr io.Writer, path string) {
	if unsafe, mode, err := config.UnsafePermissions(path); err == nil && unsafe {
		fprintf(stderr, "envoke: warning: %s is writable by group/other (mode %o) -- consider tightening its permissions\n", path, mode)
	}

	// The store matters more than the config it describes: a writable store
	// lets someone forge an approval outright, rather than merely tamper
	// with a config whose next edit would revoke its own trust. Allow's
	// 0o700 doesn't cover it — os.MkdirAll only applies its mode to
	// directories it actually creates.
	if unsafe, mode, dir, err := trust.UnsafeStorePermissions(); err == nil && unsafe {
		fprintf(stderr, "envoke: warning: the trust store %s is writable by group/other (mode %o) -- anyone who can write there can forge an approval; run `chmod go-w %s`\n", dir, mode, dir)
	}
}

func usage(w io.Writer) {
	fprintln(w, `envoke - run shell scripts when you cd into or out of a directory

Usage:
  envoke version                                     print version, commit, build date, and Go/OS/arch info, then exit
  envoke shell-init [<shell>]                        print shell hook code to eval/source (bash|zsh|fish|tcsh|powershell; guessed from $SHELL if omitted)
  envoke completion [<shell>]                        print a tab-completion script (bash|zsh|fish; guessed from $SHELL if omitted)
  envoke allow [--yes|-y] [path]                     trust a config file after reviewing and confirming it (default: the located config; --yes/-y skips the y/N prompt)
  envoke revoke [path]                               withdraw trust for a config (default: the located config)
  envoke list                                        list every trusted config and whether its current content still matches
  envoke prune                                       drop trust records whose config no longer exists
  envoke disable                                     stop running blocks, in every shell, until enable
  envoke enable                                      undo disable (set ENVOKE_DISABLE=1 or =0 to override either one for a single shell)
  envoke reload [--shell <name>]                     re-apply the enter blocks for the current directory: eval "$(envoke reload)"
  envoke exec [<from> <to>]                          run the blocks matching a directory change, each in its own subprocess (for scripts/CI, not your interactive shell)
  envoke debug [<from> <to>]                         print which blocks would fire for a directory change, without running them
  envoke shell-hook [--shell <name>] <from> <to>     run blocks matching a directory change (internal, called by the shell hook; <from>/<to> may also come from $ENVOKE_FROM/$ENVOKE_TO)

exec and debug default to $OLDPWD -> $PWD, and accept relative paths.`)
}

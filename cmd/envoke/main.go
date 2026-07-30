// Command envoke runs shell scripts when you cd into or out of a directory.
//
// The matching engine, shell hook plumbing, config trust, and `envoke debug`
// dry-run diagnostics are all implemented. `envoke shell-hook` only ever
// executes blocks from a config that has been through `envoke allow` since
// its last edit — see CLAUDE.md's non-negotiable trust-before-execution
// principle. Fish/tcsh/powershell integration and packaging are later MVP
// steps — see CLAUDE.md's MVP scope order.
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
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/envoke"
	"github.com/Neirda24/envoke/internal/executor"
	"github.com/Neirda24/envoke/internal/matcher"
	"github.com/Neirda24/envoke/internal/shellinit"
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
	case "exec":
		return cmdExec(args[1:], stderr)
	case "debug":
		return cmdDebug(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "envoke: unknown command %q\n\n", args[0])
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
			_, _ = fmt.Fprintln(stderr, "usage:", usage)
			return false, 0
		}
		_, _ = fmt.Fprintf(stderr, "envoke: %v\nusage: %s\n", err, usage)
		return false, 2
	}
	return true, 0
}

// printVersion prints the version, commit, and build date (as injected by
// goreleaser's ldflags -- see .goreleaser.yaml -- or "dev"/"unknown" for a
// local `go build`/`go test`, which never sets them) plus the Go toolchain
// and OS/arch the binary was built with, on two lines.
func printVersion(stdout io.Writer) {
	_, _ = fmt.Fprintf(stdout, "envoke %s (commit %s, built %s)\n", version, commit, date)
	_, _ = fmt.Fprintf(stdout, "%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
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
			_, _ = fmt.Fprintln(stderr, "envoke:", err)
			_, _ = fmt.Fprintln(stderr, "usage:", shellInitUsage)
			return 2
		}
	case 1:
		shell = flags.Arg(0)
	default:
		_, _ = fmt.Fprintln(stderr, "usage:", shellInitUsage)
		return 2
	}

	script, err := shellinit.Generate(shell)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	_, _ = fmt.Fprint(stdout, script)
	return 0
}

// cmdShellHook is invoked by the generated shell hook on every directory
// change. If the config that matched has been allowed (see cmdAllow) and
// its content hasn't changed since, it prints executor.Render's output to
// stdout for the shell to eval/source — running the matched blocks in the
// caller's own shell process, which is what makes exported vars or
// `source`d scripts actually take effect. Otherwise it reports the match on
// stderr only and prints nothing, so evaluating the (empty) output stays a
// safe no-op.
//
// --shell <name> tells Render which export syntax to speak (bash/zsh/fish/
// tcsh/powershell); omitting it (as bash's and zsh's generated hooks do)
// defaults to the POSIX profile, so existing installs keep working
// unchanged.
//
// The two directories may be supplied either as positional arguments or,
// when no positional arguments are given at all, via the ENVOKE_FROM and
// ENVOKE_TO environment variables. The environment form exists for the tcsh
// hook, whose only way to pipe into `source` is through an `eval` — and
// interpolating directory names into a string that gets re-parsed is a
// command-injection hole (see internal/shellinit's tcshHook comment). Every
// other shell's hook passes them positionally.
const shellHookUsage = "envoke shell-hook [--shell <name>] [--] <from> <to>  (or set ENVOKE_FROM/ENVOKE_TO)"

func cmdShellHook(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("shell-hook")
	shell := flags.String("shell", "", "shell dialect to render for (bash, zsh, fish, tcsh, powershell)")
	if ok, code := parseFlags(flags, args, shellHookUsage, stderr); !ok {
		return code
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
			_, _ = fmt.Fprintln(stderr, "usage:", shellHookUsage)
			return 2
		}
	default:
		_, _ = fmt.Fprintln(stderr, "usage:", shellHookUsage)
		return 2
	}

	path, found, err := config.Locate()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
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
		// A config path that doesn't exist is a normal state, not a
		// failure: $ENVOKERC is honoured verbatim (see config.Locate), so
		// pointing it at a file you haven't written yet is ordinary. This
		// runs on every single directory change, so reporting that as an
		// error would print a message on every `cd` until the file appears.
		// Anything else — a parse error, a permission problem — stays loud,
		// because it means a config that exists is not doing what its owner
		// thinks it is.
		if errors.Is(err, fs.ErrNotExist) {
			return 0
		}
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	total := len(leaves) + len(enters)
	if total == 0 {
		return 0
	}

	trusted, err := trust.IsTrusted(path, content)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !trusted {
		_, _ = fmt.Fprintf(stderr, "envoke: %d block(s) matched for %s -> %s but %s is not trusted: run `envoke allow %s`\n", total, from, to, path, path)
		return 0
	}

	_, _ = fmt.Fprint(stdout, executor.Render(*shell, leaves, enters))
	return 0
}

// cmdAllow trusts a config file's current content, so shell-hook will run
// blocks matched against it from now on until it's edited again. With no
// path argument it trusts the config found by config.Locate.
//
// What gets shown for review depends on whether there's a prior approval to
// compare against (trust.PreviousContent):
//   - No prior approval: the full block dump (printBlocksForReview), same as
//     always -- there's nothing to diff against, so a full read-through is
//     the right thing for a first-time trust.
//   - Prior approval, content unchanged: nothing to review at all. Reports
//     that the config is already trusted and returns immediately, without
//     prompting or touching the trust record again -- see the comment at the
//     call site for why.
//   - Prior approval, content changed: a labeled +/- line diff
//     (printDiff/diffLines) against the previously-approved content, instead
//     of the full dump -- this is the actual point of the feature: reviewing
//     a small edit to an already-trusted config shouldn't require re-reading
//     the whole file.
//
// In the first and third cases, this then reads a y/N confirmation from
// stdin by default, and aborts (without calling trust.Allow) on anything
// other than "y"/"yes" (case-insensitive), including empty input or EOF.
// --yes/-y skips the prompt entirely, for scripts/CI. The flag may appear
// anywhere in args, before or after the optional path.
func cmdAllow(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	flags := newFlagSet("allow")
	yes := flags.Bool("yes", false, "skip the y/N confirmation prompt")
	flags.BoolVar(yes, "y", false, "shorthand for --yes")
	if ok, code := parseFlags(flags, args, allowUsage, stderr); !ok {
		return code
	}

	// stdlib flag stops at the first non-flag argument, but `envoke allow
	// <path> --yes` was documented and released as working, so the trailing
	// form keeps working rather than silently becoming a second path.
	// Deliberately just this one boolean rather than a general
	// flags-anywhere reimplementation: a config file genuinely named
	// `--yes` can still be approved as `./--yes`.
	var positional []string
	for _, a := range flags.Args() {
		if a == "--yes" || a == "-y" {
			*yes = true
			continue
		}
		positional = append(positional, a)
	}

	var path string
	switch len(positional) {
	case 0:
		p, found, err := config.Locate()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "envoke:", err)
			return 1
		}
		if !found {
			_, _ = fmt.Fprintf(stderr, "envoke: no config found (looked for %s); pass a path explicitly: envoke allow <path>\n", p)
			return 1
		}
		path = p
	case 1:
		path = positional[0]
	default:
		_, _ = fmt.Fprintln(stderr, "usage:", allowUsage)
		return 2
	}

	warnUnsafePermissions(stderr, path)

	// The file is read exactly once here, and the same bytes are what gets
	// parsed, shown for review, and finally recorded as trusted. Reading it
	// again at any of those steps would mean approving content the user was
	// never shown -- an edit landing between the diff and the y/N answer
	// would be trusted sight-unseen (see config.LoadFile).
	cfg, current, err := config.LoadFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	previous, hadPrevious, err := trust.PreviousContent(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	alreadyTrusted, err := trust.IsTrusted(path, current)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	if hadPrevious && alreadyTrusted && previous == string(current) {
		// The file wasn't actually edited since it was last approved, and
		// the trust record agrees, so there's nothing to review and nothing
		// new to approve. Re-printing the full block dump (or even just
		// re-running the y/N prompt) would be pure busywork: the user would
		// be asked to confirm that nothing changed, for a config they
		// already reviewed and approved verbatim. Report the state honestly
		// and return without prompting or re-recording trust -- --yes is a
		// no-op here for the same reason: there was never a prompt in this
		// branch for it to skip.
		//
		// alreadyTrusted is checked on top of the content comparison so a
		// half-written trust record (content copy landed, hash record
		// didn't -- see trust.Allow's write ordering) can't wedge this into
		// reporting an untrusted config as trusted forever. In that state
		// the config falls through and gets re-approved normally.
		_, _ = fmt.Fprintf(stdout, "envoke: %s is unchanged since it was last trusted -- nothing to review\n", path)
		return 0
	}

	if hadPrevious && canDiff(previous, string(current)) {
		printDiff(stdout, path, previous, string(current))
	} else {
		printBlocksForReview(stdout, path, cfg.Blocks)
	}

	if !*yes {
		_, _ = fmt.Fprint(stdout, "envoke: trust and run these blocks on every matching cd? [y/N] ")
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			_, _ = fmt.Fprintln(stderr, "envoke: aborted, not trusted")
			return 1
		}
	}

	if err := trust.Allow(path, current); err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "envoke: trusted %s\n", path)
	return 0
}

// printBlocksForReview prints the full contents (type, pattern, line, and
// script body) of every block in cfg before it's trusted, so `envoke allow`
// can't be run as a blind habitual reflex (see CLAUDE.md's
// trust-before-execution principle and the audit this addresses): a user
// approving a config gets the actual code that will run on every matching
// `cd`, not just a hash getting recorded silently. Used for a first-time
// trust, where there's no prior approval to diff against (see printDiff for
// the re-allow-after-an-edit case).
func printBlocksForReview(stdout io.Writer, path string, blocks []config.Block) {
	_, _ = fmt.Fprintf(stdout, "envoke: about to trust %s -- review each block below before confirming:\n\n", path)
	if len(blocks) == 0 {
		_, _ = fmt.Fprintln(stdout, "  (no blocks defined)")
		_, _ = fmt.Fprintln(stdout)
		return
	}
	for _, b := range blocks {
		_, _ = fmt.Fprintf(stdout, "  %s %s (line %d)\n", b.Type, b.RawPattern, b.Line)
		for _, line := range strings.Split(b.Script, "\n") {
			_, _ = fmt.Fprintf(stdout, "    %s\n", line)
		}
		_, _ = fmt.Fprintln(stdout)
	}
}

// printDiff prints a labeled +/- line diff (see diffLines) between the
// previously-approved content and the config's current content, in place of
// printBlocksForReview's full dump, when there's a prior approval to compare
// against. Only the lines that actually changed are shown, so re-approving
// a config after a small edit doesn't require re-reading the entire file to
// spot what's different -- the whole point of this feature (see CLAUDE.md's
// Status entry on diff-on-allow).
func printDiff(stdout io.Writer, path, previous, current string) {
	_, _ = fmt.Fprintf(stdout, "envoke: %s changed since it was last trusted -- here's what's different:\n\n", path)
	for _, line := range diffLines(strings.Split(previous, "\n"), strings.Split(current, "\n")) {
		_, _ = fmt.Fprintln(stdout, line)
	}
	_, _ = fmt.Fprintln(stdout)
}

// diffLines computes a simple LCS-based (longest common subsequence) line
// diff between old and new, returning only the lines that differ, each
// prefixed "- " (present in old, removed) or "+ " (present in new, added)
// -- the same convention as `diff -u`/git's unified diff output, so it reads
// immediately without an explanation. Lines common to both are aligned via
// the LCS and omitted entirely rather than printed as context, so an edit
// that only touches one block doesn't drag the rest of an unchanged config
// into the output -- that's the actual problem this feature solves (see
// CLAUDE.md's Status entry). This is a classic O(len(old)*len(new)) DP
// table, not a full Myers diff: config files are small (a handful of
// blocks), so the simpler algorithm is both correct and fast enough, and
// needs no dependency beyond the stdlib slices/strings already in use here
// (see CLAUDE.md's zero-non-stdlib-dependency rule).
func diffLines(oldLines, newLines []string) []string {
	n, m := len(oldLines), len(newLines)

	// lcs is the (n+1)x(m+1) DP table flattened into one allocation:
	// lcs[i*(m+1)+j] is the length of the longest common subsequence of
	// oldLines[i:] and newLines[j:]. One contiguous block rather than n+1
	// separate row slices -- same asymptotics, one allocation instead of
	// n+2, and the inner loop walks memory in order. int32 because the
	// value is bounded by the line count, which diffCap keeps well inside
	// it, halving the table's footprint.
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

// cmdExec runs the blocks matching a directory change in a subprocess each,
// for non-interactive callers — scripts, Makefiles, CI — that want a
// project's enter hooks to have run without an interactive shell to hook
// into.
//
// This is emphatically not what the shell hook uses, and the distinction
// matters enough to be worth stating at the call site: each block runs in
// its own `sh -c` subprocess, so `export`, `source` and `cd` inside a block
// affect that subprocess and nothing else. Anything meant to change the
// caller's own shell needs `envoke shell-hook` via the generated hook.
//
// Trust is enforced inside envoke.Transition rather than here, so no future
// caller of that package can forget it.
const revokeUsage = "envoke revoke [path]"

// cmdRevoke is the counterpart `envoke allow` never had. Trust is the one
// thing envoke asks a user to grant deliberately, and until now the only
// ways to take it back were editing the config (which revokes it as a side
// effect of something else) or deleting a sha256-named file out of
// ~/.local/share by hand.
func cmdRevoke(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("revoke")
	if ok, code := parseFlags(flags, args, revokeUsage, stderr); !ok {
		return code
	}

	var path string
	switch flags.NArg() {
	case 0:
		p, found, err := config.Locate()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "envoke:", err)
			return 1
		}
		if !found {
			_, _ = fmt.Fprintf(stderr, "envoke: no config found (looked for %s); pass a path explicitly: envoke revoke <path>\n", p)
			return 1
		}
		path = p
	case 1:
		path = flags.Arg(0)
	default:
		_, _ = fmt.Fprintln(stderr, "usage:", revokeUsage)
		return 2
	}

	found, err := trust.Revoke(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		// Not an error: the requested end state already holds.
		_, _ = fmt.Fprintf(stdout, "envoke: %s was not trusted -- nothing to revoke\n", path)
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "envoke: revoked trust for %s\n", path)
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
		_, _ = fmt.Fprintln(stderr, "usage:", listUsage)
		return 2
	}

	records, err := trust.List()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if len(records) == 0 {
		_, _ = fmt.Fprintln(stdout, "envoke: no configs are trusted")
		return 0
	}

	for _, r := range records {
		if r.ConfigPath == "" {
			_, _ = fmt.Fprintf(stdout, "  %-9s <unknown path, approved by an older envoke> (%s)\n", "unknown", r.StorePath)
			continue
		}
		_, _ = fmt.Fprintf(stdout, "  %-9s %s\n", listStatus(r), r.ConfigPath)
	}
	return 0
}

// listStatus classifies a record against the config as it exists now:
// whether shell-hook would currently act on it, and if not, why.
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
		_, _ = fmt.Fprintln(stderr, "usage:", pruneUsage)
		return 2
	}

	removed, skipped, err := trust.Prune()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	for _, r := range removed {
		_, _ = fmt.Fprintf(stdout, "envoke: removed the trust record for %s (config no longer exists)\n", r.ConfigPath)
	}
	if len(removed) == 0 {
		_, _ = fmt.Fprintln(stdout, "envoke: nothing to prune")
	}
	if len(skipped) > 0 {
		_, _ = fmt.Fprintf(stderr, "envoke: %d record(s) approved by an older envoke have no recorded path and were left alone; remove them by hand or re-run `envoke allow` on those configs\n", len(skipped))
	}
	return 0
}

// transitionArgs resolves the <from> <to> pair shared by `exec` and
// `debug`. Both are things a human types, unlike shell-hook which is only
// ever called by generated code, so both accept relative paths and both
// default to the shell's own last transition ($OLDPWD -> $PWD) — typing out
// two absolute paths to ask "what would fire where I am right now?" was
// pure friction, and `envoke debug . /tmp` used to fail outright.
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

func cmdExec(args []string, stderr io.Writer) int {
	flags := newFlagSet("exec")
	if ok, code := parseFlags(flags, args, execUsage, stderr); !ok {
		return code
	}
	from, to, err := transitionArgs(flags.Args())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		_, _ = fmt.Fprintln(stderr, "usage:", execUsage)
		return 2
	}

	path, found, err := config.Locate()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		_, _ = fmt.Fprintf(stderr, "envoke: no config found (looked for %s)\n", path)
		return 1
	}

	warnUnsafePermissions(stderr, path)

	if err := envoke.Transition(context.Background(), path, from, to); err != nil {
		if errors.Is(err, envoke.ErrUntrusted) {
			_, _ = fmt.Fprintf(stderr, "envoke: %v: run `envoke allow %s`\n", err, path)
			return 1
		}
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
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
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		_, _ = fmt.Fprintln(stderr, "usage:", debugUsage)
		return 2
	}

	path, found, err := config.Locate()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		_, _ = fmt.Fprintf(stderr, "envoke: no config found (looked for %s)\n", path)
		return 1
	}

	warnUnsafePermissions(stderr, path)

	cfg, content, err := config.LoadFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	trusted, err := trust.IsTrusted(path, content)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	trustNote := "trusted"
	if !trusted {
		trustNote = fmt.Sprintf("NOT trusted -- run `envoke allow %s` before these would actually run", path)
	}

	_, _ = fmt.Fprintf(stdout, "envoke debug: %s -> %s using %s (%s)\n", from, to, path, trustNote)
	if len(leaves)+len(enters) == 0 {
		_, _ = fmt.Fprintln(stdout, "  no blocks would fire")
		return 0
	}
	for _, m := range leaves {
		_, _ = fmt.Fprintf(stdout, "  %s %s (line %d: %s)\n", m.Block.Type, m.Dir, m.Block.Line, m.Block.RawPattern)
		printIndentedScript(stdout, m.Block.Script)
	}
	for _, m := range enters {
		_, _ = fmt.Fprintf(stdout, "  %s %s (line %d: %s)\n", m.Block.Type, m.Dir, m.Block.Line, m.Block.RawPattern)
		printIndentedScript(stdout, m.Block.Script)
	}
	return 0
}

// printIndentedScript prints a block's script body indented further than the
// summary line above it, so `envoke debug` shows the actual code that would
// run, not just metadata about the match.
func printIndentedScript(stdout io.Writer, script string) {
	for _, line := range strings.Split(script, "\n") {
		_, _ = fmt.Fprintf(stdout, "    %s\n", line)
	}
}

// warnUnsafePermissions prints a non-fatal stderr warning if the config file
// at path is writable by group or other — relevant on shared homes, NFS, or
// multi-user machines, where such a file could be tampered with by someone
// other than its owner. internal/trust's content-hash revocation already
// stops a silently-modified config from running unapproved; this warns
// proactively about a config whose permissions make that tampering possible
// in the first place. Never blocks execution: a Stat failure or safe mode is
// silently ignored here, since the caller's own config.Locate/ParseFile call
// already handles a genuinely missing or unreadable file.
func warnUnsafePermissions(stderr io.Writer, path string) {
	if unsafe, mode, err := config.UnsafePermissions(path); err == nil && unsafe {
		_, _ = fmt.Fprintf(stderr, "envoke: warning: %s is writable by group/other (mode %o) -- consider tightening its permissions\n", path, mode)
	}

	// The store matters more than the config it describes: a writable store
	// lets someone forge an approval outright, rather than merely tamper
	// with a config whose next edit would revoke its own trust. It isn't
	// covered by Allow's 0o700, because os.MkdirAll only applies its mode to
	// directories it actually creates.
	if unsafe, mode, dir, err := trust.UnsafeStorePermissions(); err == nil && unsafe {
		_, _ = fmt.Fprintf(stderr, "envoke: warning: the trust store %s is writable by group/other (mode %o) -- anyone who can write there can forge an approval; run `chmod go-w %s`\n", dir, mode, dir)
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `envoke - run shell scripts when you cd into or out of a directory

Usage:
  envoke version                                     print version, commit, build date, and Go/OS/arch info, then exit
  envoke shell-init [<shell>]                        print shell hook code to eval/source (bash|zsh|fish|tcsh|powershell; guessed from $SHELL if omitted)
  envoke allow [--yes|-y] [path]                     trust a config file after reviewing and confirming it (default: the located config; --yes/-y skips the y/N prompt)
  envoke revoke [path]                               withdraw trust for a config (default: the located config)
  envoke list                                        list every trusted config and whether its current content still matches
  envoke prune                                       drop trust records whose config no longer exists
  envoke exec [<from> <to>]                          run the blocks matching a directory change, each in its own subprocess (for scripts/CI, not your interactive shell)
  envoke debug [<from> <to>]                         print which blocks would fire for a directory change, without running them
  envoke shell-hook [--shell <name>] <from> <to>     run blocks matching a directory change (internal, called by the shell hook; <from>/<to> may also come from $ENVOKE_FROM/$ENVOKE_TO)

exec and debug default to $OLDPWD -> $PWD, and accept relative paths.`)
}

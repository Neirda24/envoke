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
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/Neirda24/envoke/internal/config"
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
	case "version":
		printVersion(stdout)
		return 0
	case "shell-init":
		return cmdShellInit(args[1:], stdout, stderr)
	case "shell-hook":
		return cmdShellHook(args[1:], stdout, stderr)
	case "allow":
		return cmdAllow(args[1:], stdout, stderr, stdin)
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

// printVersion prints the version, commit, and build date (as injected by
// goreleaser's ldflags -- see .goreleaser.yaml -- or "dev"/"unknown" for a
// local `go build`/`go test`, which never sets them) plus the Go toolchain
// and OS/arch the binary was built with, on two lines.
func printVersion(stdout io.Writer) {
	_, _ = fmt.Fprintf(stdout, "envoke %s (commit %s, built %s)\n", version, commit, date)
	_, _ = fmt.Fprintf(stdout, "%s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func cmdShellInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: envoke shell-init <bash|zsh>")
		return 2
	}

	script, err := shellinit.Generate(args[0])
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
func cmdShellHook(args []string, stdout, stderr io.Writer) int {
	shell := ""
	if len(args) >= 2 && args[0] == "--shell" {
		shell = args[1]
		args = args[2:]
	}

	var from, to string
	switch {
	case len(args) == 2:
		from, to = args[0], args[1]
	case len(args) == 0:
		var ok bool
		from, ok = os.LookupEnv("ENVOKE_FROM")
		if ok {
			to, ok = os.LookupEnv("ENVOKE_TO")
		}
		if !ok {
			_, _ = fmt.Fprintln(stderr, "usage: envoke shell-hook [--shell <name>] <from> <to>  (or set ENVOKE_FROM/ENVOKE_TO)")
			return 2
		}
	default:
		_, _ = fmt.Fprintln(stderr, "usage: envoke shell-hook [--shell <name>] <from> <to>  (or set ENVOKE_FROM/ENVOKE_TO)")
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

	cfg, err := config.ParseFile(path)
	if err != nil {
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

	trusted, err := trust.IsTrusted(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !trusted {
		_, _ = fmt.Fprintf(stderr, "envoke: %d block(s) matched for %s -> %s but %s is not trusted: run `envoke allow %s`\n", total, from, to, path, path)
		return 0
	}

	_, _ = fmt.Fprint(stdout, executor.Render(shell, leaves, enters))
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
	yes := false
	var positional []string
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		default:
			positional = append(positional, a)
		}
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
		_, _ = fmt.Fprintln(stderr, "usage: envoke allow [--yes|-y] [path]")
		return 2
	}

	warnUnsafePermissions(stderr, path)

	cfg, err := config.ParseFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	current, err := os.ReadFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	previous, hadPrevious, err := trust.PreviousContent(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	if hadPrevious && previous == string(current) {
		// The file wasn't actually edited since it was last approved --
		// trust.IsTrusted already reports this config as trusted, so there's
		// nothing to review and nothing new to approve. Re-printing the full
		// block dump (or even just re-running the y/N prompt) would be pure
		// busywork: the user would be asked to confirm that nothing changed,
		// for a config they already reviewed and approved verbatim. Report
		// the state honestly and return without prompting or re-recording
		// trust -- --yes is a no-op here for the same reason: there was
		// never a prompt in this branch for it to skip.
		_, _ = fmt.Fprintf(stdout, "envoke: %s is unchanged since it was last trusted -- nothing to review\n", path)
		return 0
	}

	if hadPrevious {
		printDiff(stdout, path, previous, string(current))
	} else {
		printBlocksForReview(stdout, path, cfg.Blocks)
	}

	if !yes {
		_, _ = fmt.Fprint(stdout, "envoke: trust and run these blocks on every matching cd? [y/N] ")
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			_, _ = fmt.Fprintln(stderr, "envoke: aborted, not trusted")
			return 1
		}
	}

	if err := trust.Allow(path); err != nil {
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
	// lcs[i][j] = length of the longest common subsequence of
	// oldLines[i:] and newLines[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case oldLines[i] == newLines[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
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
		case lcs[i+1][j] >= lcs[i][j+1]:
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

// cmdDebug prints which enter/leave blocks would fire for a directory
// transition, without running them and regardless of trust — a dry-run
// diagnostic for developing a config without surprises (see README's
// Diagnostics section). It does note whether the config is currently
// trusted, since that determines whether shell-hook would actually run
// what's listed here.
func cmdDebug(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		_, _ = fmt.Fprintln(stderr, "usage: envoke debug <from> <to>")
		return 2
	}
	from, to := args[0], args[1]

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

	cfg, err := config.ParseFile(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	trusted, err := trust.IsTrusted(path)
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
	unsafe, mode, err := config.UnsafePermissions(path)
	if err != nil || !unsafe {
		return
	}
	_, _ = fmt.Fprintf(stderr, "envoke: warning: %s is writable by group/other (mode %o) -- consider tightening its permissions\n", path, mode)
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, `envoke - run shell scripts when you cd into or out of a directory

Usage:
  envoke version                                    print version, commit, build date, and Go/OS/arch info, then exit
  envoke shell-init <bash|zsh|fish|tcsh|powershell>  print shell hook code to eval/source
  envoke allow [--yes|-y] [path]                     trust a config file after reviewing and confirming it (default: the located config; --yes/-y skips the y/N prompt)
  envoke debug <from> <to>                           print which blocks would fire for a directory change, without running them
  envoke shell-hook [--shell <name>] <from> <to>      run blocks matching a directory change (internal, called by the shell hook; <from>/<to> may also come from $ENVOKE_FROM/$ENVOKE_TO)`)
}

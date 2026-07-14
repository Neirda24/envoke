// Command envoke runs shell scripts when you cd into or out of a directory.
//
// The matching engine, shell hook plumbing, and config trust are all
// implemented. `envoke shell-hook` only ever executes blocks from a config
// that has been through `envoke allow` since its last edit — see
// CLAUDE.md's non-negotiable trust-before-execution principle. Fish/tcsh/
// powershell integration, `envoke debug`, and packaging are later MVP
// steps — see CLAUDE.md's MVP scope order.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/executor"
	"github.com/Neirda24/envoke/internal/matcher"
	"github.com/Neirda24/envoke/internal/shellinit"
	"github.com/Neirda24/envoke/internal/trust"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a subcommand, writing to the given streams instead of
// os.Stdout/os.Stderr directly so it can be exercised in tests without
// capturing process output.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, "envoke "+version)
		return 0
	case "shell-init":
		return cmdShellInit(args[1:], stdout, stderr)
	case "shell-hook":
		return cmdShellHook(args[1:], stdout, stderr)
	case "allow":
		return cmdAllow(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "envoke: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func cmdShellInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: envoke shell-init <bash|zsh>")
		return 2
	}

	script, err := shellinit.Generate(args[0])
	if err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	fmt.Fprint(stdout, script)
	return 0
}

// cmdShellHook is invoked by the generated shell hook on every directory
// change. If the config that matched has been allowed (see cmdAllow) and
// its content hasn't changed since, it prints executor.Render's output to
// stdout for the shell to eval — running the matched blocks in the caller's
// own shell process, which is what makes exported vars or `source`d scripts
// actually take effect. Otherwise it reports the match on stderr only and
// prints nothing, so `eval "$(envoke shell-hook ...)"` stays a safe no-op.
func cmdShellHook(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: envoke shell-hook <from> <to>")
		return 2
	}
	from, to := args[0], args[1]

	path, found, err := config.Locate()
	if err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !found {
		return 0
	}

	cfg, err := config.ParseFile(path)
	if err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	leaves, enters, err := matcher.Resolve(cfg, from, to)
	if err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}

	total := len(leaves) + len(enters)
	if total == 0 {
		return 0
	}

	trusted, err := trust.IsTrusted(path)
	if err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if !trusted {
		fmt.Fprintf(stderr, "envoke: %d block(s) matched for %s -> %s but %s is not trusted: run `envoke allow %s`\n", total, from, to, path, path)
		return 0
	}

	fmt.Fprint(stdout, executor.Render(leaves, enters))
	return 0
}

// cmdAllow trusts a config file's current content, so shell-hook will run
// blocks matched against it from now on until it's edited again. With no
// argument it trusts the config found by config.Locate.
func cmdAllow(args []string, stdout, stderr io.Writer) int {
	var path string
	switch len(args) {
	case 0:
		p, found, err := config.Locate()
		if err != nil {
			fmt.Fprintln(stderr, "envoke:", err)
			return 1
		}
		if !found {
			fmt.Fprintf(stderr, "envoke: no config found (looked for %s); pass a path explicitly: envoke allow <path>\n", p)
			return 1
		}
		path = p
	case 1:
		path = args[0]
	default:
		fmt.Fprintln(stderr, "usage: envoke allow [path]")
		return 2
	}

	if _, err := config.ParseFile(path); err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	if err := trust.Allow(path); err != nil {
		fmt.Fprintln(stderr, "envoke:", err)
		return 1
	}
	fmt.Fprintf(stdout, "envoke: trusted %s\n", path)
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `envoke - run shell scripts when you cd into or out of a directory

Usage:
  envoke version                  print version and exit
  envoke shell-init <bash|zsh>    print shell hook code to eval/source
  envoke allow [path]             trust a config file (default: the located config)
  envoke shell-hook <from> <to>   run blocks matching a directory change (internal, called by the shell hook)`)
}

// Command envoke runs shell scripts when you cd into or out of a directory.
//
// Only the core matching engine and shell hook plumbing are implemented so
// far. `envoke shell-hook` deliberately never executes a matched block yet
// — there's no config trust mechanism (`envoke allow`) until a later MVP
// step, and CLAUDE.md's trust-before-execution principle is non-negotiable.
// See CLAUDE.md's MVP scope order.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/matcher"
	"github.com/Neirda24/envoke/internal/shellinit"
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
		return cmdShellHook(args[1:], stderr)
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
// change. It resolves which blocks match but does not run them yet (see the
// package doc comment) — it only reports the count on stderr so the
// scaffolding is observable while trust support is still unimplemented.
// Nothing is ever written to stdout, so `eval "$(envoke shell-hook ...)"` in
// the hook is always a safe no-op today.
func cmdShellHook(args []string, stderr io.Writer) int {
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

	if total := len(leaves) + len(enters); total > 0 {
		fmt.Fprintf(stderr, "envoke: %d block(s) matched for %s -> %s but were not run: config trust isn't implemented yet (envoke allow lands in a later MVP step)\n", total, from, to)
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `envoke - run shell scripts when you cd into or out of a directory

Status: core matching engine + shell hook plumbing. Matched blocks are
reported but not executed yet — config trust ("envoke allow") isn't
implemented.

Usage:
  envoke version                  print version and exit
  envoke shell-init <bash|zsh>    print shell hook code to eval/source
  envoke shell-hook <from> <to>   report blocks matching a directory change (internal, called by the shell hook)`)
}

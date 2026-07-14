// Command envoke runs shell scripts when you cd into or out of a directory.
//
// Only the core matching engine (config parser, path matcher, executor) is
// implemented so far. Shell integration, "envoke allow", and "envoke debug"
// land in later MVP steps — see CLAUDE.md's MVP scope order.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	flag.Usage = usage
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println("envoke " + version)
		return
	}

	usage()
	os.Exit(2)
}

func usage() {
	fmt.Fprintln(os.Stderr, `envoke - run shell scripts when you cd into or out of a directory

Status: core matching engine only (config parser, path matcher, executor).
Shell integration, "envoke allow", and "envoke debug" are not wired up yet.

Usage:
  envoke -version    print version and exit`)
}

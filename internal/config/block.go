// Package config parses envoke config files into a set of enter/leave
// blocks, each pairing a path pattern with a shell script body.
package config

import "regexp"

// BlockType distinguishes an enter block (fires when a matching directory is
// entered) from a leave block (fires when a matching directory is left).
type BlockType int

const (
	Enter BlockType = iota
	Leave
)

func (t BlockType) String() string {
	switch t {
	case Enter:
		return "enter"
	case Leave:
		return "leave"
	default:
		return "unknown"
	}
}

// Block is a single enter/leave rule: a compiled path pattern and the script
// to run when a directory matches it.
type Block struct {
	Type BlockType

	// Pattern is the compiled, anchored form of RawPattern: matching is
	// always a full match against a directory path, never a prefix or
	// substring match (see compilePattern).
	Pattern *regexp.Regexp

	// RawPattern is the pattern text as written in the config, before ~/env
	// expansion. Kept for diagnostics (e.g. `envoke debug`).
	RawPattern string

	// Script is the dedented script body, run via the shell on match.
	Script string

	// Line is the 1-indexed line number of the block header, for error
	// messages and diagnostics.
	Line int
}

// Config is a parsed envoke config file: an ordered list of blocks. Order is
// significant — blocks fire in declaration order when multiple blocks match
// the same directory.
type Config struct {
	// Path is the file this was parsed from, absolute, or "" when it was
	// parsed straight from a reader.
	Path string

	// Dir is the directory Path lives in. It is the base a `./`-relative
	// pattern resolves against, and — for a Local config — the subtree
	// outside which none of its blocks may match.
	Dir string

	// DirUnresolved reports that Dir could not be traced back to where the
	// file physically is: the resolution LoadFragmentResolved was handed was
	// empty, so the base fell back to the link's own directory, while the
	// read through it succeeded.
	//
	// It exists so the confinement decision can fail closed. Without it, a
	// fragment whose link could not be resolved looked exactly like a file
	// that really lives in envokerc.d — which is the one shape that is *not*
	// confined.
	DirUnresolved bool

	// Local confines this config's blocks to Dir's subtree: nothing in it can
	// match a directory outside the tree it belongs to, however its patterns
	// are written.
	//
	// Set for a fragment that is a symlink out of envokerc.d into a project,
	// because that file's content changes with the project — a `git pull` can
	// rewrite it, and while trust still gates every change, a config that
	// travels with a repository has no business matching /etc. Configs that
	// really live in your own config directory are not confined.
	Local bool

	Blocks []Block
}

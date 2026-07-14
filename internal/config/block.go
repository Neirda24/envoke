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
	// substring match (see expandPattern).
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
	Blocks []Block
}

package config

import (
	"bytes"
	"strings"
	"testing"
)

// fuzzBase stands in for the directory a config file lives in, so
// "./"-relative patterns compile rather than being rejected for want of a
// base — the substitution splices text into a regex, which is the part worth
// fuzzing.
const fuzzBase = "/home/fuzz.user/projects/app"

// FuzzParse fuzzes the hand-rolled config parser. It is the one component in
// envoke that consumes a whole file of unstructured text and turns it into
// something that decides what shell code runs, and it is hand-rolled by
// deliberate design choice rather than generated from a
// grammar — which is exactly the combination Go's native fuzzing exists for.
//
// The properties asserted are the ones the rest of the codebase relies on
// without re-checking:
//
//   - Parse never panics, whatever the input.
//   - It returns either a config or a positioned error, never both and never
//     neither.
//   - Every block it does return is usable: a compiled pattern, a non-empty
//     script, and a line number pointing into the input. cmd/envoke prints
//     b.Line to the user and executor dereferences b.Pattern, so a block
//     that satisfies neither would be a nil dereference or a lie in the
//     `envoke allow` review dump.
//
// Note that `go test` runs this against its seed corpus on every run, so the
// seeds double as ordinary regression cases even when nobody is fuzzing.
func FuzzParse(f *testing.F) {
	f.Add("enter /a\n    echo hi\n")
	f.Add("leave ~/Projects/([^/]+)\n\tdeactivate\n")
	f.Add("# comment only\n")
	f.Add("enter /a\n    echo one\n\n    echo two\n\nleave /a\n    echo three\n")
	f.Add("enter\n    echo missing-pattern\n")
	f.Add("enter /a\n")
	f.Add("    orphan indented line\n")
	f.Add("enter (unclosed\n    echo hi\n")
	f.Add("enter $NOPE/x\n    echo hi\n")
	f.Add("enter /a\r\n    echo crlf\r\n")
	f.Add("enter \x00/a\n    echo nul\n")
	f.Add("enter ./src\n    echo relative\n")
	f.Add("enter ../../x\n    echo up\n")

	f.Fuzz(func(t *testing.T, input string) {
		cfg, err := Parse(strings.NewReader(input), fuzzBase)

		if err != nil {
			if cfg != nil {
				t.Fatalf("Parse returned both a config and an error %v", err)
			}
			return
		}
		if cfg == nil {
			t.Fatalf("Parse returned neither a config nor an error")
		}

		for i, b := range cfg.Blocks {
			if b.Pattern == nil {
				t.Fatalf("block %d has a nil compiled pattern (input %q)", i, input)
			}
			if b.Script == "" {
				t.Fatalf("block %d has an empty script; the parser must reject a bodyless block (input %q)", i, input)
			}
			if b.Line < 1 {
				t.Fatalf("block %d has line %d, want a 1-indexed line number (input %q)", i, b.Line, input)
			}
			if b.Type != Enter && b.Type != Leave {
				t.Fatalf("block %d has type %v, want enter or leave (input %q)", i, b.Type, input)
			}
			// Matching runs on every cd, so it must not panic on anything
			// the parser was willing to accept.
			b.Pattern.MatchString("/some/path")
		}
	})
}

// FuzzParseBytesMatchesParse pins the equivalence LoadFile depends on:
// parsing bytes already in memory must give the same answer as parsing the
// same bytes from a reader. LoadFile exists to close a TOCTOU by reading
// once and reusing those bytes (see its doc comment), which is only sound
// if the two paths agree.
func FuzzParseBytesMatchesParse(f *testing.F) {
	f.Add([]byte("enter /a\n    echo hi\n"))
	f.Add([]byte("leave /b\n\techo bye\n"))
	f.Add([]byte("garbage"))
	f.Add([]byte("enter ./src\n\techo relative\n"))

	f.Fuzz(func(t *testing.T, input []byte) {
		fromReader, errReader := Parse(strings.NewReader(string(input)), fuzzBase)
		fromBytes, errBytes := Parse(bytes.NewReader(input), fuzzBase)

		if (errReader == nil) != (errBytes == nil) {
			t.Fatalf("disagreement on error: %v vs %v (input %q)", errReader, errBytes, input)
		}
		if errReader != nil {
			if errReader.Error() != errBytes.Error() {
				t.Fatalf("different errors: %q vs %q", errReader, errBytes)
			}
			return
		}
		if len(fromReader.Blocks) != len(fromBytes.Blocks) {
			t.Fatalf("different block counts: %d vs %d (input %q)", len(fromReader.Blocks), len(fromBytes.Blocks), input)
		}
		for i := range fromReader.Blocks {
			a, b := fromReader.Blocks[i], fromBytes.Blocks[i]
			if a.Type != b.Type || a.RawPattern != b.RawPattern || a.Script != b.Script || a.Line != b.Line {
				t.Fatalf("block %d differs: %+v vs %+v", i, a, b)
			}
		}
	})
}

// FuzzCompilePattern fuzzes pattern compilation directly, which Parse only
// reaches with text that already looked like a block header. It must never
// panic, and anything it accepts must be safe to match against — including
// after the `~` and `$VAR` substitutions, which splice text into a regex
// and are therefore the interesting part.
func FuzzCompilePattern(f *testing.F) {
	f.Add("~/Projects/([^/]+)")
	f.Add("~")
	f.Add("$HOME/x")
	f.Add("${HOME}/x")
	f.Add("/a/(b|c)$")
	f.Add("$")
	f.Add("${")
	f.Add("$1")
	f.Add("(((((((((((")
	f.Add("a{1000000}")
	f.Add("./src/([^/]+)")
	f.Add("../../..")
	f.Add("...")
	// Found by this target: unbalanced parens used to close the anchoring
	// group early, so `^(?:)|()$` matched every path at offset 0.
	f.Add(")|(")
	f.Add(")$|^(")

	home := func() (string, error) { return "/home/fuzz.user", nil }

	f.Fuzz(func(t *testing.T, pattern string) {
		re, err := compilePattern(pattern, home, fuzzBase)
		if err != nil {
			if re != nil {
				t.Fatalf("compilePattern returned both a regexp and an error %v", err)
			}
			return
		}
		if re == nil {
			t.Fatalf("compilePattern returned neither a regexp nor an error (pattern %q)", pattern)
		}

		// A compiled pattern is anchored, so it can only ever produce a
		// whole-path match -- that anchoring is what makes matching
		// segment-based instead of prefix-based, and it is easy to lose in
		// a refactor of the substitution code above it.
		for _, dir := range []string{"", "/", "/home/fuzz.user", "/home/fuzz.user/Projects/envoke", "/a/b/c"} {
			if loc := re.FindStringIndex(dir); loc != nil && (loc[0] != 0 || loc[1] != len(dir)) {
				t.Fatalf("pattern %q matched %q partially at %v; compiled patterns must be fully anchored", pattern, dir, loc)
			}
		}
	})
}

package config

import (
	"errors"
	"strings"
	"testing"
)

func TestParse_ReadmeExample(t *testing.T) {
	const src = `enter ~/Projects/([^/]+)
    source venv/bin/activate

leave ~/Projects/([^/]+)
    deactivate
`
	cfg, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(cfg.Blocks))
	}

	enter := cfg.Blocks[0]
	if enter.Type != Enter {
		t.Errorf("block 0: expected Enter, got %v", enter.Type)
	}
	if enter.RawPattern != "~/Projects/([^/]+)" {
		t.Errorf("block 0: unexpected raw pattern %q", enter.RawPattern)
	}
	if enter.Script != "source venv/bin/activate" {
		t.Errorf("block 0: unexpected script %q", enter.Script)
	}
	if enter.Line != 1 {
		t.Errorf("block 0: expected header line 1, got %d", enter.Line)
	}

	leave := cfg.Blocks[1]
	if leave.Type != Leave {
		t.Errorf("block 1: expected Leave, got %v", leave.Type)
	}
	if leave.Script != "deactivate" {
		t.Errorf("block 1: unexpected script %q", leave.Script)
	}
	if leave.Line != 4 {
		t.Errorf("block 1: expected header line 4, got %d", leave.Line)
	}
}

func TestParse_MultiLineScriptWithBlankLine(t *testing.T) {
	const src = `enter /a
    echo one

    echo two
`
	cfg, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "echo one\n\necho two"
	if got := cfg.Blocks[0].Script; got != want {
		t.Errorf("Script = %q, want %q", got, want)
	}
}

func TestParse_ScriptIndentationPreservedRelativeToBlock(t *testing.T) {
	const src = `enter /a
    for f in *; do
        echo "$f"
    done
`
	cfg, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "for f in *; do\n    echo \"$f\"\ndone"
	if got := cfg.Blocks[0].Script; got != want {
		t.Errorf("Script = %q, want %q", got, want)
	}
}

func TestParse_CommentsAndBlankLinesIgnoredOutsideBlocks(t *testing.T) {
	const src = `# top-level comment
enter /a
    echo hi

# another comment
leave /a
    echo bye
`
	cfg, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(cfg.Blocks))
	}
}

func TestParse_MissingPatternIsError(t *testing.T) {
	_, err := Parse(strings.NewReader("enter\n    echo hi\n"))
	assertParseErrorLine(t, err, 1)
}

func TestParse_UnknownHeaderIsError(t *testing.T) {
	_, err := Parse(strings.NewReader("frobnicate /a\n    echo hi\n"))
	assertParseErrorLine(t, err, 1)
}

func TestParse_EmptyBodyIsError(t *testing.T) {
	_, err := Parse(strings.NewReader("enter /a\nleave /a\n    echo hi\n"))
	assertParseErrorLine(t, err, 1)
}

func TestParse_IndentedLineOutsideBlockIsError(t *testing.T) {
	_, err := Parse(strings.NewReader("    echo hi\n"))
	assertParseErrorLine(t, err, 1)
}

func TestParse_InvalidPatternIsError(t *testing.T) {
	_, err := Parse(strings.NewReader("enter (unclosed\n    echo hi\n"))
	assertParseErrorLine(t, err, 1)
}

func assertParseErrorLine(t *testing.T, err error, wantLine int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	perr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if perr.Line != wantLine {
		t.Errorf("ParseError.Line = %d, want %d", perr.Line, wantLine)
	}
}

// TestParse_CommentPlacement pins the rule documented in
// docs/configuration.md: `#` starts a comment only outside a block, so an
// indented one is script text, and an unindented one ends the block above
// it.
func TestParse_CommentPlacement(t *testing.T) {
	t.Run("indented comment stays in the script", func(t *testing.T) {
		cfg, err := Parse(strings.NewReader("enter /a\n    echo one\n    # a shell comment\n    echo two\n"))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(cfg.Blocks) != 1 {
			t.Fatalf("expected 1 block, got %d", len(cfg.Blocks))
		}
		if want := "echo one\n# a shell comment\necho two"; cfg.Blocks[0].Script != want {
			t.Errorf("script = %q, want %q", cfg.Blocks[0].Script, want)
		}
	})

	t.Run("unindented comment ends the block", func(t *testing.T) {
		cfg, err := Parse(strings.NewReader("enter /a\n    echo one\n# not part of it\n"))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if cfg.Blocks[0].Script != "echo one" {
			t.Errorf("script = %q, want %q", cfg.Blocks[0].Script, "echo one")
		}
	})

	t.Run("a body resumed after one is a positioned error", func(t *testing.T) {
		_, err := Parse(strings.NewReader("enter /a\n    echo one\n# ends the block\n    echo orphan\n"))
		var perr *ParseError
		if !errors.As(err, &perr) {
			t.Fatalf("expected a *ParseError, got %v", err)
		}
		if perr.Line != 4 {
			t.Errorf("error reported on line %d, want 4", perr.Line)
		}
	})
}

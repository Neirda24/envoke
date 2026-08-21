package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func fakeHome(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

func failingHome(err error) func() (string, error) {
	return func() (string, error) { return "", err }
}

func TestCompilePattern_PlainPatternIsAnchored(t *testing.T) {
	re, err := compilePattern("/home/foo", fakeHome("/home/user"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}

	if !re.MatchString("/home/foo") {
		t.Errorf("expected exact match on /home/foo")
	}
	// Regression: ondir's basename-prefix bug — /home/foo must not match
	// /home/foobar, nor should it match a subdirectory.
	if re.MatchString("/home/foobar") {
		t.Errorf("pattern must not match /home/foobar (prefix false positive)")
	}
	if re.MatchString("/home/foo/bar") {
		t.Errorf("pattern must not match /home/foo/bar (not a full match)")
	}
}

func TestCompilePattern_LeadingTildeExpandsToHome(t *testing.T) {
	re, err := compilePattern("~/Projects/([^/]+)", fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}

	if !re.MatchString("/Users/adrien/Projects/envoke") {
		t.Errorf("expected match on expanded home directory path")
	}
	if re.MatchString("~/Projects/envoke") {
		t.Errorf("literal ~ must not match once expanded")
	}
}

func TestCompilePattern_BareTildeExpandsToHome(t *testing.T) {
	re, err := compilePattern("~", fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/Users/adrien") {
		t.Errorf("expected bare ~ to match home directory exactly")
	}
}

func TestCompilePattern_MidStringTildeIsLiteral(t *testing.T) {
	re, err := compilePattern("/a~b", fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/a~b") {
		t.Errorf("mid-string ~ should be treated as a literal character, got no match on /a~b")
	}
}

func TestCompilePattern_HomeDirIsQuotedAsLiteral(t *testing.T) {
	// A home directory containing regex metacharacters (e.g. a "." in the
	// username) must be matched literally, not as regex syntax.
	re, err := compilePattern("~/x", fakeHome("/Users/john.doe"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/Users/john.doe/x") {
		t.Errorf("expected literal match against home dir containing a dot")
	}
	if re.MatchString("/Users/johnAdoe/x") {
		t.Errorf("'.' from home dir must not act as a regex wildcard")
	}
}

func TestCompilePattern_HomeDirErrorPropagates(t *testing.T) {
	wantErr := errors.New("no home dir")
	_, err := compilePattern("~/Projects", failingHome(wantErr), "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped home dir error, got %v", err)
	}
}

func TestCompilePattern_EnvVarExpansion(t *testing.T) {
	t.Setenv("ENVOKE_TEST_DIR", "/opt/my.app")

	re, err := compilePattern("$ENVOKE_TEST_DIR/bin", fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/opt/my.app/bin") {
		t.Errorf("expected env var expansion to produce a literal match")
	}
	if re.MatchString("/opt/myXapp/bin") {
		t.Errorf("'.' from env var value must not act as a regex wildcard")
	}
}

func TestCompilePattern_TrailingDollarIsUntouched(t *testing.T) {
	// A pattern ending in a capture group followed by an anchor-like $ must
	// not be mistaken for env var syntax.
	re, err := compilePattern("~/Projects/([^/]+)$", fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/Users/adrien/Projects/envoke") {
		t.Errorf("expected match, trailing $ should have been left alone")
	}
}

func TestCompilePattern_InvalidRegexReturnsError(t *testing.T) {
	_, err := compilePattern("(unclosed", fakeHome("/Users/adrien"), "")
	if err == nil {
		t.Fatalf("expected error for invalid regex")
	}
}

// TestCompilePattern_UndefinedEnvVarIsError covers the failure mode this
// replaced: an undefined variable used to expand to "", so `$HOEM/Projects`
// compiled cleanly into `^(?:/Projects)$` — a valid pattern that simply
// could never match, with nothing anywhere reporting the typo.
func TestCompilePattern_UndefinedEnvVarIsError(t *testing.T) {
	t.Setenv("ENVOKE_TEST_DEFINED", "/opt/app")

	for _, pattern := range []string{
		"$ENVOKE_TEST_UNDEFINED/bin",
		"${ENVOKE_TEST_UNDEFINED}/bin",
		"$ENVOKE_TEST_DEFINED/$ENVOKE_TEST_UNDEFINED",
	} {
		t.Run(pattern, func(t *testing.T) {
			_, err := compilePattern(pattern, fakeHome("/Users/adrien"), "")
			if err == nil {
				t.Fatalf("expected an error for an undefined variable")
			}
			if !strings.Contains(err.Error(), "ENVOKE_TEST_UNDEFINED") {
				t.Errorf("error must name the offending variable, got %q", err)
			}
		})
	}
}

// An explicitly-empty variable is a value, not an omission — only a
// genuinely unset name is an error.
func TestCompilePattern_EmptyEnvVarIsNotAnError(t *testing.T) {
	t.Setenv("ENVOKE_TEST_EMPTY", "")

	re, err := compilePattern("/opt$ENVOKE_TEST_EMPTY/bin", fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/opt/bin") {
		t.Errorf("expected an empty variable to expand to nothing")
	}
}

// TestCompilePattern_DollarBeforeNonNameIsLiteral keeps `$` usable as the
// regex anchor it is. os.Expand would consume `$?`, `$*`, `$#` and `$0`-`$9`
// as shell special variables, which is why expandEnv is hand-rolled.
func TestCompilePattern_DollarBeforeNonNameIsLiteral(t *testing.T) {
	for _, pattern := range []string{
		`~/Projects/(a|b)$`,
		`~/Projects/x$|~/Projects/y`,
		`~/Projects/$1`,
	} {
		t.Run(pattern, func(t *testing.T) {
			if _, err := compilePattern(pattern, fakeHome("/Users/adrien"), ""); err != nil {
				t.Errorf("expected `$` before a non-identifier to stay literal, got %v", err)
			}
		})
	}
}

// An unterminated `${` is not a reference; it stays literal rather than
// swallowing the rest of the pattern.
func TestCompilePattern_UnterminatedBraceIsLiteral(t *testing.T) {
	re, err := compilePattern(`/opt/\$\{unclosed`, fakeHome("/Users/adrien"), "")
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/opt/${unclosed") {
		t.Errorf("expected the literal text to match")
	}
}

// base is a config directory in the platform's native form. Relative patterns
// are resolved against a real directory path, so on Windows the base arrives
// with backslashes and has to come back out slash-normalized — the same rule
// matcher.MatchPath applies to the paths being tested.
func base(p string) string {
	return filepath.FromSlash(p)
}

func TestCompilePattern_RelativeResolvesAgainstConfigDir(t *testing.T) {
	for _, tt := range []struct {
		pattern string
		base    string
		match   []string
		noMatch []string
	}{
		{
			pattern: "./src",
			base:    "/proj",
			match:   []string{"/proj/src"},
			// The anchoring that makes matching segment-based has to survive
			// the base being spliced in front of the pattern.
			noMatch: []string{"/proj/srcx", "/other/proj/src", "/proj", "/proj/src/deep"},
		},
		{
			pattern: ".",
			base:    "/proj",
			match:   []string{"/proj"},
			noMatch: []string{"/proj/src", "/pro"},
		},
		{
			pattern: "..",
			base:    "/proj/app",
			match:   []string{"/proj"},
			noMatch: []string{"/proj/app"},
		},
		{
			pattern: "../sibling",
			base:    "/proj/app",
			match:   []string{"/proj/sibling"},
			noMatch: []string{"/proj/app/sibling"},
		},
		{
			pattern: "../../x",
			base:    "/a/b/c",
			match:   []string{"/a/x"},
			noMatch: []string{"/a/b/x", "/x"},
		},
		{
			// Walking up to the filesystem root leaves a base that already
			// ends in the separator the remainder carries. Doubling it
			// compiles cleanly and then matches nothing at all, so the match
			// assertions below are the point of these cases.
			pattern: "../scratch",
			base:    "/work",
			match:   []string{"/scratch"},
			noMatch: []string{"//scratch", "/work/scratch", "/scratch/deep"},
		},
		{
			// filepath.Dir clamps at the root, so walking past it resolves
			// against the root rather than failing.
			pattern: "../../../../deep",
			base:    "/a/b",
			match:   []string{"/deep"},
			noMatch: []string{"//deep", "/a/deep", "/a/b/deep"},
		},
		{
			// A config file living in the root has such a base from the
			// start, with no "../" walking involved.
			pattern: "./sub",
			base:    "/",
			match:   []string{"/sub"},
			noMatch: []string{"//sub", "/sub/deep"},
		},
		{
			// An empty remainder carries no separator of its own, so the root
			// has to keep the one it ends in and match itself.
			pattern: ".",
			base:    "/",
			match:   []string{"/"},
			noMatch: []string{"", "//", "/etc"},
		},
		{
			pattern: "../..",
			base:    "/a/b",
			match:   []string{"/"},
			noMatch: []string{"", "/a", "/a/b"},
		},
		{
			// The common case, a base with no trailing separator: one level
			// above the root is still an ordinary directory.
			pattern: "../x",
			base:    "/a/b",
			match:   []string{"/a/x"},
			noMatch: []string{"/a//x", "/x", "/a/b/x"},
		},
		{
			pattern: "./src/([^/]+)",
			base:    "/proj",
			match:   []string{"/proj/src/app"},
			noMatch: []string{"/proj/src", "/proj/src/app/deep"},
		},
	} {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := compilePattern(tt.pattern, fakeHome("/home/user"), base(tt.base))
			if err != nil {
				t.Fatalf("compilePattern: %v", err)
			}
			for _, dir := range tt.match {
				if !re.MatchString(dir) {
					t.Errorf("pattern %q (base %s) should match %q, compiled to %v", tt.pattern, tt.base, dir, re)
				}
			}
			for _, dir := range tt.noMatch {
				if re.MatchString(dir) {
					t.Errorf("pattern %q (base %s) should not match %q, compiled to %v", tt.pattern, tt.base, dir, re)
				}
			}
		})
	}
}

// The config directory is spliced into a regex, so it has to be quoted like
// every other substitution — a project named "v1.0" must not turn its own
// dots into wildcards.
func TestCompilePattern_RelativeBaseIsQuoted(t *testing.T) {
	re, err := compilePattern("./src", fakeHome("/home/user"), base("/proj/v1.0"))
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/proj/v1.0/src") {
		t.Errorf("expected the literal directory to match, compiled to %v", re)
	}
	if re.MatchString("/proj/v1X0/src") {
		t.Errorf("a `.` in the config's own directory must stay literal, compiled to %v", re)
	}
}

// TestCompilePattern_RelativeBaseIsNotEnvExpanded pins the ordering between
// the two substitutions. QuoteMeta turns a directory literally named "$HOME"
// into `\$HOME`, which is still a `$` followed by an identifier: expanding
// the environment afterwards would silently retarget the pattern at the real
// home directory.
func TestCompilePattern_RelativeBaseIsNotEnvExpanded(t *testing.T) {
	t.Setenv("ENVOKE_TEST_DIR", "/somewhere/else")

	re, err := compilePattern("./src", fakeHome("/home/user"), base("/tmp/$ENVOKE_TEST_DIR"))
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/tmp/$ENVOKE_TEST_DIR/src") {
		t.Errorf("expected the directory's own name to be matched literally, compiled to %v", re)
	}
}

// Env expansion still applies to what follows the relative prefix.
func TestCompilePattern_RelativeStillExpandsEnvInRemainder(t *testing.T) {
	t.Setenv("ENVOKE_TEST_LEAF", "build")

	re, err := compilePattern("./$ENVOKE_TEST_LEAF", fakeHome("/home/user"), base("/proj"))
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/proj/build") {
		t.Errorf("expected $VAR after the relative prefix to expand, compiled to %v", re)
	}
}

// Without a config file there is no directory to resolve against, and
// compiling the pattern anyway would leave a literal "." in the regex — a
// wildcard matching the wrong thing rather than an honest error.
func TestCompilePattern_RelativeWithoutBaseIsError(t *testing.T) {
	for _, pattern := range []string{"./src", ".", "..", "../x"} {
		t.Run(pattern, func(t *testing.T) {
			if _, err := compilePattern(pattern, fakeHome("/home/user"), ""); err == nil {
				t.Errorf("expected an error for a relative pattern with no base")
			}
		})
	}
}

// Only a leading "./" or "../" is relative, exactly as only a leading "~" is
// home. Everything else keeps the meaning it has always had.
func TestCompilePattern_NonRelativeDotsKeepRegexMeaning(t *testing.T) {
	for _, tt := range []struct {
		pattern string
		match   string
	}{
		{`...`, "/ab"},
		{`.foo`, "xfoo"},
		{`(/opt|/srv)/x`, "/srv/x"},
	} {
		t.Run(tt.pattern, func(t *testing.T) {
			re, err := compilePattern(tt.pattern, fakeHome("/home/user"), base("/proj"))
			if err != nil {
				t.Fatalf("compilePattern: %v", err)
			}
			if !re.MatchString(tt.match) {
				t.Errorf("pattern %q should still be an ordinary regex matching %q, compiled to %v", tt.pattern, tt.match, re)
			}
		})
	}
}

// TestCompilePattern_UnbalancedGroupsCannotEscapeTheAnchors is a regression
// test for a bug this package's own fuzzing found: the anchoring wrapper
// `^(?:...)$` only anchors a pattern whose groups balance. `)|(` wrapped into
// `^(?:)|()$`, which parses as `^(?:)` OR `()$` — a top-level alternation
// outside the anchors, matching the empty string at the start of every path.
// A block written that way fired for every directory in the filesystem.
func TestCompilePattern_UnbalancedGroupsCannotEscapeTheAnchors(t *testing.T) {
	for _, pattern := range []string{")|(", ")$|^(", "a)|(b", ")"} {
		t.Run(pattern, func(t *testing.T) {
			re, err := compilePattern(pattern, fakeHome("/home/user"), base("/proj"))
			if err != nil {
				return // rejected outright, which is the point
			}
			for _, dir := range []string{"/", "/etc", "/home/user/anything"} {
				if loc := re.FindStringIndex(dir); loc != nil && (loc[0] != 0 || loc[1] != len(dir)) {
					t.Fatalf("pattern %q matched %q partially at %v; a compiled pattern must be fully anchored", pattern, dir, loc)
				}
			}
		})
	}
}

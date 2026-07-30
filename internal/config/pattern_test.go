package config

import (
	"errors"
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
	re, err := compilePattern("/home/foo", fakeHome("/home/user"))
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
	re, err := compilePattern("~/Projects/([^/]+)", fakeHome("/Users/adrien"))
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
	re, err := compilePattern("~", fakeHome("/Users/adrien"))
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/Users/adrien") {
		t.Errorf("expected bare ~ to match home directory exactly")
	}
}

func TestCompilePattern_MidStringTildeIsLiteral(t *testing.T) {
	re, err := compilePattern("/a~b", fakeHome("/Users/adrien"))
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
	re, err := compilePattern("~/x", fakeHome("/Users/john.doe"))
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
	_, err := compilePattern("~/Projects", failingHome(wantErr))
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped home dir error, got %v", err)
	}
}

func TestCompilePattern_EnvVarExpansion(t *testing.T) {
	t.Setenv("ENVOKE_TEST_DIR", "/opt/my.app")

	re, err := compilePattern("$ENVOKE_TEST_DIR/bin", fakeHome("/Users/adrien"))
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
	re, err := compilePattern("~/Projects/([^/]+)$", fakeHome("/Users/adrien"))
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/Users/adrien/Projects/envoke") {
		t.Errorf("expected match, trailing $ should have been left alone")
	}
}

func TestCompilePattern_InvalidRegexReturnsError(t *testing.T) {
	_, err := compilePattern("(unclosed", fakeHome("/Users/adrien"))
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
			_, err := compilePattern(pattern, fakeHome("/Users/adrien"))
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

	re, err := compilePattern("/opt$ENVOKE_TEST_EMPTY/bin", fakeHome("/Users/adrien"))
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
			if _, err := compilePattern(pattern, fakeHome("/Users/adrien")); err != nil {
				t.Errorf("expected `$` before a non-identifier to stay literal, got %v", err)
			}
		})
	}
}

// An unterminated `${` is not a reference; it stays literal rather than
// swallowing the rest of the pattern.
func TestCompilePattern_UnterminatedBraceIsLiteral(t *testing.T) {
	re, err := compilePattern(`/opt/\$\{unclosed`, fakeHome("/Users/adrien"))
	if err != nil {
		t.Fatalf("compilePattern: %v", err)
	}
	if !re.MatchString("/opt/${unclosed") {
		t.Errorf("expected the literal text to match")
	}
}

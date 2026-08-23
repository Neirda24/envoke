package matcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

// requireCaseInsensitiveFS skips unless the filesystem holding root treats two
// spellings of one name as one directory.
//
// It probes instead of reading runtime.GOOS because the platform name does not
// answer the question: macOS ships case-insensitive but can be formatted the
// other way, and a Linux directory can be mounted case-insensitively. The probe
// is removed again so the caller's own directories are the only ones under
// root.
func requireCaseInsensitiveFS(t *testing.T, root string) {
	t.Helper()
	probe := mkdir(t, root, "CaseProbe")
	info, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	other, err := os.Stat(filepath.Join(root, "caseprobe"))
	if err != nil || !os.SameFile(info, other) {
		t.Skipf("the filesystem holding %s is case-sensitive: %q and %q are not one directory", root, probe, filepath.Join(root, "caseprobe"))
	}
	if err := os.Remove(probe); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

// upper upper-cases every part, for naming a directory by another spelling.
// Only the parts below a test's root are ever passed: the root is whatever the
// platform's temp directory is called, and re-casing that names a different
// directory on a case-sensitive filesystem instead of the same one.
func upper(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.ToUpper(p)
	}
	return out
}

// under joins parts below root.
func under(root string, parts ...string) string {
	return filepath.Join(append([]string{root}, parts...)...)
}

// TestWithin_PathsDifferingOnlyInCase pins Within's own answer for two
// spellings of one directory, because that answer is **not** the same on every
// platform and nothing else in the suite says so.
//
// Within delegates to filepath.Rel, whose component comparison is
// strings.EqualFold on Windows and byte equality everywhere else (sameWord, in
// the stdlib's path_windows.go and path_unix.go). So a config confined to
// `C:\proj` considers `C:\PROJ\src` inside it, and one confined to `/proj` does
// not consider `/PROJ/src` inside it — even where the filesystem is
// case-insensitive and the two spellings name one directory.
//
// The divergence is meant to stop here, and the two platforms arrive from
// opposite sides. Within is the confinement bound's lexical fast path; where it
// does not fold, a candidate it rejects is put to os.SameFile before being
// refused (TestNewMatch_ConfinedConfigMatchesThroughADifferentlyCasedAncestor),
// and where it does fold there is nothing left for identity to add, which is why
// the walk is not consulted on Windows at all
// (TestSameDirOrAncestor_RefusesWithoutComparingIdentityOnWindows). Either way
// the bound answers the same thing. Keeping
// the fold out of Within is what lets it stay usable on a path that need not
// exist, which both configset.confine and newMatch's fallback need; a refactor
// that moves case-folding in here has to answer that, and one away from
// filepath.Rel has to state which answer it means to give.
func TestWithin_PathsDifferingOnlyInCase(t *testing.T) {
	// foldsCase is where filepath.Rel compares components with EqualFold,
	// which is Windows and nowhere else.
	foldsCase := runtime.GOOS == "windows"

	for _, tt := range []struct {
		name      string
		dir, base string
		want      bool
	}{
		{name: "the base itself, cased differently", dir: np("/PROJ"), base: np("/proj"), want: foldsCase},
		{name: "a child of a differently cased base", dir: np("/PROJ/src"), base: np("/proj"), want: foldsCase},
		// Not the same question: the base matches exactly here, and the case
		// of a component *below* it has no bearing on being within it. True
		// everywhere, and the row is here so the two are not conflated.
		{name: "a differently cased child under an exact base", dir: np("/proj/SRC"), base: np("/proj"), want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Within(tt.dir, tt.base); got != tt.want {
				t.Errorf("Within(%q, %q) = %v, want %v on %s", tt.dir, tt.base, got, tt.want, runtime.GOOS)
			}
		})
	}
}

// TestWithin_TwoVolumesAreNeverWithinEachOther is the Windows half of the
// claim Within's own comment makes: filepath.Rel already knows two volumes
// have no relative path, so no config on one drive can be confined to a
// directory on another.
//
// It needs no second volume to actually exist — Rel is string arithmetic — and
// deliberately so, since whether a runner has a D: is not something a test
// should depend on.
func TestWithin_TwoVolumesAreNeverWithinEachOther(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a volume-qualified path is not something this platform has")
	}
	for _, tt := range []struct{ dir, base string }{
		{dir: `D:\proj\src`, base: `C:\proj`},
		{dir: `D:\proj`, base: `C:\proj`},
		{dir: `\\server\share\proj`, base: `C:\proj`},
	} {
		if Within(tt.dir, tt.base) {
			t.Errorf("Within(%q, %q) = true, want false: separate volumes have no relative path", tt.dir, tt.base)
		}
	}
}

// TestNewMatch_ConfinedConfigMatchesThroughADifferentlyCasedAncestor is where
// the bound's uniform answer is held, and it needs real directories: on a
// case-insensitive filesystem `PROJ` and `proj` are one directory, a cd to
// either lands in the project the fragment came with, and only the filesystem
// can say so.
//
// The block still has to fire. A shell reports the $PWD the user typed, and
// EvalSymlinks hands back the spelling it was given wherever it is not
// following a link, so nothing upstream of the bound folds the case for it.
//
// That is what makes the walk necessary, and it is true everywhere except
// Windows, where EvalSymlinks does normalise and Within folds besides. The rows
// therefore answer the same on Windows by the lexical path alone, which
// TestNewMatch_ConfinedConfigBoundIsLexicalOnWindows asserts rather than leaves
// to coincidence.
func TestNewMatch_ConfinedConfigMatchesThroughADifferentlyCasedAncestor(t *testing.T) {
	root := realRoot(t)
	requireCaseInsensitiveFS(t, root)
	proj := mkdir(t, root, "proj")
	mkdir(t, root, "proj", "src", "deep")

	cfg := local(proj)
	blk := block(config.Enter, matchAny, "echo hi")

	for _, tt := range []struct {
		name  string
		parts []string
	}{
		{"the bound itself", []string{"PROJ"}},
		{"a child of the bound", []string{"PROJ", "src"}},
		{"a grandchild of the bound", []string{"PROJ", "src", "deep"}},
		// The bound matches exactly here, so no identity walk is involved:
		// the row is the counterpart of Within's own, kept so the two are not
		// conflated.
		{"a differently cased child under the bound as spelled", []string{"proj", "SRC"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := under(root, tt.parts...)
			m, ok := NewMatch(cfg, blk, dir)
			if !ok {
				t.Fatalf("NewMatch(%q) = false, want a match: it is the same directory as the bound %q", dir, proj)
			}
			if m.Dir != dir {
				t.Errorf("Match.Dir = %q, want the directory the shell reported (%q)", m.Dir, dir)
			}
		})
	}
}

// TestNewMatch_ConfinedConfigRefusesOutsideItsBoundHoweverCased is the half
// that matters: the identity walk exists to admit one directory under two
// names, and must not have become a way out of the bound.
//
// Every directory here exists, so each is judged by the walk rather than by the
// fallback for a path that will not resolve, and each is named twice — as
// spelled, and upper-cased. On a case-insensitive filesystem the second
// spelling names the same directory as the first and is refused for the same
// reason; on a case-sensitive one it names nothing, and the answer must still
// be no.
func TestNewMatch_ConfinedConfigRefusesOutsideItsBoundHoweverCased(t *testing.T) {
	root := realRoot(t)
	proj := mkdir(t, root, "proj")
	mkdir(t, root, "proj2", "src")
	mkdir(t, root, "other")
	mkdir(t, root, "elsewhere", "proj")

	cfg := local(proj)
	blk := block(config.Enter, matchAny, "echo hi")

	// The pattern matches anything, so a table of refusals proves nothing
	// unless this fires.
	if _, ok := NewMatch(cfg, blk, proj); !ok {
		t.Fatalf("the fixture cannot refuse anything meaningfully: it does not match its own bound %q", proj)
	}

	for _, tt := range []struct {
		name  string
		parts []string
	}{
		// A name the bound's is a prefix of: what a comparison of strings
		// gets wrong, and what an identity comparison cannot.
		{"a directory whose name merely begins with the bound's", []string{"proj2"}},
		{"a subdirectory of that directory", []string{"proj2", "src"}},
		{"a sibling reached through the same parent", []string{"other"}},
		{"the parent the bound sits in", nil},
		// The walk compares identity, not names, so the bound's own basename
		// somewhere else buys nothing.
		{"a directory elsewhere carrying the bound's own name", []string{"elsewhere", "proj"}},
	} {
		for _, spelling := range []struct {
			how   string
			parts []string
		}{
			{"as spelled", tt.parts},
			{"upper-cased", upper(tt.parts)},
		} {
			t.Run(tt.name+", "+spelling.how, func(t *testing.T) {
				dir := under(root, spelling.parts...)
				if _, ok := NewMatch(cfg, blk, dir); ok {
					t.Errorf("NewMatch(%q) = true, want false: it is outside the bound %q", dir, proj)
				}
			})
		}
	}
}

// TestSameDirOrAncestor_RefusesWithoutComparingIdentityOnWindows pins the one
// platform on which the identity half of the bound does not compare identity.
//
// os.SameFile there rests on a file index that ReFS does not supply and that
// FAT/exFAT and some SMB redirectors derive from a directory-entry offset, and
// the walk is consulted only for a directory Within has already placed outside
// base — so an identity two distinct directories happen to share widens a
// security bound rather than costing a match. The refusal is what removes that,
// and it removes nothing else: on Windows EvalSymlinks has already normalised
// every component to its on-disk spelling and filepath.Rel folds case, so
// Within answers what the walk was added for.
//
// The rows are the least ambiguous yes the walk can be asked for: directories
// that exist, under a base spelled exactly as they spell it, with no link or
// case difference anywhere. A no to one of those can only mean nothing was
// compared, which is the assertion here; off Windows the same rows are what says
// the walk still compares.
func TestSameDirOrAncestor_RefusesWithoutComparingIdentityOnWindows(t *testing.T) {
	root := realRoot(t)
	proj := mkdir(t, root, "proj")
	src := mkdir(t, root, "proj", "src")

	compares := runtime.GOOS != "windows"

	for _, tt := range []struct {
		name string
		dir  string
		want bool
	}{
		{name: "base itself", dir: proj, want: compares},
		{name: "a child of base", dir: src, want: compares},
		{name: "a grandchild of base", dir: mkdir(t, root, "proj", "src", "deep"), want: compares},
		// Not platform-dependent: the walk reaching the filesystem root without
		// meeting base is a no everywhere, and the row is here so a guard that
		// inverted the answer could not pass on the rows above alone.
		{name: "the directory base sits in", dir: root, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameDirOrAncestor(tt.dir, proj); got != tt.want {
				t.Errorf("sameDirOrAncestor(%q, %q) = %v, want %v on %s", tt.dir, proj, got, tt.want, runtime.GOOS)
			}
		})
	}
}

// TestNewMatch_ConfinedConfigBoundIsLexicalOnWindows is the same guard seen
// where it matters — through the bound — and it asserts two things at once: that
// the answer is what it always was, and that no walk was involved in reaching
// it.
//
// candidate.bounds is the only trace the identity half leaves, written wherever
// withinBound reached it after Within had missed
// (TestNewMatch_ConfinedConfigRefusesOnThePatternBeforeTheBound observes it for
// the same reason). So nil across the admitted rows says Within admitted them by
// itself, which is the claim the guard rests on; the refused rows keep the trace,
// because the identity half is still *reached* on Windows and only refuses
// without statting.
//
// Windows-only because off it the expectations invert rather than hold: there
// Within misses on case, the walk admits, and the trace is non-nil for the very
// rows that are nil here —
// TestNewMatch_ConfinedConfigMatchesThroughADifferentlyCasedAncestor is that
// half.
func TestNewMatch_ConfinedConfigBoundIsLexicalOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the walk is only guarded on Windows; every row here is answered by the identity half elsewhere")
	}
	root := realRoot(t)
	requireCaseInsensitiveFS(t, root)
	proj := mkdir(t, root, "proj")
	mkdir(t, root, "proj", "src")
	mkdir(t, root, "elsewhere")

	cfg := local(proj)
	blk := block(config.Enter, matchAny, "echo hi")

	for _, tt := range []struct {
		name  string
		parts []string
		want  bool
		// consulted is whether withinBound reached the identity half at all,
		// which on Windows is as far as anything gets.
		consulted bool
	}{
		{name: "the bound itself, cased differently", parts: []string{"PROJ"}, want: true},
		{name: "a child of the bound, through a cased ancestor", parts: []string{"PROJ", "src"}, want: true},
		{name: "a differently cased child under the bound as spelled", parts: []string{"proj", "SRC"}, want: true},
		{name: "a directory outside the bound", parts: []string{"elsewhere"}, want: false, consulted: true},
		{name: "a directory outside the bound, cased differently", parts: []string{"ELSEWHERE"}, want: false, consulted: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := under(root, tt.parts...)
			c := &candidate{dir: dir}
			if _, ok := newMatch(cfg, blk, c); ok != tt.want {
				t.Fatalf("newMatch(%q) = %v, want %v against the bound %q", dir, ok, tt.want, proj)
			}
			if consulted := c.bounds != nil; consulted != tt.consulted {
				t.Errorf("reached the identity bound = %v, want %v for %q", consulted, tt.consulted, dir)
			}
			if within, asked := c.bounds[proj]; asked && within {
				t.Errorf("the identity bound answered yes for %q, and on this platform it must never answer anything but no", dir)
			}
		})
	}
}

// TestNewMatch_ConfinedConfigPatternKeepsTheSpellingItResolvedTo records what
// the bound's identity fallback does not extend to. The bound decides whether a
// block may fire; the pattern still decides whether it does, and it runs
// against the resolved path as spelled — a regex over a path, which means what
// it says about case.
//
// So the two halves can disagree, and the platforms differ in whether they do:
// filepath.EvalSymlinks normalises every component to its on-disk spelling on
// Windows and reproduces the spelling it was handed elsewhere. Deriving want
// from that rather than from runtime.GOOS is the assertion — that the pattern
// ran against the resolved form, whatever the resolved form turned out to be.
func TestNewMatch_ConfinedConfigPatternKeepsTheSpellingItResolvedTo(t *testing.T) {
	root := realRoot(t)
	requireCaseInsensitiveFS(t, root)
	proj := mkdir(t, root, "proj")
	src := mkdir(t, root, "proj", "src")

	dir := under(root, "PROJ", "src")
	physical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}

	blk := block(config.Enter, anchored(src), "echo hi")
	m, ok := NewMatch(local(proj), blk, dir)
	if want := physical == src; ok != want {
		t.Fatalf("NewMatch(%q) with a pattern for %q = %v, want %v: it resolved to %q", dir, src, ok, want, physical)
	}
	if ok && m.Groups[0] != MatchPath(physical) {
		t.Errorf("Groups[0] = %q, want the resolved %q", m.Groups[0], MatchPath(physical))
	}
}

package matcher

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
)

// tp makes a Unix-style test path absolute on the platform running the
// test: on Windows "/a/b" becomes "C:/a/b", since filepath.IsAbs rejects a
// path with no volume there and Transitions requires absolute paths.
//
// It returns forward slashes on both platforms on purpose. Patterns are
// matched against MatchPath (filepath.ToSlash), so a pattern and a path can
// both go through tp and stay consistent — which is the whole point of
// having one helper rather than two conventions.
func tp(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + p
	}
	return p
}

// set wraps a single config as the one-element set Resolve and Enters take.
// Most cases here are about the matching itself, which doesn't care how many
// configs are in play; the ones that do build their own slice.
func set(cfgs ...*config.Config) []*config.Config {
	return cfgs
}

// np is tp in the platform's *native* form, for comparing against what
// Transitions returns — it filepath.Clean's its output, so Windows gets
// backslashes back.
func np(p string) string {
	return filepath.Clean(tp(p))
}

// nps maps np over a list of expected paths.
func nps(paths ...string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = np(p)
	}
	return out
}

func block(typ config.BlockType, anchoredPattern, script string) config.Block {
	return config.Block{
		Type:       typ,
		Pattern:    regexp.MustCompile(anchoredPattern),
		RawPattern: anchoredPattern,
		Script:     script,
	}
}

// pattern anchors a Unix-style path as a full-match regex, applying the same
// volume prefix tp does so it lines up with the paths under test.
func pattern(p string) string {
	return "^" + regexp.QuoteMeta(tp(p)) + "$"
}

func TestTransitions_Traverse(t *testing.T) {
	left, entered, err := Transitions(tp("/a/b/c/d"), tp("/a/x/y"))
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}

	wantLeft := nps("/a/b/c/d", "/a/b/c", "/a/b")
	if !reflect.DeepEqual(left, wantLeft) {
		t.Errorf("left = %v, want %v", left, wantLeft)
	}

	wantEntered := nps("/a/x", "/a/x/y")
	if !reflect.DeepEqual(entered, wantEntered) {
		t.Errorf("entered = %v, want %v", entered, wantEntered)
	}
}

func TestTransitions_SamePathIsNoOp(t *testing.T) {
	left, entered, err := Transitions(tp("/a/b"), tp("/a/b"))
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if len(left) != 0 || len(entered) != 0 {
		t.Errorf("expected no transitions, got left=%v entered=%v", left, entered)
	}
}

func TestTransitions_DirectSiblingNoCommonSubtree(t *testing.T) {
	left, entered, err := Transitions(tp("/a"), tp("/b"))
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	if want := nps("/a"); !reflect.DeepEqual(left, want) {
		t.Errorf("left = %v, want %v", left, want)
	}
	if want := nps("/b"); !reflect.DeepEqual(entered, want) {
		t.Errorf("entered = %v, want %v", entered, want)
	}
}

func TestTransitions_RequiresAbsolutePaths(t *testing.T) {
	if _, _, err := Transitions("relative/a", tp("/a")); err == nil {
		t.Errorf("expected error for relative from path")
	}
	if _, _, err := Transitions(tp("/a"), "relative/b"); err == nil {
		t.Errorf("expected error for relative to path")
	}
}

func TestResolve_SegmentMatchingNotPrefix(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Leave, pattern("/home/foo"), "echo leaving foo"),
	}}

	// Regression: ondir's basename-prefix bug. /home/foobar must not trigger
	// a rule written for /home/foo.
	leaves, _, err := Resolve(set(cfg), tp("/home/foobar"), tp("/tmp"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 0 {
		t.Errorf("expected no leave matches for /home/foobar, got %v", leaves)
	}

	leaves, _, err = Resolve(set(cfg), tp("/home/foo"), tp("/tmp"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 1 || leaves[0].Dir != np("/home/foo") {
		t.Errorf("expected one leave match on /home/foo, got %v", leaves)
	}
}

func TestResolve_EnterFiresShallowFirstOnTraverse(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a/x"), "echo enter x"),
		block(config.Enter, pattern("/a/x/y"), "echo enter y"),
	}}

	_, enters, err := Resolve(set(cfg), tp("/a"), tp("/a/x/y"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 2 {
		t.Fatalf("expected 2 enter matches, got %d: %v", len(enters), enters)
	}
	if enters[0].Dir != np("/a/x") || enters[1].Dir != np("/a/x/y") {
		t.Errorf("expected shallow-first order [%s, %s], got [%s, %s]", np("/a/x"), np("/a/x/y"), enters[0].Dir, enters[1].Dir)
	}
}

func TestEnters_MatchesTheWholeAncestorChainShallowFirst(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a/x"), "echo enter x"),
		block(config.Enter, pattern("/a/x/y"), "echo enter y"),
		block(config.Leave, pattern("/a/x"), "echo leave x"),
	}}

	enters, err := Enters(set(cfg), tp("/a/x/y"))
	if err != nil {
		t.Fatalf("Enters: %v", err)
	}
	if len(enters) != 2 {
		t.Fatalf("expected 2 enter matches, got %d: %v", len(enters), enters)
	}
	if enters[0].Dir != np("/a/x") || enters[1].Dir != np("/a/x/y") {
		t.Errorf("expected shallow-first order [%s, %s], got [%s, %s]", np("/a/x"), np("/a/x/y"), enters[0].Dir, enters[1].Dir)
	}
}

// TestEnters_IncludesTheDirectoryItself is what separates Enters from
// Resolve: Resolve reports what *changed*, so it can never report a block
// for a directory that is on both sides of the transition.
func TestEnters_IncludesTheDirectoryItself(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a"), "echo a"),
	}}

	enters, err := Enters(set(cfg), tp("/a"))
	if err != nil {
		t.Fatalf("Enters: %v", err)
	}
	if len(enters) != 1 {
		t.Fatalf("expected the directory's own block, got %d matches", len(enters))
	}

	_, resolved, err := Resolve(set(cfg), tp("/a"), tp("/a"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("Resolve on a no-op transition should report nothing, got %d", len(resolved))
	}
}

func TestEnters_RejectsRelativePath(t *testing.T) {
	if _, err := Enters(set(&config.Config{}), "a/b"); err == nil {
		t.Error("expected a relative path to be rejected")
	}
}

func TestResolve_LeaveFiresDeepFirstOnTraverse(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Leave, pattern("/a/x"), "echo leave x"),
		block(config.Leave, pattern("/a/x/y"), "echo leave y"),
	}}

	leaves, _, err := Resolve(set(cfg), tp("/a/x/y"), tp("/a"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leave matches, got %d: %v", len(leaves), leaves)
	}
	if leaves[0].Dir != np("/a/x/y") || leaves[1].Dir != np("/a/x") {
		t.Errorf("expected deep-first order [%s, %s], got [%s, %s]", np("/a/x/y"), np("/a/x"), leaves[0].Dir, leaves[1].Dir)
	}
}

func TestResolve_MultipleBlocksSameDirFireInDeclarationOrder(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/a/x"), "echo first"),
		block(config.Enter, pattern("/a/x"), "echo second"),
	}}

	_, enters, err := Resolve(set(cfg), tp("/a"), tp("/a/x"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 2 || enters[0].Block.Script != "echo first" || enters[1].Block.Script != "echo second" {
		t.Errorf("expected declaration order [first, second], got %v", enters)
	}
}

// Leave ordering has two halves that point in opposite directions: across
// configs a transition unwinds in the reverse of the order it was applied,
// while within a single config the blocks fire in the order they were written.
// The reversal stops at the config boundary and does not reach inside a file.
func TestResolve_LeaveOrderIsDeclarationOrderWithinAConfigAndReversedAcrossConfigs(t *testing.T) {
	leave := func(scripts ...string) *config.Config {
		cfg := &config.Config{}
		for _, s := range scripts {
			cfg.Blocks = append(cfg.Blocks, block(config.Leave, pattern("/a/x"), s))
		}
		return cfg
	}

	for _, tt := range []struct {
		name string
		cfgs []*config.Config
		want []string
	}{
		{
			name: "several blocks in one config",
			cfgs: set(leave("first", "second")),
			want: []string{"first", "second"},
		},
		{
			name: "one block in each of several configs",
			cfgs: set(leave("outer"), leave("inner")),
			want: []string{"inner", "outer"},
		},
		{
			name: "several blocks in each of several configs",
			cfgs: set(leave("outer-first", "outer-second"), leave("inner-first", "inner-second")),
			want: []string{"inner-first", "inner-second", "outer-first", "outer-second"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			leaves, _, err := Resolve(tt.cfgs, tp("/a/x"), tp("/a"))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got := scripts(leaves); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("leaves ran %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolve_NoMatchesReturnsEmptySlices(t *testing.T) {
	cfg := &config.Config{Blocks: []config.Block{
		block(config.Enter, pattern("/never/matches"), "echo hi"),
	}}

	leaves, enters, err := Resolve(set(cfg), tp("/a"), tp("/b"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(leaves) != 0 || len(enters) != 0 {
		t.Errorf("expected no matches, got leaves=%v enters=%v", leaves, enters)
	}
}

// local builds a directory-local config rooted at dir.
func local(dir string, blocks ...config.Block) *config.Config {
	return &config.Config{Path: filepath.Join(dir, ".envokerc"), Dir: dir, Local: true, Blocks: blocks}
}

// matchAny is the pattern a confined config's bound has to hold against: one
// aimed straight out of the config's own directory.
const matchAny = "^(?:.*)$"

// realRoot is a fresh temp directory with every symlink in it already
// resolved. Confinement resolves the directory it is handed, so the cases
// exercising it need directories that exist, rooted somewhere physical: a
// platform's temp directory need not be (macOS's is under /var, itself a link
// to private/var), and what these cases are about is the link they build
// themselves.
func realRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return root
}

// mkdir creates root/parts... and returns it.
func mkdir(t *testing.T, root string, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return dir
}

// symlink points newname at oldname, skipping the test where the platform
// won't allow one (an unprivileged Windows session).
func symlink(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// anchored is pattern for a path that is already absolute and native, which
// the real directories the confinement cases need are. It goes through
// MatchPath for the same reason matching does.
func anchored(p string) string {
	return "^" + regexp.QuoteMeta(MatchPath(p)) + "$"
}

// TestNewMatch_LocalConfigIsConfinedToItsSubtree is the security property of
// directory-local configs. A .envokerc arrives with somebody else's
// repository, so however its pattern is written it must not be able to fire
// for a directory outside the tree it came with.
func TestNewMatch_LocalConfigIsConfinedToItsSubtree(t *testing.T) {
	root := realRoot(t)
	proj := mkdir(t, root, "proj")
	blk := config.Block{Type: config.Enter, Pattern: regexp.MustCompile(matchAny), Script: "echo hi"}
	cfg := local(proj, blk)

	for _, dir := range []string{proj, mkdir(t, root, "proj", "src")} {
		if _, ok := NewMatch(cfg, blk, dir); !ok {
			t.Errorf("a local config must still match inside its own subtree (%s)", dir)
		}
	}
	for _, dir := range []string{mkdir(t, root, "etc"), root, mkdir(t, root, "projx")} {
		if _, ok := NewMatch(cfg, blk, dir); ok {
			t.Errorf("a local config must not match %s, outside %s", dir, proj)
		}
	}
}

// The confinement is specific to local configs: the user's own central config
// is theirs to point anywhere.
func TestNewMatch_CentralConfigIsNotConfined(t *testing.T) {
	block := config.Block{Type: config.Enter, Pattern: regexp.MustCompile(matchAny), Script: "echo hi"}
	cfg := &config.Config{Path: np("/home/user/.envokerc"), Dir: np("/home/user"), Blocks: []config.Block{block}}

	if _, ok := NewMatch(cfg, block, np("/etc")); !ok {
		t.Errorf("the central config must be able to match outside its own directory")
	}
}

// TestNewMatch_ConfinedConfigMatchesTheResolvedDirectory covers the shape a
// project fragment actually has: config.LoadFragmentResolved is handed the
// followed symlink, so the config's Dir and the base of its ./ patterns are
// physical, while the directory matcher is handed is the shell's own logical
// $PWD. Both the bound and the pattern therefore apply to the resolved form —
// one without the other leaves half the fragment dead.
func TestNewMatch_ConfinedConfigMatchesTheResolvedDirectory(t *testing.T) {
	root := realRoot(t)
	projects := mkdir(t, root, "projects")
	proj := mkdir(t, root, "projects", "proj")
	src := mkdir(t, root, "projects", "proj", "src")
	outside := mkdir(t, root, "projects", "outside")
	link := filepath.Join(root, "link")
	symlink(t, projects, link)
	symlink(t, outside, filepath.Join(proj, "escape"))

	logical := filepath.Join(link, "proj")
	cfg := local(proj)

	for _, tt := range []struct {
		name    string
		pattern string
		dir     string
		want    bool
	}{
		{"the project through a symlinked ancestor", anchored(proj), logical, true},
		{"a subdirectory through a symlinked ancestor", anchored(src), filepath.Join(logical, "src"), true},
		{"the project by its physical path", anchored(proj), proj, true},
		// The config's patterns are compiled against physical paths, so a
		// pattern spelled the way the shell reports the directory is the one
		// that cannot match.
		{"a pattern written against the logical path", anchored(logical), logical, false},
		{"a sibling of the project", matchAny, outside, false},
		{"a sibling through the same symlinked ancestor", matchAny, filepath.Join(link, "outside"), false},
		// Inside the project by name, outside it in fact: the case a textual
		// comparison lets through.
		{"a path inside the project that resolves outside it", matchAny, filepath.Join(proj, "escape"), false},
		// A directory that is not there resolves to nothing, so the bound is
		// left with the spelling — which still has to be inside it, and to be
		// the physical spelling: reached through the symlinked ancestor, a
		// path that will not resolve has nothing left to identify the project
		// by and is refused.
		{"an unresolvable directory inside the bound", matchAny, filepath.Join(proj, "gone"), true},
		{"a pattern for an unresolvable directory inside the bound", anchored(filepath.Join(proj, "gone")), filepath.Join(proj, "gone"), true},
		{"an unresolvable directory outside the bound", matchAny, filepath.Join(root, "gone"), false},
		{"an unresolvable directory through a symlinked ancestor", matchAny, filepath.Join(logical, "gone"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			blk := block(config.Enter, tt.pattern, "echo hi")
			m, ok := NewMatch(cfg, blk, tt.dir)
			if ok != tt.want {
				t.Fatalf("NewMatch(%q) = %v, want %v", tt.dir, ok, tt.want)
			}
			if ok && m.Dir != tt.dir {
				t.Errorf("Match.Dir = %q, want the directory the shell reported (%q)", m.Dir, tt.dir)
			}
		})
	}
}

// TestNewMatch_ConfinedConfigRefusesOnThePatternBeforeTheBound pins which of
// the two refusals a confined config spends on a directory outside its project,
// because that is the whole cost of the bound on an ordinary cd: every
// directory of a transition is tested against every confined fragment, and the
// identity walk stats each of its ancestors while the pattern — literal-prefixed
// with the config's own directory by config.compilePattern — refuses with no
// syscall at all.
//
// Both refusals are required for a Match, so this asserts nothing about the
// answer, which TestNewMatch_ConfinedConfigMatchesTheResolvedDirectory and
// TestNewMatch_ConfinedConfigRefusesOutsideItsBoundHoweverCased fix. It
// observes the identity bound through candidate.bounds, which is written only
// where withinBound was reached and Within had already missed — so nil means the
// bound was never consulted, and the guard below is what makes that unambiguous
// by showing Within does miss for this directory. It is the consultation and not
// the syscalls that is visible, which is what lets the rows read the same on
// Windows, where sameDirOrAncestor refuses before statting anything. Should the
// memo ever go, this needs another way to see the bound rather than the
// assertion dropped.
func TestNewMatch_ConfinedConfigRefusesOnThePatternBeforeTheBound(t *testing.T) {
	root := realRoot(t)
	proj := mkdir(t, root, "proj")
	src := mkdir(t, root, "proj", "src")
	outside := mkdir(t, root, "elsewhere")

	if Within(outside, proj) {
		t.Fatalf("the fixture cannot show anything: %q is lexically inside %q, so no walk was ever owed", outside, proj)
	}
	cfg := local(proj)

	for _, tt := range []struct {
		name    string
		pattern string
		// walked is whether the identity bound had to be consulted at all.
		walked bool
	}{
		{
			name:    "a pattern rooted in the config's own directory",
			pattern: anchored(src),
			walked:  false,
		},
		{
			// The bound is not skippable, only second: a fragment whose
			// pattern reaches out of its own project is exactly what it
			// exists to refuse, and the syscalls are worth spending there.
			name:    "a pattern reaching outside the config's own directory",
			pattern: anchored(outside),
			walked:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &candidate{dir: outside}
			if _, ok := newMatch(cfg, block(config.Enter, tt.pattern, "echo hi"), c); ok {
				t.Fatalf("newMatch(%q) = true, want false: it is outside the bound %q", outside, proj)
			}
			if walked := c.bounds != nil; walked != tt.walked {
				t.Errorf("consulted the identity bound = %v, want %v for %q", walked, tt.walked, tt.pattern)
			}
		})
	}
}

// The captures a confined config's script sees come from the path its pattern
// ran against, which is the resolved one, while Match.Dir stays where the cd
// landed. The two disagree on purpose: recapturing against the logical form
// would mean running a pattern that need not match it at all, leaving a block
// firing with no groups.
func TestNewMatch_ConfinedConfigCapturesTheResolvedPath(t *testing.T) {
	root := realRoot(t)
	projects := mkdir(t, root, "projects")
	proj := mkdir(t, root, "projects", "proj")
	src := mkdir(t, root, "projects", "proj", "src")
	symlink(t, projects, filepath.Join(root, "link"))

	logical := filepath.Join(root, "link", "proj", "src")
	blk := block(config.Enter, "^"+regexp.QuoteMeta(MatchPath(proj))+"/(.*)$", "echo hi")

	m, ok := NewMatch(local(proj), blk, logical)
	if !ok {
		t.Fatalf("expected a match for %q", logical)
	}
	if m.Dir != logical {
		t.Errorf("Match.Dir = %q, want %q", m.Dir, logical)
	}
	if len(m.Groups) != 2 {
		t.Fatalf("Groups = %v, want the whole match and one capture", m.Groups)
	}
	if want := MatchPath(src); m.Groups[0] != want {
		t.Errorf("Groups[0] = %q, want the resolved path %q", m.Groups[0], want)
	}
	if m.Groups[1] != "src" {
		t.Errorf("Groups[1] = %q, want %q", m.Groups[1], "src")
	}
}

// The same thing through Resolve, which is where each directory of a
// transition gets resolved once for every config and block tested against it:
// two directories in one transition must each be matched against their own
// resolved form, shallowest first.
func TestResolve_ConfinedConfigMatchesThroughASymlinkedAncestor(t *testing.T) {
	root := realRoot(t)
	projects := mkdir(t, root, "projects")
	proj := mkdir(t, root, "projects", "proj")
	a := mkdir(t, root, "projects", "proj", "a")
	b := mkdir(t, root, "projects", "proj", "a", "b")
	symlink(t, projects, filepath.Join(root, "link"))

	logical := filepath.Join(root, "link", "proj")
	cfg := local(proj,
		block(config.Enter, anchored(a), "echo a"),
		block(config.Enter, anchored(b), "echo b"),
	)

	_, enters, err := Resolve(set(cfg), logical, filepath.Join(logical, "a", "b"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := scripts(enters); !reflect.DeepEqual(got, []string{"echo a", "echo b"}) {
		t.Fatalf("enters ran %v, want both blocks shallowest first", got)
	}
	for i, want := range []string{filepath.Join(logical, "a"), filepath.Join(logical, "a", "b")} {
		if enters[i].Dir != want {
			t.Errorf("enters[%d].Dir = %q, want the logical %q", i, enters[i].Dir, want)
		}
	}
}

// An unconfined config — the central config, and a fragment that really lives
// in envokerc.d — is matched against the directory the shell reported and
// nothing else: patterns there are written against the $PWD the user sees, and
// resolving would both change what they match and pay for an lstat per path
// component on every cd.
func TestNewMatch_UnconfinedConfigIsNeverResolved(t *testing.T) {
	root := realRoot(t)
	projects := mkdir(t, root, "projects")
	proj := mkdir(t, root, "projects", "proj")
	symlink(t, projects, filepath.Join(root, "link"))

	logical := filepath.Join(root, "link", "proj")
	cfg := &config.Config{Path: filepath.Join(root, ".envokerc"), Dir: root}

	for _, tt := range []struct {
		name    string
		pattern string
		want    bool
	}{
		{"a pattern written against the logical path", anchored(logical), true},
		{"a pattern written against the resolved path", anchored(proj), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := NewMatch(cfg, block(config.Enter, tt.pattern, "echo hi"), logical)
			if ok != tt.want {
				t.Fatalf("NewMatch(%q) = %v, want %v", logical, ok, tt.want)
			}
			if ok && m.Groups[0] != MatchPath(logical) {
				t.Errorf("Groups[0] = %q, want the unresolved %q", m.Groups[0], MatchPath(logical))
			}
		})
	}
}

// Nothing on the unconfined path asks the filesystem anything, which is what
// keeps a pattern matching a directory that no longer exists — the leave side
// of a cd out of a directory that was just removed.
func TestNewMatch_UnconfinedConfigMatchesADirectoryThatIsGone(t *testing.T) {
	gone := np("/gone/for/good")
	blk := block(config.Leave, anchored(gone), "echo hi")

	if _, ok := NewMatch(&config.Config{Path: np("/home/user/.envokerc"), Dir: np("/home/user")}, blk, gone); !ok {
		t.Errorf("an unconfined config must match %q without resolving it", gone)
	}
}

// A fragment whose own symlink would not resolve reports the link's directory
// as Dir and is confined on that basis (configset.confine). The bound is then
// the config directory, so the project the link pointed at is out of reach —
// including when the directory being tested cannot be resolved either, which
// leaves the bound holding on the spelling alone.
func TestNewMatch_ConfigWithUnresolvedDirIsBoundedToTheLinkDirectory(t *testing.T) {
	root := realRoot(t)
	fragments := mkdir(t, root, "envokerc.d")
	proj := mkdir(t, root, "proj")

	cfg := &config.Config{
		Path:          filepath.Join(fragments, "project"),
		Dir:           fragments,
		DirUnresolved: true,
		Local:         true,
	}
	blk := block(config.Enter, matchAny, "echo hi")

	for _, tt := range []struct {
		name string
		dir  string
		want bool
	}{
		{"its own directory", fragments, true},
		{"the project the link pointed at", proj, false},
		{"an unresolvable directory inside its own", filepath.Join(fragments, "gone"), true},
		{"an unresolvable directory outside it", filepath.Join(proj, "gone"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := NewMatch(cfg, blk, tt.dir); ok != tt.want {
				t.Errorf("NewMatch(%q) = %v, want %v (bound to %q)", tt.dir, ok, tt.want, fragments)
			}
		})
	}
}

func TestWithin(t *testing.T) {
	for _, tt := range []struct {
		dir, base string
		want      bool
	}{
		{np("/proj"), np("/proj"), true},
		{np("/proj/src"), np("/proj"), true},
		{np("/proj/src/deep"), np("/proj"), true},
		{np("/projx"), np("/proj"), false},
		{np("/"), np("/proj"), false},
		{np("/other"), np("/proj"), false},
		{np("/proj"), np("/proj/src"), false},
	} {
		t.Run(tt.dir+" in "+tt.base, func(t *testing.T) {
			if got := Within(tt.dir, tt.base); got != tt.want {
				t.Errorf("Within(%q, %q) = %v, want %v", tt.dir, tt.base, got, tt.want)
			}
		})
	}
}

// TestResolve_ConfigOrderIsMirroredOnLeave pins how several configs compose:
// on the way in, the outermost config's blocks run first; on the way out, the
// innermost unwinds first. Same rule the directory ordering already follows,
// applied to the configs matching a single directory.
func TestResolve_ConfigOrderIsMirroredOnLeave(t *testing.T) {
	root := realRoot(t)
	proj := mkdir(t, root, "proj")
	all := regexp.MustCompile(anchored(proj))
	central := &config.Config{
		Path: np("/home/user/.envokerc"),
		Dir:  np("/home/user"),
		Blocks: []config.Block{
			{Type: config.Enter, Pattern: all, Script: "central-enter"},
			{Type: config.Leave, Pattern: all, Script: "central-leave"},
		},
	}
	inner := local(proj,
		config.Block{Type: config.Enter, Pattern: all, Script: "local-enter"},
		config.Block{Type: config.Leave, Pattern: all, Script: "local-leave"},
	)
	cfgs := []*config.Config{central, inner}

	_, enters, err := Resolve(cfgs, root, proj)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := scripts(enters); !reflect.DeepEqual(got, []string{"central-enter", "local-enter"}) {
		t.Errorf("enters ran %v, want the outermost config first", got)
	}

	leaves, _, err := Resolve(cfgs, proj, root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := scripts(leaves); !reflect.DeepEqual(got, []string{"local-leave", "central-leave"}) {
		t.Errorf("leaves ran %v, want the innermost config first", got)
	}
}

// A match has to carry the config it came from: with several in play, that is
// the only thing that says whose trust decision gates it.
func TestResolve_MatchCarriesItsConfig(t *testing.T) {
	root := realRoot(t)
	proj := mkdir(t, root, "proj")
	all := regexp.MustCompile(anchored(proj))
	cfg := local(proj, config.Block{Type: config.Enter, Pattern: all, Script: "echo hi"})

	_, enters, err := Resolve(set(cfg), root, proj)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(enters) != 1 {
		t.Fatalf("got %d enters, want 1", len(enters))
	}
	if enters[0].Config != cfg {
		t.Errorf("Match.Config = %v, want the config the block was declared in", enters[0].Config)
	}
}

func scripts(matches []Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Block.Script
	}
	return out
}

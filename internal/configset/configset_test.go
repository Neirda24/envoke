package configset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Neirda24/envoke/internal/config"
	"github.com/Neirda24/envoke/internal/trust"
)

// isolateStore points the trust store at a fresh temp dir, so a Decide test
// sees only what it set up itself.
func isolateStore(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// resolvedTempDir is t.TempDir() with symlinks resolved. The fragment walk
// resolves its root before walking, so an unresolved fixture path can never
// match what Load reports back: macOS hands out /var/..., a symlink to
// /private/var, and a Windows runner's %TMP% is normally the 8.3 short form
// (C:\Users\RUNNER~1). Any test comparing a path it built against one Load
// returned has to start from the resolved spelling.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func paths(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func TestLoad_CentralConfigFirstThenFragmentsInOrder(t *testing.T) {
	root := resolvedTempDir(t)
	central := writeFile(t, filepath.Join(root, "envokerc"), "enter /a\n\techo central\n")
	dir := filepath.Join(root, "envokerc.d")
	second := writeFile(t, filepath.Join(dir, "20-second"), "enter /b\n\techo second\n")
	first := writeFile(t, filepath.Join(dir, "10-first"), "enter /c\n\techo first\n")

	entries := Load(central, dir)

	got := paths(entries)
	want := []string{central, first, second}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entries[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if entries[0].Fragment {
		t.Errorf("the central config must not be marked a fragment")
	}
	if !entries[1].Fragment {
		t.Errorf("a file in envokerc.d must be marked a fragment")
	}
}

func TestLoad_NoFragmentDirectoryIsFine(t *testing.T) {
	root := t.TempDir()
	central := writeFile(t, filepath.Join(root, "envokerc"), "enter /a\n\techo hi\n")

	if got := paths(Load(central, "")); len(got) != 1 || got[0] != central {
		t.Errorf("entries = %v, want just the central config", got)
	}
	if got := paths(Load("", filepath.Join(root, "missing"))); len(got) != 0 {
		t.Errorf("entries = %v, want none", got)
	}
}

// One unparseable fragment must not take the others down with it: with a
// symlinked fragment, the file that breaks may have been rewritten by someone
// else's commit.
func TestLoad_BrokenFragmentIsIsolated(t *testing.T) {
	dir := resolvedTempDir(t)
	broken := writeFile(t, filepath.Join(dir, "10-broken"), "this is not a block\n")
	good := writeFile(t, filepath.Join(dir, "20-good"), "enter /a\n\techo fine\n")

	entries := Load("", dir)
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want both files reported", paths(entries))
	}
	if entries[0].Path != broken || entries[0].Err == nil {
		t.Errorf("expected %s to report a parse error, got %+v", broken, entries[0])
	}
	if entries[1].Path != good || entries[1].Err != nil {
		t.Errorf("expected %s to load cleanly, got %+v", good, entries[1])
	}
	if cfgs := Configs(entries); len(cfgs) != 1 {
		t.Errorf("Configs = %d, want only the one that loaded", len(cfgs))
	}
}

// A fragment that is a symlink into a project is confined to that project: it
// is content someone else's commit can rewrite, and while trust still gates
// every change, a config that travels with a repository has no business
// matching outside it.
func TestLoad_SymlinkedFragmentIsConfinedToItsProject(t *testing.T) {
	project := t.TempDir()
	dir := t.TempDir()

	target := writeFile(t, filepath.Join(project, "envoke.conf"), "enter ./src\n\techo hi\n")
	if err := os.Symlink(target, filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entries := Load("", dir)
	if len(entries) != 1 || entries[0].Err != nil {
		t.Fatalf("entries = %+v", entries)
	}
	cfg := entries[0].Config
	if !cfg.Local {
		t.Errorf("a fragment pointing out of the config directory must be confined")
	}

	resolved, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if cfg.Dir != resolved {
		t.Errorf("Dir = %q, want the project %q", cfg.Dir, resolved)
	}
}

// A fragment that really lives in the config directory is the user's own, and
// is not confined.
func TestLoad_PlainFragmentIsNotConfined(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "10-mine"), "enter /anywhere\n\techo hi\n")

	entries := Load("", dir)
	if len(entries) != 1 || entries[0].Err != nil {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Config.Local {
		t.Errorf("a fragment inside the config directory must not be confined")
	}
}

// TestLoad_SymlinkedConfigDirectoryIsNotConfined covers the normal dotfiles
// layout: the whole config directory is a symlink into a dotfiles repo. Every
// fragment then resolves outside its own literal path, and confining them all
// would break every pattern the user wrote.
func TestLoad_SymlinkedConfigDirectoryIsNotConfined(t *testing.T) {
	dotfiles := t.TempDir()
	real := filepath.Join(dotfiles, "envokerc.d")
	writeFile(t, filepath.Join(real, "10-mine"), "enter /anywhere\n\techo hi\n")

	link := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entries := Load("", link)
	if len(entries) != 1 || entries[0].Err != nil {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Config.Local {
		t.Errorf("a symlinked config directory must not confine the fragments inside it")
	}
}

// The same file reachable as both the central config and a fragment would
// otherwise fire every one of its blocks twice per cd. The dedup keys on the
// resolution, so it holds whichever of the two paths is the symlink.
func TestLoad_SameFileIsNotLoadedTwice(t *testing.T) {
	const body = "enter /a\n\techo once\n"

	t.Run("a fragment links back at the central config", func(t *testing.T) {
		root := t.TempDir()
		central := writeFile(t, filepath.Join(root, "envokerc"), body)
		dir := filepath.Join(root, "envokerc.d")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.Symlink(central, filepath.Join(dir, "central")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		entries := Load(central, dir)
		if got := paths(entries); len(got) != 1 || got[0] != central {
			t.Fatalf("entries = %v, want just %s", got, central)
		}
		// One entry, one copy of the block: two entries would run the same
		// script twice on every matching cd.
		if got := len(Configs(entries)[0].Blocks); got != 1 {
			t.Errorf("the loaded config has %d blocks, want 1", got)
		}
	})

	// The reverse: the central config is the link and the fragment is the file
	// it points at. The central config still wins, since it is loaded first.
	t.Run("the central config links at a fragment", func(t *testing.T) {
		dir := t.TempDir()
		fragment := writeFile(t, filepath.Join(dir, "10-mine"), body)

		central := filepath.Join(t.TempDir(), "envokerc")
		if err := os.Symlink(fragment, central); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		if got := paths(Load(central, dir)); len(got) != 1 || got[0] != central {
			t.Errorf("entries = %v, want just %s", got, central)
		}
	})
}

func TestDecide(t *testing.T) {
	content := []byte("enter /a\n\techo hi\n")

	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, path string)
		want  Decision
	}{
		{
			name:  "never seen",
			setup: func(*testing.T, string) {},
			want:  Untrusted,
		},
		{
			name: "approved against this content",
			setup: func(t *testing.T, path string) {
				if err := trust.Allow(path, content); err != nil {
					t.Fatalf("Allow: %v", err)
				}
			},
			want: Run,
		},
		{
			name: "approved against different content",
			setup: func(t *testing.T, path string) {
				if err := trust.Allow(path, []byte("enter /a\n\techo something else\n")); err != nil {
					t.Fatalf("Allow: %v", err)
				}
			},
			want: Untrusted,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isolateStore(t)
			path := writeFile(t, filepath.Join(t.TempDir(), "fragment"), string(content))
			tt.setup(t, path)

			got, err := Decide(Entry{Path: path, Fragment: true, Content: content})
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got != tt.want {
				t.Errorf("Decide = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecide_FailedEntryNeedsNoTrustLookup(t *testing.T) {
	isolateStore(t)

	got, err := Decide(Entry{Path: "/nope/fragment", Err: os.ErrNotExist})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got != Failed {
		t.Errorf("Decide = %v, want Failed", got)
	}
}

func TestByConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "10-mine"), "enter /a\n\techo hi\n")

	entries := Load("", dir)
	byConfig := ByConfig(entries)
	if len(byConfig) != 1 {
		t.Fatalf("ByConfig has %d entries, want 1", len(byConfig))
	}
	if got := byConfig[entries[0].Config]; got.Path != entries[0].Path {
		t.Errorf("ByConfig lost the entry for %s", entries[0].Path)
	}
}

// A fragment that isn't a symlink still gets relative patterns, resolved
// against the config directory it sits in.
func TestLoad_RelativePatternInAPlainFragment(t *testing.T) {
	dir := resolvedTempDir(t)
	writeFile(t, filepath.Join(dir, "10-mine"), "enter ./sub\n\techo hi\n")

	entries := Load("", dir)
	if len(entries) != 1 || entries[0].Err != nil {
		t.Fatalf("entries = %+v", entries)
	}
	want := filepath.ToSlash(filepath.Join(dir, "sub"))
	pattern := entries[0].Config.Blocks[0].Pattern
	if !pattern.MatchString(want) {
		t.Errorf("pattern %v should match %q", pattern, want)
	}
	if strings.Contains(pattern.String(), "./sub") {
		t.Errorf("the relative prefix should have been resolved away, got %v", pattern)
	}
}

// The confinement rule as a table, because the case that matters cannot be
// provoked through Load: it needs a symlink the kernel reads through and
// filepath.EvalSymlinks refuses to follow, which is a Windows reparse-point
// shape rather than anything a portable test can build. The rule is
// exercised here and the flag that feeds it in TestLoadFragment_* over in
// internal/config.
func TestConfine(t *testing.T) {
	root := filepath.Join("/home/you/.config/envoke", "envokerc.d")

	tests := []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{
			name: "a file that really lives in the config directory",
			cfg:  config.Config{Dir: root},
			want: false,
		},
		{
			name: "a file in a subdirectory of it",
			cfg:  config.Config{Dir: filepath.Join(root, "work")},
			want: false,
		},
		{
			name: "a symlink into a project",
			cfg:  config.Config{Dir: "/home/you/work/api"},
			want: true,
		},
		{
			name: "a link that could not be resolved, which looks like it lives here",
			cfg:  config.Config{Dir: root, DirUnresolved: true},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := confine(&tc.cfg, root); got != tc.want {
				t.Errorf("confine(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

// BenchmarkLoad is here to keep the per-`cd` cost of the fragment directory
// visible. Load runs on every directory change and opens, parses and compiles
// every file in the set, so the number that matters is per fragment, not per
// call:
//
//	go test ./internal/configset -bench Load -benchtime 200x
//
// Nothing here asserts a threshold — a benchmark that fails on a slow runner
// is noise. It exists so that "how expensive is another fragment?" has an
// answer someone can measure rather than guess at, given the rest of the
// codebase declines a second stat per config on this same path.
func BenchmarkLoad(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("%d-fragments", n), func(b *testing.B) {
			dir := b.TempDir()
			for i := range n {
				path := filepath.Join(dir, fmt.Sprintf("%03d-frag", i))
				body := fmt.Sprintf("enter /work/p%d\n    export P=%d\n", i, i)
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					b.Fatalf("WriteFile: %v", err)
				}
			}

			b.ResetTimer()
			for range b.N {
				for _, e := range Load("", dir) {
					if e.Err != nil {
						b.Fatalf("Load: %v", e.Err)
					}
				}
			}
		})
	}
}

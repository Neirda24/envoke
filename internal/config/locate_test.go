package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateEnv points the home directory at a fresh temp dir and clears the
// env vars Locate consults, so each test starts from a known-empty state.
//
// Both HOME and USERPROFILE are set because os.UserHomeDir reads a
// different one per platform (USERPROFILE on Windows, HOME elsewhere).
// Setting only HOME would leave these tests quietly pointing at the real
// home directory on Windows.
func isolateEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ENVOKERC", "")
	t.Setenv("ENVOKERC_D", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

// absTestPath makes a Unix-style literal absolute on this platform, the same
// trick internal/matcher's tp uses -- Locate hands $ENVOKERC back verbatim,
// but a test asserting on it should still use a path that is plausible for
// the platform it runs on.
func absTestPath(p string) string {
	if runtime.GOOS == "windows" {
		return "C:" + p
	}
	return p
}

func TestLocate_EnvokercEnvVarWinsEvenIfMissing(t *testing.T) {
	isolateEnv(t)
	want := absTestPath("/somewhere/custom-config")
	t.Setenv("ENVOKERC", want)

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_DefaultDotfileWhenPresent(t *testing.T) {
	home := isolateEnv(t)
	writeEmptyFile(t, filepath.Join(home, ".envokerc"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(home, ".envokerc")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_XDGConfigHomeWhenDotfileMissing(t *testing.T) {
	isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeEmptyFile(t, filepath.Join(xdg, "envoke", "config"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(xdg, "envoke", "config")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_DefaultXDGPathWhenXDGConfigHomeUnset(t *testing.T) {
	home := isolateEnv(t)
	writeEmptyFile(t, filepath.Join(home, ".config", "envoke", "config"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(home, ".config", "envoke", "config")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_DotfileTakesPrecedenceOverXDG(t *testing.T) {
	home := isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeEmptyFile(t, filepath.Join(home, ".envokerc"))
	writeEmptyFile(t, filepath.Join(xdg, "envoke", "config"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(home, ".envokerc")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_NothingFound(t *testing.T) {
	home := isolateEnv(t)

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if found {
		t.Errorf("expected found=false, got path %q", path)
	}
	want := filepath.Join(home, ".envokerc")
	if path != want {
		t.Errorf("path = %q, want default %q", path, want)
	}
}

func writeEmptyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLocateDir_PrefersDotEnvokercDOverXDG(t *testing.T) {
	home := isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	mkdir(t, filepath.Join(xdg, "envoke", DirName))
	if path, found, err := LocateDir(); err != nil || !found || path != filepath.Join(xdg, "envoke", DirName) {
		t.Fatalf("LocateDir = %q, %v, %v; want the XDG directory", path, found, err)
	}

	// ~/.envokerc.d wins once it exists, mirroring Locate's order.
	mkdir(t, filepath.Join(home, "."+DirName))
	path, found, err := LocateDir()
	if err != nil || !found {
		t.Fatalf("LocateDir = %q, %v, %v", path, found, err)
	}
	if want := filepath.Join(home, "."+DirName); path != want {
		t.Errorf("LocateDir = %q, want %q", path, want)
	}
}

// $ENVOKERC_D is used verbatim even when it doesn't exist, exactly as
// $ENVOKERC is: an explicit override means "use this", not "use this if
// convenient".
func TestLocateDir_EnvOverrideIsUsedVerbatim(t *testing.T) {
	isolateEnv(t)
	t.Setenv("ENVOKERC_D", "/nowhere/fragments")

	path, found, err := LocateDir()
	if err != nil {
		t.Fatalf("LocateDir: %v", err)
	}
	if !found || path != "/nowhere/fragments" {
		t.Errorf("LocateDir = %q, %v; want the override used as given", path, found)
	}
}

func TestLocateDir_NothingIsNotAnError(t *testing.T) {
	isolateEnv(t)
	if _, found, err := LocateDir(); err != nil || found {
		t.Errorf("LocateDir = found %v, err %v; want a clean not-found", found, err)
	}
}

// A file is only a fragment if it can be read as one, and the order is what
// makes filename prefixes mean anything.
func TestFragments_OrdersByPathRelativeToTheDirectory(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "a"))
	mkdir(t, filepath.Join(dir, "a", "deep"))
	for _, name := range []string{"20-second", "10-first", "a/b.conf", "a.conf", "a/deep/c.conf", "a/a.conf"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), "enter /x\n\techo hi\n")
	}

	root, got, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}

	// Sorted on the relative path, so "a.conf" comes before "a/b.conf" —
	// which a plain directory walk gets backwards, since it descends into "a"
	// before reaching "a.conf".
	want := []string{"10-first", "20-second", "a.conf", "a/a.conf", "a/b.conf", "a/deep/c.conf"}
	if len(got) != len(want) {
		t.Fatalf("Fragments = %v, want %d entries", got, len(want))
	}
	for i, w := range want {
		if rel, _ := filepath.Rel(root, got[i]); filepath.ToSlash(rel) != w {
			t.Errorf("Fragments[%d] = %q, want %q", i, rel, w)
		}
	}
}

// The returned root is the resolved directory, because the paths are resolved
// too and a caller comparing the two — configset, deciding what to confine —
// would otherwise find every fragment outside the directory it walked.
func TestFragments_ReturnsTheResolvedRoot(t *testing.T) {
	real := filepath.Join(t.TempDir(), "envokerc.d")
	mkdir(t, real)
	writeFile(t, filepath.Join(real, "10-mine"), "enter /x\n\techo hi\n")

	link := filepath.Join(t.TempDir(), "envokerc.d")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	root, got, err := Fragments(link)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if root != resolved {
		t.Errorf("root = %q, want the resolved directory %q", root, resolved)
	}
	if len(got) != 1 {
		t.Fatalf("Fragments = %v, want the one fragment", got)
	}
	if filepath.Dir(got[0]) != root {
		t.Errorf("Fragments[0] = %q, which is not under the returned root %q", got[0], root)
	}
}

// A root that cannot be resolved is reported as the directory it was asked
// about, never as "": the caller confines fragments against it, and "" is not a
// bound. Both fallbacks -- the directory does not exist, and it exists but will
// not resolve -- answer the same way.
func TestFragments_UnresolvableRootFallsBackToTheDirectoryAsGiven(t *testing.T) {
	t.Run("does not exist", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nope")

		root, got, err := Fragments(dir)
		if err != nil {
			t.Fatalf("Fragments: %v", err)
		}
		if root != dir {
			t.Errorf("root = %q, want the directory as given %q", root, dir)
		}
		if len(got) != 0 {
			t.Errorf("Fragments = %v, want none", got)
		}
	})

	t.Run("exists but will not resolve", func(t *testing.T) {
		// A symlink pointing at itself: the kernel refuses to resolve it, with
		// an error that is not "does not exist".
		loop := filepath.Join(t.TempDir(), "loop")
		if err := os.Symlink(loop, loop); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		root, _, err := Fragments(loop)
		if err != nil {
			t.Fatalf("Fragments: %v", err)
		}
		if root != loop {
			t.Errorf("root = %q, want the directory as given %q", root, loop)
		}
	})
}

func TestFragments_SkipsEditorDroppings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real"), "enter /x\n\techo hi\n")
	writeFile(t, filepath.Join(dir, ".hidden"), "enter /x\n\techo hi\n")
	writeFile(t, filepath.Join(dir, "backup~"), "enter /x\n\techo hi\n")
	mkdir(t, filepath.Join(dir, ".git"))
	writeFile(t, filepath.Join(dir, ".git", "config"), "enter /x\n\techo hi\n")

	_, got, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "real" {
		t.Errorf("Fragments = %v, want just the one real fragment", got)
	}
}

func TestFragments_MissingDirectoryIsEmptyNotAnError(t *testing.T) {
	_, got, err := Fragments(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Fragments = %v, want none", got)
	}
}

// A symlinked fragment is how a config committed inside a project joins the
// set. Its "./" has to mean the project, not the directory the link sits in.
func TestLoadFragment_ResolvesSymlinksForTheRelativeBase(t *testing.T) {
	project := t.TempDir()
	fragments := t.TempDir()

	target := filepath.Join(project, "envoke.conf")
	writeFile(t, target, "enter ./src\n\techo hi\n")

	link := filepath.Join(fragments, "project")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cfg, content, err := loadFragment(link)
	if err != nil {
		t.Fatalf("loadFragment: %v", err)
	}
	if string(content) == "" {
		t.Errorf("expected the target's content to be read through the link")
	}
	// Path stays the link: that is what the user controls and what the trust
	// record is keyed on.
	if abs, _ := filepath.Abs(link); cfg.Path != abs {
		t.Errorf("Path = %q, want the link %q", cfg.Path, abs)
	}

	resolved, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.ToSlash(filepath.Join(resolved, "src"))
	if !cfg.Blocks[0].Pattern.MatchString(want) {
		t.Errorf("pattern %v should match %q — ./ must resolve against the target", cfg.Blocks[0].Pattern, want)
	}
}

// LoadFragmentResolved is handed the resolution its caller already performed,
// so the file's symlinks are followed once per directory change rather than
// once per thing that needs them. The two answers it accepts have to mean
// exactly what loadFragment's own resolution would have meant.
func TestLoadFragmentResolved_UsesTheResolutionItIsGiven(t *testing.T) {
	project := t.TempDir()
	fragments := t.TempDir()

	target := filepath.Join(project, "envoke.conf")
	writeFile(t, target, "enter ./src\n\techo hi\n")

	link := filepath.Join(fragments, "project")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	t.Run("a resolution given is the base", func(t *testing.T) {
		cfg, content, err := LoadFragmentResolved(link, resolved)
		if err != nil {
			t.Fatalf("LoadFragmentResolved: %v", err)
		}
		if string(content) == "" {
			t.Errorf("expected the target's content to be read through the link")
		}
		if cfg.DirUnresolved {
			t.Errorf("DirUnresolved must be false when the caller resolved the link")
		}
		if want := filepath.Dir(resolved); cfg.Dir != want {
			t.Errorf("Dir = %q, want the target's directory %q", cfg.Dir, want)
		}
		if abs, _ := filepath.Abs(link); cfg.Path != abs {
			t.Errorf("Path = %q, want the link %q", cfg.Path, abs)
		}
	})

	// "" means the caller's EvalSymlinks refused the link, not that there was
	// none to follow: the base falls back to the link's own directory and the
	// config says so, which is what lets configset fail closed and confine it.
	t.Run("no resolution falls back to the link's own directory", func(t *testing.T) {
		cfg, content, err := LoadFragmentResolved(link, "")
		if err != nil {
			t.Fatalf("LoadFragmentResolved: %v", err)
		}
		if string(content) == "" {
			t.Errorf("expected the read through the link to still happen")
		}
		if !cfg.DirUnresolved {
			t.Errorf("DirUnresolved must report that Dir is the link's own directory")
		}
		if want := filepath.Dir(cfg.Path); cfg.Dir != want {
			t.Errorf("Dir = %q, want the link's own directory %q", cfg.Dir, want)
		}
	})
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// A dangling symlink in envokerc.d is kept, not skipped: the user created that
// link on purpose, and silently loading nothing is worse than an error naming
// the file.
func TestFragments_KeepsABrokenSymlinkSoItGetsReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "project")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, got, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Fragments = %v, want the broken link kept", got)
	}
	if _, _, err := loadFragment(got[0]); err == nil {
		t.Errorf("expected loading a broken link to fail loudly")
	}
}

// A symlink to a directory is not a fragment; a fragment is a file.
func TestFragments_SkipsASymlinkedDirectory(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(dir, "adir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, got, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Fragments = %v, want none", got)
	}
}

// The bound exists because every fragment is opened, parsed and hashed on
// every directory change: $ENVOKERC_D pointing at a home directory would put
// that walk in front of every shell prompt. It has to be an error, not a
// truncation -- a config directory that is quietly half-read is exactly the
// failure this is meant to prevent.
func TestFragments_RefusesTooManyFiles(t *testing.T) {
	dir := t.TempDir()
	for i := range maxFragments + 1 {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("%04d-frag", i)), "enter /x\n    echo hi\n")
	}

	_, _, err := Fragments(dir)
	if err == nil {
		t.Fatal("expected an error past the file bound")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("more than %d config files", maxFragments)) {
		t.Errorf("error %q should name the bound it hit", err)
	}
}

func TestFragments_AcceptsExactlyTheBound(t *testing.T) {
	dir := t.TempDir()
	for i := range maxFragments {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("%04d-frag", i)), "enter /x\n    echo hi\n")
	}

	_, paths, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(paths) != maxFragments {
		t.Errorf("got %d fragments, want %d", len(paths), maxFragments)
	}
}

func TestFragments_RefusesDeepNesting(t *testing.T) {
	dir := t.TempDir()
	deep := dir
	for range maxFragmentDepth + 1 {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(deep, "frag"), "enter /x\n    echo hi\n")

	_, _, err := Fragments(dir)
	if err == nil {
		t.Fatal("expected an error past the depth bound")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("more than %d levels deep", maxFragmentDepth)) {
		t.Errorf("error %q should name the bound it hit", err)
	}
}

// A directory tree exactly at the bound still loads, so the bound is off by
// nothing: the deepest allowed fragment is maxFragmentDepth levels down.
func TestFragments_AcceptsNestingUpToTheBound(t *testing.T) {
	dir := t.TempDir()
	deep := dir
	for range maxFragmentDepth {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(deep, "frag"), "enter /x\n    echo hi\n")

	_, paths, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d fragments, want 1", len(paths))
	}
}

// A fragment that is not a regular file is refused before anything opens it.
// Every fragment is read whole on every directory change and before any trust
// decision, so a character device would read until memory ran out and a FIFO
// with no writer would block the prompt forever -- and a symlinked fragment,
// which is how a project's config joins the set, points wherever that project's
// last commit says it does.
func TestFragments_RefusesANonRegularFile(t *testing.T) {
	t.Run("a named pipe", func(t *testing.T) {
		dir := t.TempDir()
		mkfifo(t, filepath.Join(dir, "20-pipe"))

		_, _, err := Fragments(dir)
		if err == nil {
			t.Fatal("expected a FIFO fragment to be refused, not opened")
		}
		if want := walkedPath(t, dir, "20-pipe"); !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "named pipe") {
			t.Errorf("error %q should name %s and what it is", err, want)
		}
	})

	t.Run("a symlink to a character device", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no /dev/zero on Windows")
		}
		dir := t.TempDir()
		link := filepath.Join(dir, "20-project")
		if err := os.Symlink("/dev/zero", link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := os.Stat(link); err != nil {
			t.Skipf("/dev/zero unavailable: %v", err)
		}

		_, _, err := Fragments(dir)
		if err == nil {
			t.Fatal("expected a fragment linked to a device to be refused")
		}
		if want := walkedPath(t, dir, "20-project"); !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "character device") {
			t.Errorf("error %q should name %s and what it points at", err, want)
		}
	})
}

// walkedPath is the path Fragments reports for name under dir: the walk resolves
// the directory first, and t.TempDir hands back an unresolved one on macOS, where
// every temporary directory sits under a symlinked /var.
func walkedPath(t *testing.T, dir, name string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}
	return filepath.Join(resolved, name)
}

// Requiring a regular file sits on the path every `cd` takes, and the feature's
// headline case is a symlinked file: everything that was a fragment before still
// is, including the broken link that is kept so its load can report it.
func TestFragments_KeepsRegularSymlinkedAndBrokenEntries(t *testing.T) {
	dir := t.TempDir()
	project := t.TempDir()

	writeFile(t, filepath.Join(dir, "10-plain"), "enter /x\n\techo hi\n")
	target := filepath.Join(project, "envoke.conf")
	writeFile(t, target, "enter /y\n\techo hi\n")
	if err := os.Symlink(target, filepath.Join(dir, "20-linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(project, "gone"), filepath.Join(dir, "30-broken")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	root, got, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	want := []string{"10-plain", "20-linked", "30-broken"}
	if len(got) != len(want) {
		t.Fatalf("Fragments = %v, want %v", got, want)
	}
	for i, w := range want {
		if rel, _ := filepath.Rel(root, got[i]); filepath.ToSlash(rel) != w {
			t.Errorf("Fragments[%d] = %q, want %q", i, rel, w)
		}
	}

	for _, p := range got[:2] {
		if _, content, err := loadFragment(p); err != nil || len(content) == 0 {
			t.Errorf("loadFragment(%s) = %d bytes, %v; want it to load", p, len(content), err)
		}
	}
	if _, _, err := loadFragment(got[2]); err == nil {
		t.Error("expected the broken link to fail loudly on load")
	}
}

// A directory in envokerc.d is not a fragment, and it is not a failure either:
// it is skipped and every real fragment beside it still loads. The difference
// between those two answers is the whole set -- Fragments' error fails the
// directory, where configset.Entry.Err is per file, so treating a directory as a
// bad fragment takes every project's rules down with it.
//
// The entry this was written for cannot be built here: on Windows a directory
// arrives in this position as a junction, which needs `mklink /J` (see
// TestFragments_FollowsAJunction). A symlink to a directory is the portable
// stand-in, and reaches the same stat-and-decide branch.
func TestFragments_SkipsADirectoryEntryAndKeepsTheRestOfTheSet(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	writeFile(t, filepath.Join(dir, "10-first"), "enter /x\n\techo hi\n")
	writeFile(t, filepath.Join(dir, "30-third"), "enter /z\n\techo hi\n")
	if err := os.Symlink(other, filepath.Join(dir, "20-adir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	root, got, err := Fragments(dir)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	want := []string{"10-first", "30-third"}
	if len(got) != len(want) {
		t.Fatalf("Fragments = %v, want %v -- a directory-like entry must not fail the set", got, want)
	}
	for i, w := range want {
		if rel, _ := filepath.Rel(root, got[i]); filepath.ToSlash(rel) != w {
			t.Errorf("Fragments[%d] = %q, want %q", i, rel, w)
		}
	}
}

// $ENVOKERC_D is honoured verbatim, so it can name something that is not a
// directory at all, and the error then has to be about the directory. Left to
// the walk, that one path reaches the half of the callback that judges
// fragments: it was reported as a bad *fragment*, or -- for the dotted default
// name -- skipped, leaving no fragments and no error.
func TestFragments_RefusesARootThatIsNotADirectory(t *testing.T) {
	t.Run("a regular file", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, DirName)
		writeFile(t, file, "enter /x\n\techo hi\n")

		root, got, err := Fragments(file)
		if err == nil {
			t.Fatalf("Fragments = %v, want a file named as the fragment directory refused", got)
		}
		if want := walkedPath(t, dir, DirName); !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name %s", err, want)
		}
		if !strings.Contains(err.Error(), "a regular file") || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("error %q should say what it found and what was wanted", err)
		}
		if root == "" {
			t.Error("root must never be empty: a caller confines fragments against it")
		}
	})

	// A FIFO is what the walk's own type check exists for, one level up. The root
	// is stat'd and never opened, so this reports rather than blocking every
	// prompt on an open(2) that has no writer to return from.
	t.Run("a named pipe", func(t *testing.T) {
		dir := t.TempDir()
		mkfifo(t, filepath.Join(dir, DirName))

		_, _, err := Fragments(filepath.Join(dir, DirName))
		if err == nil {
			t.Fatal("expected a FIFO named as the fragment directory to be refused, not opened")
		}
		if !strings.Contains(err.Error(), "a named pipe") || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("error %q should say what it found and what was wanted", err)
		}
	})
}

// A junction is how a directory reaches either position on Windows without
// administrator rights, and Go describes it as fs.ModeIrregular rather than
// fs.ModeSymlink, so neither filepath.EvalSymlinks nor filepath.WalkDir follows
// one. This is the only test that exercises either case end to end, and it runs
// on exactly one platform.
func TestFragments_FollowsAJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a junction cannot be created off Windows; the mode handling is covered portably by the symlinked-directory cases, the end-to-end path is not")
	}

	t.Run("as the config directory itself", func(t *testing.T) {
		real := t.TempDir()
		writeFile(t, filepath.Join(real, "10-mine"), "enter /x\n\techo hi\n")
		link := filepath.Join(t.TempDir(), DirName)
		mklinkJunction(t, link, real)

		root, got, err := Fragments(link)
		if err != nil {
			t.Fatalf("Fragments: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("Fragments = %v, want the one fragment behind the junction", got)
		}
		resolved, err := filepath.EvalSymlinks(real)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if root != resolved {
			t.Errorf("root = %q, want the directory the junction names, %q", root, resolved)
		}
		if filepath.Dir(got[0]) != root {
			t.Errorf("Fragments[0] = %q, which is not under the returned root %q", got[0], root)
		}
	})

	t.Run("as an entry inside it", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "10-mine"), "enter /x\n\techo hi\n")
		mklinkJunction(t, filepath.Join(dir, "20-junction"), t.TempDir())

		_, got, err := Fragments(dir)
		if err != nil {
			t.Fatalf("Fragments: %v", err)
		}
		if len(got) != 1 || filepath.Base(got[0]) != "10-mine" {
			t.Errorf("Fragments = %v, want the junction skipped and the fragment kept", got)
		}
	})
}

// mklinkJunction makes a directory junction, or skips. mklink is a cmd builtin
// rather than a program, and /J is the form that needs no elevation -- which is
// why a Windows user has junctions where this package's other tests use
// os.Symlink.
func mklinkJunction(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows-only")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
}

// mkfifo makes a named pipe, or skips. It shells out rather than calling
// syscall.Mkfifo because that function is not declared on Windows, where this
// package's tests are also built -- a runtime skip would not save the compile.
func mkfifo(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no FIFOs; this case is unix-only")
	}
	bin, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	if out, err := exec.Command(bin, path).CombinedOutput(); err != nil {
		t.Skipf("mkfifo %s: %v: %s", path, err, out)
	}
}

func TestDepthUnder(t *testing.T) {
	root := filepath.Join("a", "b")
	for _, tc := range []struct {
		path string
		want int
	}{
		{root, 0},
		{filepath.Join(root, "x"), 1},
		{filepath.Join(root, "x", "y"), 2},
	} {
		if got := depthUnder(root, tc.path); got != tc.want {
			t.Errorf("depthUnder(%s, %s) = %d, want %d", root, tc.path, got, tc.want)
		}
	}
}

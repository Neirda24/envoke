package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Locate resolves the path to the user's envoke config, in order:
//
//  1. $ENVOKERC, if set — used verbatim, even if the file doesn't exist yet.
//  2. ~/.envokerc, if it exists (the documented default).
//  3. $XDG_CONFIG_HOME/envoke/config (or ~/.config/envoke/config), if it
//     exists.
//
// found is false when none of the above exist and $ENVOKERC wasn't set —
// a normal "nothing to load" state, not an error. path is still set in that
// case (the ~/.envokerc default) so callers can use it in messages.
func Locate() (path string, found bool, err error) {
	if p := os.Getenv("ENVOKERC"); p != "" {
		return p, true, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("locate config: %w", err)
	}

	def := filepath.Join(home, ".envokerc")
	if fileExists(def) {
		return def, true, nil
	}

	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	xdgPath := filepath.Join(xdg, "envoke", "config")
	if fileExists(xdgPath) {
		return xdgPath, true, nil
	}

	return def, false, nil
}

// DirName is the fragment directory's basename: config files loaded together
// with (not instead of) the central config.
const DirName = "envokerc.d"

// LocateDir resolves the fragment directory, mirroring Locate's order over
// $ENVOKERC_D, ~/.envokerc.d and $XDG_CONFIG_HOME/envoke/envokerc.d. The two
// are complementary, not alternatives: both are consulted on every directory
// change.
func LocateDir() (path string, found bool, err error) {
	if p := os.Getenv("ENVOKERC_D"); p != "" {
		return p, true, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("locate config dir: %w", err)
	}

	def := filepath.Join(home, "."+DirName)
	if dirExists(def) {
		return def, true, nil
	}

	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	xdgPath := filepath.Join(xdg, "envoke", DirName)
	if dirExists(xdgPath) {
		return xdgPath, true, nil
	}

	return def, false, nil
}

// Fragments lists the config files under dir, recursively, ordered by their
// path relative to dir — the ordering that makes "10-"/"20-" filename
// prefixes decide precedence. It has to be computed explicitly: a plain walk
// orders "a/b.txt" before "a.txt".
//
// root is the directory the walk used. The paths reported are resolved, so a
// caller comparing them against the directory that holds them (configset,
// deciding whether a fragment merely points into it) needs the same
// resolution. It is never empty; when dir cannot be resolved, or does not
// exist, it is dir as given.
//
// Skipped: names starting with "." or ending with "~", and anything that
// turns out to be a directory. Symlinked *files* are kept — that is how a
// config committed inside a project gets into the set. Refused, rather than
// skipped: an entry that is neither a directory nor a regular file, and a dir
// that is not a directory at all.
//
// A directory that doesn't exist yields no fragments and no error.
func Fragments(dir string) (root string, paths []string, err error) {
	// filepath.WalkDir does not follow a symlink, including the root it is
	// handed, so walking dir directly finds nothing when the config directory
	// is itself a link into a dotfiles repository.
	root, err = filepath.EvalSymlinks(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dir, nil, nil
		}
		root = dir
	}
	root, err = walkableRoot(root)
	if err != nil {
		return root, nil, err
	}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if p != root && skipFragment(d.Name()) {
				return fs.SkipDir
			}
			if depthUnder(root, p) > maxFragmentDepth {
				return fmt.Errorf("%s is more than %d levels deep; this directory is walked on every directory change, so it is bounded -- flatten it, or point $ENVOKERC_D at the subdirectory you meant", p, maxFragmentDepth)
			}
			return nil
		}
		if skipFragment(d.Name()) {
			return nil
		}
		if len(paths) >= maxFragments {
			return fmt.Errorf("more than %d config files; this directory is walked on every directory change, so it is bounded -- $ENVOKERC_D is probably pointing at something larger than a config directory", maxFragments)
		}
		// What the stat reports decides, never the label the entry arrived
		// under: Go reports a Windows junction (what `mklink /J` makes, and it
		// needs no elevation) as fs.ModeIrregular, setting fs.ModeSymlink for
		// IO_REPARSE_TAG_SYMLINK alone.
		//
		// A directory is skipped. A *broken* link is deliberately kept, so the
		// load fails and says so rather than silently ignoring a project
		// config the user went out of their way to link in.
		mode := d.Type()
		if !mode.IsRegular() {
			info, err := os.Stat(p)
			if err != nil {
				paths = append(paths, p)
				return nil
			}
			if info.IsDir() {
				return nil
			}
			mode = info.Mode()
		}
		// Settled here, before anything opens the file, because opening is
		// itself what blocks: open(2) on a FIFO with no writer never returns,
		// and a device would read until memory ran out — both in front of
		// every shell prompt.
		if !mode.IsRegular() {
			return fmt.Errorf("%s is %s; a config fragment has to be a regular file, and every one of them is read on every directory change -- remove it, or point it at the config file you meant", p, fileTypeName(mode))
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return root, nil, nil
		}
		return root, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	// Each key is computed once. Inside the comparator, filepath.Rel would run
	// and allocate O(n log n) times per directory change for the same n keys.
	type keyed struct {
		key  string
		path string
	}
	byKey := make([]keyed, len(paths))
	for i, p := range paths {
		byKey[i] = keyed{key: fragmentKey(root, p), path: p}
	}
	sort.Slice(byKey, func(i, j int) bool { return byKey[i].key < byKey[j].key })
	for i, k := range byKey {
		paths[i] = k.path
	}
	return root, paths, nil
}

// maxFragments and maxFragmentDepth bound the fragment walk, and
// maxConfigBytes bounds one config file. Every one of these files is opened,
// parsed and hashed on every directory change, and nothing in the format
// limits their number or size — an $ENVOKERC_D pointing at a home directory
// would put a whole-tree walk in front of every shell prompt. The bounds are
// far above any real config directory and are reported rather than applied
// silently: half a config is half of the scripts that were meant to run.
const (
	maxFragments     = 512
	maxFragmentDepth = 8
	maxConfigBytes   = 1024 * 1024
)

// walkableRoot is the directory to hand the fragment walk: root itself for an
// ordinary directory, and what it names for a directory reachable only
// through something filepath.EvalSymlinks declined to follow.
//
// A Windows junction is that case, and it is the ordinary dotfiles layout
// there. EvalSymlinks steps over a junction and leaves it in the path;
// filepath.WalkDir then lstats that root and calls it a file, so the config
// directory arrives at the file half of the callback, where its leading "."
// gets it skipped — the user's whole fragment set silently does not exist.
//
// os.Stat settles it: it reopens a name surrogate without
// FILE_FLAG_OPEN_REPARSE_POINT and describes the target. Neither stat is
// reached for a root the walk could have entered by itself.
func walkableRoot(root string) (string, error) {
	lst, err := os.Lstat(root)
	if err != nil || lst.IsDir() {
		// Ordinary, or unreadable: an error is left to the walk, which lstats
		// the root itself and reports it against the path either way.
		return root, nil
	}

	info, err := os.Stat(root)
	if err != nil {
		// A link that will not follow — a loop, or a target that has gone.
		return root, nil
	}
	if !info.IsDir() {
		return root, fmt.Errorf("%s is %s, not a directory of config fragments; point $ENVOKERC_D at a directory, or $ENVOKERC at a single config file", root, fileTypeName(info.Mode()))
	}

	target, err := os.Readlink(root)
	if err != nil {
		return root, fmt.Errorf("cannot follow %s to the directory it names: %w", root, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(root), target)
	}
	// The walk reports resolved paths and a caller compares them against the
	// root returned with them, so the target is resolved in its turn.
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		return resolved, nil
	}
	return target, nil
}

// fileTypeName names what a file is for an error message a reader can act on.
// fs.FileMode's own String answers "why was this refused" with a permission
// bitmap ("p---------"), which is not an answer.
func fileTypeName(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeDir != 0:
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeCharDevice != 0:
		return "a character device"
	case mode&fs.ModeDevice != 0:
		return "a block device"
	case mode.IsRegular():
		// Only walkableRoot reaches this: for a fragment, being a regular file
		// is the thing that was wanted.
		return "a regular file"
	default:
		return "not a regular file"
	}
}

// depthUnder is how many levels below root p sits: 0 for root itself, 1 for
// its direct children.
func depthUnder(root, p string) int {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func fragmentKey(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func skipFragment(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

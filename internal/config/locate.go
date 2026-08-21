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
//  1. $ENVOKERC, if set — used verbatim, even if the file doesn't exist yet
//     (an explicit override means "use this path", not "use this path if
//     convenient").
//  2. ~/.envokerc, if it exists (the documented default).
//  3. $XDG_CONFIG_HOME/envoke/config (or ~/.config/envoke/config if
//     XDG_CONFIG_HOME isn't set), if it exists.
//
// found is false when none of the above exist and $ENVOKERC wasn't set —
// that's a normal "nothing to load" state, not an error. path is still set
// in that case (the ~/.envokerc default) so callers can use it in messages.
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

// DirName is the fragment directory's basename: a directory of config files
// that are loaded together with (not instead of) the central config, so rules
// can be split per project instead of accumulating in one file.
const DirName = "envokerc.d"

// LocateDir resolves the fragment directory, mirroring Locate's order:
//
//  1. $ENVOKERC_D, if set — used verbatim, even if it doesn't exist yet.
//  2. ~/.envokerc.d, if it exists.
//  3. $XDG_CONFIG_HOME/envoke/envokerc.d (or ~/.config/envoke/envokerc.d),
//     if it exists.
//
// found is false when none exist and $ENVOKERC_D wasn't set — a normal "no
// fragments" state, not an error. Both this and Locate are consulted on every
// directory change: the two are complementary, not alternatives.
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
// path relative to dir. That ordering is the whole point of the directory:
// it is what makes "10-", "20-" filename prefixes decide which fragment's
// blocks come first, and it has to be computed explicitly — a plain directory
// walk orders "a/b.txt" before "a.txt", which is not what a reader sorting the
// names in their head would predict.
//
// root is the directory the walk used, returned because a caller needs the same
// resolution the walk did: the paths reported here are resolved, so anything
// comparing them against the directory that holds them — configset, deciding
// whether a fragment merely points into it — has to compare against the
// resolved directory or every fragment reads as pointing out of it. It is never
// empty; when the directory cannot be resolved, or does not exist, it is dir as
// given.
//
// Skipped: names starting with "." and names ending with "~", which between
// them cover editor droppings and swap files; and anything that turns out to be
// a directory, whatever the walk called it, since a fragment is a file.
// Symlinked *files* are kept — that is how a config committed inside a project
// gets into the set, by a deliberate `ln -s` rather than by envoke finding it.
//
// Refused, rather than skipped: an entry that is neither a directory nor a
// regular file, and a dir that is not a directory at all. See the type check
// below and walkableRoot.
//
// A directory that doesn't exist yields no fragments and no error: $ENVOKERC_D
// is honoured verbatim, so pointing it somewhere you haven't created yet is
// ordinary.
//
// The walk is bounded (see maxFragments/maxFragmentDepth) and says so when it
// stops, rather than truncating quietly.
func Fragments(dir string) (root string, paths []string, err error) {
	// The directory is resolved before walking, and the walk reports the
	// resolved paths. filepath.WalkDir does not follow a symlink — including
	// the root it is handed — so walking dir directly finds nothing at all
	// when the config directory is itself a link into a dotfiles repository,
	// which is the normal dotfiles layout.
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
		// Anything WalkDir has not already settled as a regular file needs the
		// extra stat, and what that stat reports is what decides — never the
		// label the entry arrived under, which describes a link rather than its
		// target and is not the same label for every kind of link. Go reports a
		// Windows junction — IO_REPARSE_TAG_MOUNT_POINT, what `mklink /J` makes,
		// the form that needs no elevation — as fs.ModeIrregular, setting
		// fs.ModeSymlink for IO_REPARSE_TAG_SYMLINK alone. Deciding on the label
		// fails the whole set over a directory in the exact position where a
		// symlinked one is silently skipped.
		//
		// A directory is skipped, since a fragment is a file. A *broken* link is
		// deliberately kept, so the load fails and says so: dropping it here
		// would silently ignore a project config the user went out of their way
		// to link in — the worse of the two failures.
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
		// The type is settled here, before anything opens the file, because
		// opening is itself what blocks: open(2) on a FIFO with no writer never
		// returns. Every fragment is read whole before any trust decision is
		// reached, so a device would read until memory ran out and a FIFO would
		// hang, both in front of every shell prompt. It costs no syscall for an
		// ordinary fragment — WalkDir's own Type() answers for a regular file,
		// and the stat above is spent only on the entries that are not one.
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
	// and allocate O(n log n) times per directory change to produce the same n
	// keys. The keys are distinct — distinct paths under one root have distinct
	// relative paths — so the order does not depend on the sort being stable.
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

// maxFragments and maxFragmentDepth bound the fragment walk. Nothing in the
// format limits it otherwise, and every one of these files is opened, parsed
// and hashed on every directory change — so an $ENVOKERC_D pointing at a home
// directory, or at "/", would put a whole-tree walk in front of every shell
// prompt. The bounds are far above any real config directory (a fragment per
// project, grouped a level or two deep) and are reported rather than applied
// silently: a directory quietly half-read is the failure mode this is meant
// to avoid, not the one it should introduce.
//
// maxConfigBytes bounds one config file, central or fragment: the walk counts
// files and says nothing about what is in them, and the format limits a file's
// size no more than it limits their number. The same argument applies a level
// down — a fragment symlinked out of a project is read whole, on every directory
// change, before Decide has looked at it, and its content is whatever that
// project's last commit says. A megabyte is hundreds of times any real config,
// and past it is an error naming the bound rather than a truncated read: half a
// config is half of the scripts that were meant to run.
const (
	maxFragments     = 512
	maxFragmentDepth = 8
	maxConfigBytes   = 1024 * 1024
)

// walkableRoot is the directory to hand the fragment walk for root: root itself
// for an ordinary directory, and what it names for a directory reachable only
// through something filepath.EvalSymlinks declined to follow.
//
// A Windows junction is that case, and it is the ordinary dotfiles layout there,
// because `mklink /J` needs no elevation where `mklink /D` does. Go reports
// IO_REPARSE_TAG_MOUNT_POINT as fs.ModeIrregular, setting fs.ModeSymlink for
// IO_REPARSE_TAG_SYMLINK alone, so EvalSymlinks' own walk steps over a junction
// and leaves it in the path, and filepath.WalkDir then lstats that root and
// calls it a file. The config *directory* arrives at the file half of the walk
// callback, where a name beginning with "." — the default one — is skipped: the
// user's whole fragment set silently does not exist.
//
// os.Stat is what settles it, because it reopens a name surrogate without
// FILE_FLAG_OPEN_REPARSE_POINT and describes the target, while os.Readlink reads
// a junction's target as well as a symlink's. Neither is reached for a root the
// walk could have entered by itself; an ordinary directory costs the one lstat,
// which is also what makes a $ENVOKERC_D naming something that is not a
// directory an error about the directory rather than about a fragment.
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
		// Left to the walk for the same reason.
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
	// root returned with them, so the target is resolved in its turn; it can be
	// reached through links of its own.
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

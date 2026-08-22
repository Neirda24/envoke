package config

import (
	"os"
	"path/filepath"

	"github.com/Neirda24/envoke/internal/fsperm"
)

// UnsafePermissions reports whether the file at path is writable by anyone
// other than its owner. Content-hash revocation stops a *silently* modified
// config from running; this flags the permissions that make the modification
// possible. See internal/fsperm for what the question means per platform —
// notably that Windows cannot answer it.
func UnsafePermissions(path string) (unsafe bool, mode os.FileMode, err error) {
	return fsperm.Unsafe(path)
}

// UnsafeDirPermissions is UnsafePermissions for the directory a config lives
// in, and dir names the directory it looked at.
//
// The stronger of the two signals: whoever can write the directory can rename
// the config away and drop their own in its place, which the file's own mode
// says nothing about. Kept separate because it costs a second stat, which
// only the commands a human is watching are worth spending it on (see
// cmd/envoke's warnUnsafeConfigAndDir).
func UnsafeDirPermissions(path string) (unsafe bool, mode os.FileMode, dir string, err error) {
	dir = filepath.Dir(path)
	unsafe, mode, err = fsperm.Unsafe(dir)
	return unsafe, mode, dir, err
}

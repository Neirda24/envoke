package config

import (
	"os"
	"path/filepath"

	"github.com/Neirda24/envoke/internal/fsperm"
)

// UnsafePermissions reports whether the file at path is writable by anyone
// other than its owner — relevant on shared homes, NFS mounts, or multi-user
// machines, where such a file could be modified by another local user.
// internal/trust's content-hash revocation protects against *silently*
// running a modified config, but nothing else proactively flags that the
// file's permissions make tampering possible in the first place; this is that
// check.
//
// mode is the file's permission bits, for use in a warning message. err is
// non-nil only if the file couldn't be stat'd, in which case unsafe is
// always false. See internal/fsperm for what "writable by anyone else" means
// per platform — notably that Windows cannot answer it.
func UnsafePermissions(path string) (unsafe bool, mode os.FileMode, err error) {
	return fsperm.Unsafe(path)
}

// UnsafeDirPermissions is UnsafePermissions for the directory a config lives
// in, and dir names the directory it looked at.
//
// It is the stronger of the two signals: a config whose own mode is `0644`
// looks safe, but if anyone can write the directory holding it they can
// replace the file wholesale — rename it away and drop their own in its
// place — which the file's own permissions say nothing about.
//
// Kept separate rather than folded into UnsafePermissions because the two
// have different costs. The file check is cheap enough to run on every
// directory change; this one is only worth a syscall in the commands a human
// is watching (see cmd/envoke's warnUnsafeConfigAndDir).
func UnsafeDirPermissions(path string) (unsafe bool, mode os.FileMode, dir string, err error) {
	dir = filepath.Dir(path)
	unsafe, mode, err = fsperm.Unsafe(dir)
	return unsafe, mode, dir, err
}

package config

import "os"

// UnsafePermissions reports whether the file at path is writable by anyone
// other than its owner (group or other write bits set) — relevant on shared
// homes, NFS mounts, or multi-user machines, where such a file could be
// modified by another local user. internal/trust's content-hash revocation
// protects against *silently* running a modified config, but nothing else
// proactively flags that the file's permissions make tampering possible in
// the first place; this is that check.
//
// unsafe is true when mode has the group- or other-write bit set (0o022).
// mode is the file's permission bits, for use in a warning message. err is
// non-nil only if the file couldn't be stat'd, in which case unsafe is
// always false.
func UnsafePermissions(path string) (unsafe bool, mode os.FileMode, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0, err
	}
	perm := info.Mode().Perm()
	return perm&0o022 != 0, perm, nil
}

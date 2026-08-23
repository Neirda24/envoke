// Package fsperm answers one question, in one place: can anyone other than
// the owner write this path?
//
// Three callers ask it — a config file, the directory holding it, and the
// trust store. Spelled inline as `mode&0o022 != 0` it is correct on Unix and
// meaningless on Windows, where Go synthesises the permission word from the
// read-only attribute alone. One build-tagged predicate states the platform
// rule once.
package fsperm

import "os"

// Unsafe reports whether path is writable by a user other than its owner.
// mode is the permission bits as stat'd, for a warning message to quote. err
// is non-nil only when path couldn't be stat'd, and unsafe is false then —
// the caller's own load is what reports a missing or unreadable file.
//
// Always false on Windows; see unsafePerm there.
func Unsafe(path string) (unsafe bool, mode os.FileMode, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0, err
	}
	perm := info.Mode().Perm()
	return unsafePerm(perm), perm, nil
}

// Package fsperm answers one question, in one place: can anyone other than
// the owner write this path?
//
// It exists because the answer is platform-specific and the question is asked
// from three places — a config file, the directory holding it, and the trust
// store — which had each spelled it as `mode&0o022 != 0`. That expression is
// correct on Unix and meaningless on Windows, where Go synthesises the whole
// permission word from a single read-only attribute: os.Stat reports 0666 for
// every writable file and 0777 for every directory, so the group/other bits
// are set on a perfectly ordinary machine. Three copies meant three chances
// to forget that; one build-tagged predicate means the platform rule is
// stated once and tested once.
package fsperm

import "os"

// Unsafe reports whether path is writable by a user other than its owner.
// mode is the permission bits as stat'd, for a warning message to quote. err
// is non-nil only when path couldn't be stat'd, and unsafe is false then —
// "I couldn't look" is not "it's fine", but it is the caller's own load that
// reports a missing or unreadable file, not this.
//
// On Windows this is always false: see the package comment for why, and
// docs/trust.md for what does protect a trust store there.
func Unsafe(path string) (unsafe bool, mode os.FileMode, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0, err
	}
	perm := info.Mode().Perm()
	return unsafePerm(perm), perm, nil
}

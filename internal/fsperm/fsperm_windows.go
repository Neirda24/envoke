//go:build windows

package fsperm

import "os"

// unsafePerm is always false on Windows, because there is nothing here to
// read. Go's os.Stat does not surface the ACL that actually governs access;
// it synthesises the permission word from the read-only attribute alone —
// 0666 for any writable file, 0777 for any directory. Testing 0o022 against
// that reports every config, and the trust store itself, as world-writable,
// which is both wrong and loud: the store warning is on the path every `cd`
// takes, so it would print on every prompt.
//
// A real answer means reading the DACL through advapi32, which needs
// golang.org/x/sys and would end this module's zero-dependency property for
// a warning. Reporting nothing is the honest alternative to reporting
// nonsense; docs/trust.md says so rather than leaving the silence to be
// mistaken for a clean bill of health.
func unsafePerm(os.FileMode) bool {
	return false
}

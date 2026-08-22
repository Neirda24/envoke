//go:build windows

package fsperm

import "os"

// unsafePerm is always false on Windows: there is nothing here to read. Go's
// os.Stat does not surface the ACL that governs access, it synthesises the
// permission word from the read-only attribute alone — 0666 for any writable
// file, 0777 for any directory — so testing 0o022 against it reports every
// config and the trust store as world-writable, on every prompt.
//
// A real answer means reading the DACL through advapi32, which needs
// golang.org/x/sys and would end this module's zero-dependency property for a
// warning. docs/trust.md states the silence so it is not mistaken for a clean
// bill of health.
func unsafePerm(os.FileMode) bool {
	return false
}

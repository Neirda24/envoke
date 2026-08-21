//go:build !windows

package fsperm

import "os"

// unsafePerm is the group-write and other-write bits. Either one means a user
// who is not the owner can rewrite the file, or — for a directory — replace
// what is in it wholesale.
func unsafePerm(perm os.FileMode) bool {
	return perm&0o022 != 0
}

package fsperm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The one test that runs on every platform and asserts a different answer per
// platform, which is the whole point of this package: the Unix bitmask used
// to be inlined at three call sites, and on Windows it reported every file
// and the trust store itself as world-writable. The Windows CI runner covers
// internal/..., so this is what stops that coming back.
func TestUnsafe_PerPlatform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("enter /x\n    echo hi\n"), 0o666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	unsafe, mode, err := Unsafe(path)
	if err != nil {
		t.Fatalf("Unsafe: %v", err)
	}

	if runtime.GOOS == "windows" {
		// Not "0666 happens to be safe" but "0666 is not a fact about this
		// file": Go made it up from the read-only attribute.
		if unsafe {
			t.Errorf("Unsafe reported a file as group/other-writable on Windows (mode %o), where the bits are synthesised", mode)
		}
		return
	}
	if !unsafe {
		t.Errorf("Unsafe(%o) = false, want true", mode)
	}
}

func TestUnsafe_OwnerOnlyIsSafeEverywhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if unsafe, mode, err := Unsafe(path); err != nil {
		t.Fatalf("Unsafe: %v", err)
	} else if unsafe {
		t.Errorf("Unsafe(%o) = true, want false", mode)
	}
}

// A path that isn't there reports the stat error and unsafe=false. Callers
// warn on this; none of them may treat "couldn't look" as a reason to warn.
func TestUnsafe_MissingPath(t *testing.T) {
	unsafe, _, err := Unsafe(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected an error for a missing path")
	}
	if unsafe {
		t.Error("a missing path must not report as unsafe")
	}
}

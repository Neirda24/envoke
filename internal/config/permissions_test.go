package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnsafePermissions_BitmaskCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows has no group/other write bits: Go synthesises 0666 or
		// 0444 from the read-only attribute, so every case here would be
		// meaningless rather than merely different. The warning this backs
		// is a Unix multi-user concern in the first place.
		t.Skip("Unix permission bits are not modelled on Windows")
	}

	tests := []struct {
		name       string
		mode       os.FileMode
		wantUnsafe bool
	}{
		{"owner read-write only", 0o600, false},
		{"owner read-write, group/other read-only", 0o644, false},
		{"group write", 0o664, true},
		{"group write, no group read", 0o620, true},
		{"world write", 0o602, true},
		{"group and world write", 0o666, true},
		{"owner-only, fully open", 0o700, false},
		{"group/other execute but not write", 0o711, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte("enter /a\n    echo hi\n"), tt.mode); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			// WriteFile's mode is subject to umask, so set it explicitly.
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatalf("Chmod: %v", err)
			}

			unsafe, mode, err := UnsafePermissions(path)
			if err != nil {
				t.Fatalf("UnsafePermissions: %v", err)
			}
			if unsafe != tt.wantUnsafe {
				t.Errorf("UnsafePermissions(%s) unsafe = %v, want %v (mode %o)", tt.name, unsafe, tt.wantUnsafe, mode)
			}
			if mode != tt.mode {
				t.Errorf("mode = %o, want %o", mode, tt.mode)
			}
		})
	}
}

func TestUnsafePermissions_MissingFileReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")

	unsafe, _, err := UnsafePermissions(path)
	if err == nil {
		t.Fatalf("expected an error for a missing file")
	}
	if unsafe {
		t.Errorf("unsafe should be false when Stat fails")
	}
}

// The containing directory is the stronger signal: a config whose own mode is
// 0644 looks fine, but anyone who can write the directory can rename it away
// and drop their own file in its place.
func TestUnsafeDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not modelled on Windows")
	}

	for _, tt := range []struct {
		name       string
		mode       os.FileMode
		wantUnsafe bool
	}{
		{"owner only", 0o700, false},
		{"group/other read and execute", 0o755, false},
		{"group write", 0o770, true},
		{"world write", 0o777, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "cfgdir")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatalf("Mkdir: %v", err)
			}
			path := filepath.Join(dir, "config")
			if err := os.WriteFile(path, []byte("enter /a\n\techo hi\n"), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if err := os.Chmod(dir, tt.mode); err != nil {
				t.Fatalf("Chmod: %v", err)
			}

			unsafe, mode, got, err := UnsafeDirPermissions(path)
			if err != nil {
				t.Fatalf("UnsafeDirPermissions: %v", err)
			}
			if unsafe != tt.wantUnsafe {
				t.Errorf("unsafe = %v, want %v (mode %o)", unsafe, tt.wantUnsafe, mode)
			}
			if got != dir {
				t.Errorf("dir = %q, want %q", got, dir)
			}
		})
	}
}

func TestUnsafeDirPermissions_MissingDirReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "config")

	unsafe, _, _, err := UnsafeDirPermissions(path)
	if err == nil {
		t.Fatalf("expected an error for a missing directory")
	}
	if unsafe {
		t.Errorf("unsafe should be false when Stat fails")
	}
}

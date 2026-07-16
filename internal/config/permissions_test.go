package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnsafePermissions_BitmaskCases(t *testing.T) {
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

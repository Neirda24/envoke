package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestIsTrusted_NeverAllowedIsFalse(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	trusted, err := IsTrusted(path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("expected untrusted config to report false")
	}
}

func TestIsTrusted_MissingFileIsFalseNotError(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "does-not-exist")

	trusted, err := IsTrusted(path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("expected missing config to report false")
	}
}

func TestAllow_ThenIsTrusted(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	if err := Allow(path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	trusted, err := IsTrusted(path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if !trusted {
		t.Errorf("expected config to be trusted after Allow")
	}
}

func TestIsTrusted_EditAfterAllowRevokesTrust(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	if err := Allow(path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	// Even a whitespace-only edit must revoke trust.
	writeConfig(t, path, "enter /a\n    echo hi\n\n")

	trusted, err := IsTrusted(path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("expected edited config to be untrusted")
	}
}

func TestAllow_ReapprovingAfterEditRestoresTrust(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")
	if err := Allow(path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	writeConfig(t, path, "enter /a\n    echo bye\n")
	if err := Allow(path); err != nil {
		t.Fatalf("Allow (re-approve): %v", err)
	}

	trusted, err := IsTrusted(path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if !trusted {
		t.Errorf("expected re-approved config to be trusted")
	}
}

func TestTrust_DistinctPathsDoNotCollide(t *testing.T) {
	home := isolateEnv(t)
	pathA := filepath.Join(home, "a", "envokerc")
	pathB := filepath.Join(home, "b", "envokerc")
	if err := os.MkdirAll(filepath.Join(home, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, pathA, "enter /a\n    echo a\n")
	writeConfig(t, pathB, "enter /b\n    echo b\n")

	if err := Allow(pathA); err != nil {
		t.Fatalf("Allow(a): %v", err)
	}

	trustedA, err := IsTrusted(pathA)
	if err != nil {
		t.Fatalf("IsTrusted(a): %v", err)
	}
	trustedB, err := IsTrusted(pathB)
	if err != nil {
		t.Fatalf("IsTrusted(b): %v", err)
	}
	if !trustedA {
		t.Errorf("expected pathA to be trusted")
	}
	if trustedB {
		t.Errorf("expected pathB to remain untrusted (must not share a's record)")
	}
}

func TestAllow_UsesXDGDataHomeWhenSet(t *testing.T) {
	home := isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")
	if err := Allow(path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(xdg, "envoke", "allow"))
	if err != nil {
		t.Fatalf("expected trust record under XDG_DATA_HOME, ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly 1 trust record, got %d", len(entries))
	}
}

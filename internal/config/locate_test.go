package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateEnv points HOME at a fresh temp dir and clears the env vars Locate
// consults, so each test starts from a known-empty state.
func isolateEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENVOKERC", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func TestLocate_EnvokercEnvVarWinsEvenIfMissing(t *testing.T) {
	isolateEnv(t)
	t.Setenv("ENVOKERC", "/somewhere/custom-config")

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if !found || path != "/somewhere/custom-config" {
		t.Errorf("Locate() = (%q, %v), want (/somewhere/custom-config, true)", path, found)
	}
}

func TestLocate_DefaultDotfileWhenPresent(t *testing.T) {
	home := isolateEnv(t)
	writeEmptyFile(t, filepath.Join(home, ".envokerc"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(home, ".envokerc")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_XDGConfigHomeWhenDotfileMissing(t *testing.T) {
	isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeEmptyFile(t, filepath.Join(xdg, "envoke", "config"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(xdg, "envoke", "config")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_DefaultXDGPathWhenXDGConfigHomeUnset(t *testing.T) {
	home := isolateEnv(t)
	writeEmptyFile(t, filepath.Join(home, ".config", "envoke", "config"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(home, ".config", "envoke", "config")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_DotfileTakesPrecedenceOverXDG(t *testing.T) {
	home := isolateEnv(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeEmptyFile(t, filepath.Join(home, ".envokerc"))
	writeEmptyFile(t, filepath.Join(xdg, "envoke", "config"))

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	want := filepath.Join(home, ".envokerc")
	if !found || path != want {
		t.Errorf("Locate() = (%q, %v), want (%q, true)", path, found, want)
	}
}

func TestLocate_NothingFound(t *testing.T) {
	home := isolateEnv(t)

	path, found, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if found {
		t.Errorf("expected found=false, got path %q", path)
	}
	want := filepath.Join(home, ".envokerc")
	if path != want {
		t.Errorf("path = %q, want default %q", path, want)
	}
}

func writeEmptyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

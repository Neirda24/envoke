package state

import (
	"os"
	"testing"
)

// isolate points the data home at a temp directory and clears the override,
// so a test never reads or writes the developer's real state.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	unsetEnv(t, DisableEnv)
}

func unsetEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
}

func disabledOrFail(t *testing.T) (bool, Source) {
	t.Helper()
	disabled, source, err := Disabled()
	if err != nil {
		t.Fatalf("Disabled: %v", err)
	}
	return disabled, source
}

func TestDisabled_DefaultsToEnabled(t *testing.T) {
	isolate(t)
	if disabled, source := disabledOrFail(t); disabled || source != Default {
		t.Errorf("Disabled() = %v, %v; want false, Default", disabled, source)
	}
}

func TestDisable_PersistsAndEnableClearsIt(t *testing.T) {
	isolate(t)

	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if disabled, source := disabledOrFail(t); !disabled || source != Flag {
		t.Errorf("after Disable: %v, %v; want true, Flag", disabled, source)
	}

	if err := Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if disabled, _ := disabledOrFail(t); disabled {
		t.Error("Enable did not clear the flag")
	}
}

func TestDisable_IsIdempotent(t *testing.T) {
	isolate(t)

	for _, call := range []func() error{Disable, Disable, Enable, Enable} {
		if err := call(); err != nil {
			t.Fatalf("repeated call: %v", err)
		}
	}
	if disabled, _ := disabledOrFail(t); disabled {
		t.Error("two Enables should leave envoke enabled")
	}
}

// TestDisabled_EnvOverridesTheFlag is the reason the override exists: it has
// to work in both directions, or a shell where the persistent switch is set
// could never turn envoke back on for one session.
func TestDisabled_EnvOverridesTheFlag(t *testing.T) {
	isolate(t)
	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	t.Setenv(DisableEnv, "0")
	if disabled, source := disabledOrFail(t); disabled || source != Env {
		t.Errorf("ENVOKE_DISABLE=0 over the flag = %v, %v; want false, Env", disabled, source)
	}
}

func TestDisabled_EnvValues(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"yes", true},
		{"anything", true},
		{"TRUE", true},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
	}

	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			isolate(t)
			t.Setenv(DisableEnv, c.value)
			if disabled, source := disabledOrFail(t); disabled != c.want || source != Env {
				t.Errorf("%s=%q -> %v, %v; want %v, Env", DisableEnv, c.value, disabled, source, c.want)
			}
		})
	}
}

// TestDisabled_EmptyEnvDefersToTheFlag keeps `export ENVOKE_DISABLE=` from
// meaning something different than not exporting it at all -- an empty value
// expresses no opinion, so the persistent switch still decides.
func TestDisabled_EmptyEnvDefersToTheFlag(t *testing.T) {
	isolate(t)
	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	t.Setenv(DisableEnv, "")
	if disabled, source := disabledOrFail(t); !disabled || source != Flag {
		t.Errorf("empty override = %v, %v; want true, Flag", disabled, source)
	}
}

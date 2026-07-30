// Package state holds envoke's on-disk runtime state: where it lives, and
// the one flag stored there that isn't a trust record — whether envoke is
// switched off.
package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DisableEnv is the environment variable that overrides the persistent
// switch for one shell session.
const DisableEnv = "ENVOKE_DISABLE"

// DataHome is $XDG_DATA_HOME, or ~/.local/share when it isn't set — the XDG
// base directory for application state. Trust records and the disable flag
// both live under it, which is why this resolution is here and not in
// internal/trust.
//
// Config, by contrast, follows $XDG_CONFIG_HOME (see config.Locate): what
// envoke records about itself is state, not something a user edits.
func DataHome() (string, error) {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return base, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("state: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}

// Source says what decided the current on/off state, so a command can tell
// the user which of the two switches is the one actually in effect.
type Source int

const (
	// Default: neither switch is set, so envoke is on.
	Default Source = iota
	// Flag: the persistent flag set by Disable.
	Flag
	// Env: $ENVOKE_DISABLE, which wins over the flag.
	Env
)

func (s Source) String() string {
	switch s {
	case Flag:
		return "the persistent switch"
	case Env:
		return "$" + DisableEnv
	default:
		return "the default"
	}
}

// Disabled reports whether envoke should skip running blocks.
//
// $ENVOKE_DISABLE is consulted first and decides on its own, in both
// directions: it is the per-session override, so it has to be able to turn
// envoke back on in a shell where the persistent flag is set, not only off.
// An unset or empty value expresses no opinion and falls through to the
// flag.
//
// Any value that doesn't read as a negation counts as "disabled", so a typo
// switches envoke off rather than quietly leaving it on — the safe direction
// for a tool whose job is executing scripts.
func Disabled() (bool, Source, error) {
	if v, ok := os.LookupEnv(DisableEnv); ok && v != "" {
		switch strings.ToLower(v) {
		case "0", "false", "no", "off":
			return false, Env, nil
		default:
			return true, Env, nil
		}
	}

	path, err := flagPath()
	if err != nil {
		return false, Default, err
	}
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, Flag, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, Default, nil
	default:
		return false, Default, fmt.Errorf("state: %w", err)
	}
}

// Disable sets the persistent switch, for every shell, until Enable. It does
// not touch trust records: turning envoke off is not withdrawing approval,
// and coming back should not mean re-approving every config.
func Disable() error {
	path, err := flagPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	return nil
}

// Enable clears the persistent switch. Enabling an already-enabled envoke is
// a no-op, not an error: the requested end state already holds.
func Enable() error {
	path, err := flagPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("state: %w", err)
	}
	return nil
}

// flagPath is the marker file whose existence means "disabled". Its content
// is never read, so there is nothing to parse and nothing to corrupt.
func flagPath() (string, error) {
	base, err := DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "envoke", "disabled"), nil
}

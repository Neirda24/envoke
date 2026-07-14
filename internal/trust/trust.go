// Package trust implements envoke's config trust store: a config file must
// be explicitly approved with Allow before shell-hook will act on it, and
// any change to the file's content — even whitespace — revokes that trust
// until Allow runs again. This is what CLAUDE.md's trust-before-execution
// principle requires: envoke must never auto-execute a new or modified
// config.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// IsTrusted reports whether configPath's current content matches the hash
// recorded by the most recent Allow call for that path. A missing config
// file or a config that was never allowed both report false, not an error
// — "not trusted" is the normal, expected state for anything that hasn't
// been through Allow yet.
func IsTrusted(configPath string) (bool, error) {
	recorded, err := readRecord(configPath)
	if err != nil {
		return false, err
	}
	if recorded == "" {
		return false, nil
	}

	current, err := contentHash(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("trust: %w", err)
	}

	return recorded == current, nil
}

// Allow records configPath's current content as trusted, superseding any
// previous approval for that path.
func Allow(configPath string) error {
	hash, err := contentHash(configPath)
	if err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}

	recPath, err := recordPath(configPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(recPath), 0o700); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	if err := os.WriteFile(recPath, []byte(hash), 0o600); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	return nil
}

func readRecord(configPath string) (string, error) {
	recPath, err := recordPath(configPath)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(recPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("trust: %w", err)
	}
	return string(b), nil
}

// recordPath maps a config path to its trust record file, named by the hash
// of its own absolute path so distinct config files never collide.
func recordPath(configPath string) (string, error) {
	dir, err := storeDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("trust: %w", err)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(dir, hex.EncodeToString(sum[:])), nil
}

func contentHash(configPath string) (string, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

// storeDir is $XDG_DATA_HOME/envoke/allow, or ~/.local/share/envoke/allow
// if XDG_DATA_HOME isn't set — the XDG Base Directory default for
// application state, matching README's XDG support for the config path.
func storeDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("trust: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "envoke", "allow"), nil
}

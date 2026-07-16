// Package trust implements envoke's config trust store: a config file must
// be explicitly approved with Allow before shell-hook will act on it, and
// any change to the file's content — even whitespace — revokes that trust
// until Allow runs again. This is what CLAUDE.md's trust-before-execution
// principle requires: envoke must never auto-execute a new or modified
// config.
//
// Alongside the trust hash, Allow also persists a copy of the approved
// content itself (see PreviousContent), so a future "diff on allow" feature
// can show what changed between the previously approved config and the one
// being approved now. That copy lives in a new sibling file next to the
// existing hash record (see contentPath) rather than folded into the hash
// record's own format, so upgrading never revokes an existing user's trust:
// a pre-upgrade record is just a hash file with no matching content file,
// and both IsTrusted and PreviousContent treat that as a normal, valid
// state rather than an error.
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
// previous approval for that path. It also persists a copy of that content
// (see PreviousContent) so a later Allow call can show what changed since
// the prior approval.
func Allow(configPath string) error {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	hash := hashContent(content)

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
	if err := os.WriteFile(contentPath(recPath), content, 0o600); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	return nil
}

// PreviousContent returns the content that was approved by the most
// recent Allow call for configPath, and whether a prior approval exists at
// all. A config that was never approved reports ok=false, not an error —
// same "not an error" convention as IsTrusted for the equivalent case. A
// config whose approval predates this feature (a hash record with no
// content file yet, see the package doc comment) also reports ok=false
// rather than erroring, since that's a legitimate pre-upgrade state, not
// corruption.
func PreviousContent(configPath string) (content string, ok bool, err error) {
	recPath, err := recordPath(configPath)
	if err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(contentPath(recPath))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("trust: %w", err)
	}
	return string(b), true, nil
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
// of its own absolute path so distinct config files never collide. This is
// the same path/format Allow has always written the trust hash to — kept
// stable so upgrading to a version of envoke that also writes contentPath
// never revokes an existing user's trust.
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

// contentPath is the sibling file next to a hash record (recPath) that
// holds the approved content itself, for PreviousContent. Deriving it from
// recPath rather than giving it an independent name keeps the two files
// tied together mechanically and makes the "new, additive artifact" nature
// of this file obvious at the call site.
func contentPath(recPath string) string {
	return recPath + ".content"
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func contentHash(configPath string) (string, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	return hashContent(content), nil
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

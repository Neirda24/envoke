// Package trust implements envoke's config trust store: a config file must
// be explicitly approved with Allow before shell-hook will act on it, and
// any change to the file's content — even whitespace — revokes that trust
// until Allow runs again: envoke must never auto-execute a new or modified
// config.
//
// A record is three sibling files under the store directory, all named
// after the hash of the config's absolute path:
//
//	<sha256(abs path)>          the approved content's SHA-256 — the trust token
//	<sha256(abs path)>.content  a copy of the approved content, for diffing
//	<sha256(abs path)>.path     the config's absolute path, for listing
//
// Both siblings are optional on read, so an older record — a bare hash file
// — stays valid and upgrading never revokes anyone's trust. The hash file
// is always written last: a torn write must leave a config untrusted, never
// trusted against content it isn't.
//
// The .path sibling exists because the record name is a one-way hash, so
// nothing else could answer "what have I trusted?" or "which of these
// configs no longer exist?".
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Neirda24/envoke/internal/fsperm"
	"github.com/Neirda24/envoke/internal/state"
)

// IsTrusted reports whether content — the config bytes the caller has already
// read and is about to act on — matches the hash recorded by the most recent
// Allow call for configPath. A config that was never allowed reports false,
// not an error.
//
// Taking the content rather than re-reading configPath is load-bearing: a
// function that reads the file itself can only promise that *some* version
// once matched, leaving the caller free to execute another. Callers get their
// bytes from config.LoadFile.
func IsTrusted(configPath string, content []byte) (bool, error) {
	recorded, err := readRecord(configPath)
	if err != nil {
		return false, err
	}
	if recorded == "" {
		return false, nil
	}
	return recorded == hashContent(content), nil
}

// Allow records content as the trusted content for configPath, superseding
// any previous approval, and persists a copy of it so a later Allow can show
// what changed (see PreviousContent).
//
// Like IsTrusted it takes the bytes the caller reviewed: re-reading the file
// would record a hash for content the user was never shown, so an edit
// landing between the review and the confirmation would be approved
// sight-unseen.
func Allow(configPath string, content []byte) error {
	hash := hashContent(content)

	recPath, err := recordPath(configPath)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(recPath), 0o700); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	// Siblings first, hash record last: a torn write then leaves the previous
	// hash in place and the config reads as untrusted. The other order would
	// leave a record claiming content is trusted while .content described
	// something else.
	if err := writeRecordFile(contentPath(recPath), content); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	if err := writeRecordFile(pathPath(recPath), []byte(abs)); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	if err := writeRecordFile(recPath, []byte(hash)); err != nil {
		return fmt.Errorf("allow %s: %w", configPath, err)
	}
	return nil
}

// writeRecordFile writes a store file atomically: a crash mid-write must
// leave the previous file intact, not a truncated hash record reading as
// "trusted, but not against anything".
func writeRecordFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Record describes one trusted config as the store knows it.
type Record struct {
	// ConfigPath is the absolute path that was approved, or "" for a record
	// written before the store recorded it. Such a record can't be resolved
	// back to a file, which is why List reports it rather than hiding it.
	ConfigPath string
	// Hash is the approved content's SHA-256.
	Hash string
	// StorePath is the hash record's own file, so a caller can report or
	// remove it even when ConfigPath is unknown.
	StorePath string
}

// List returns every trust record in the store, sorted by config path so
// output is stable, with unresolvable (pre-upgrade) records last. An empty
// or absent store is an empty list, not an error.
func List() ([]Record, error) {
	dir, err := storeDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("trust: %w", err)
	}

	var records []Record
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.Contains(name, ".") {
			// Siblings (.content/.path) and any leftover .tmp file from an
			// interrupted write are not records in their own right.
			continue
		}
		recPath := filepath.Join(dir, name)
		hash, err := os.ReadFile(recPath)
		if err != nil {
			return nil, fmt.Errorf("trust: %w", err)
		}
		configPath, err := os.ReadFile(pathPath(recPath))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("trust: %w", err)
		}
		records = append(records, Record{
			ConfigPath: string(configPath),
			Hash:       string(hash),
			StorePath:  recPath,
		})
	}

	sort.Slice(records, func(i, j int) bool {
		if (records[i].ConfigPath == "") != (records[j].ConfigPath == "") {
			return records[j].ConfigPath == ""
		}
		return records[i].ConfigPath < records[j].ConfigPath
	})
	return records, nil
}

// Revoke deletes configPath's trust record and its siblings. found reports
// whether there was anything to revoke; revoking an untrusted config is a
// no-op, not an error — the requested end state already holds.
func Revoke(configPath string) (found bool, err error) {
	recPath, err := recordPath(configPath)
	if err != nil {
		return false, err
	}
	return removeRecord(recPath)
}

// removeRecord deletes a hash record and its siblings, the hash record first:
// a partial removal must leave the config untrusted, not trusted with no
// content copy.
func removeRecord(recPath string) (found bool, err error) {
	for i, p := range []string{recPath, contentPath(recPath), pathPath(recPath)} {
		if err := os.Remove(p); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return found, fmt.Errorf("trust: %w", err)
		}
		if i == 0 {
			found = true
		}
	}
	return found, nil
}

// Prune deletes the records of configs that no longer exist on disk. Records
// with no recorded path are left alone and returned in skipped: without the
// path there is no telling whether the config is gone or the record simply
// predates the store recording it.
func Prune() (removed, skipped []Record, err error) {
	records, err := List()
	if err != nil {
		return nil, nil, err
	}

	for _, r := range records {
		if r.ConfigPath == "" {
			skipped = append(skipped, r)
			continue
		}
		if _, statErr := os.Stat(r.ConfigPath); statErr == nil {
			continue
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("trust: %w", statErr)
		}
		if _, err := removeRecord(r.StorePath); err != nil {
			return nil, nil, err
		}
		removed = append(removed, r)
	}
	return removed, skipped, nil
}

// PreviousContent returns the content approved by the most recent Allow call
// for configPath. ok=false — never an error — covers both a config that was
// never approved and one approved before the store kept content copies.
func PreviousContent(configPath string) (content string, ok bool, err error) {
	recPath, err := recordPath(configPath)
	if err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(contentPath(recPath))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
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
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("trust: %w", err)
	}
	return string(b), nil
}

// recordPath maps a config path to its trust record file, named by the hash
// of its own absolute path so distinct configs never collide. Changing the
// scheme would revoke every existing approval.
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

// contentPath is the sibling holding the approved content, derived from
// recPath so the two stay tied together mechanically.
func contentPath(recPath string) string {
	return recPath + ".content"
}

// pathPath is the sibling holding the approved config's absolute path.
func pathPath(recPath string) string {
	return recPath + ".path"
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// UnsafeStorePermissions reports whether the trust store, or any directory
// between it and the data home, is writable by group or other, and names the
// one that is. Anyone who can write there can drop in a record making any
// config read as trusted, forging an approval that was never given.
//
// Ancestors count because a `0700` store inside a `0777` parent is a `0777`
// store. The walk stops at the data home: above it, a writable directory
// means your whole home is writable, which is not a fact about envoke.
//
// Checked rather than enforced because os.MkdirAll only applies its mode to
// directories it creates, so Allow's 0o700 is not the guarantee it looks
// like. A directory that doesn't exist yet is safe, not an error.
func UnsafeStorePermissions() (unsafe bool, mode os.FileMode, path string, err error) {
	chain, err := storeChain()
	if err != nil {
		return false, 0, "", err
	}

	for _, dir := range chain {
		unsafe, mode, err := fsperm.Unsafe(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return false, 0, dir, fmt.Errorf("trust: %w", err)
		}
		if unsafe {
			return true, mode, dir, nil
		}
	}
	return false, 0, chain[0], nil
}

// storeChain lists the store directory and each ancestor up to and including
// the data home, innermost first. Derived, so moving the store deeper cannot
// leave a level unchecked.
func storeChain() ([]string, error) {
	dir, err := storeDir()
	if err != nil {
		return nil, err
	}
	base, err := state.DataHome()
	if err != nil {
		return nil, err
	}
	// $XDG_DATA_HOME is used as given, so it may carry a trailing separator
	// that would never compare equal to a walked-up path.
	base = filepath.Clean(base)

	var chain []string
	for p := dir; ; p = filepath.Dir(p) {
		chain = append(chain, p)
		if p == base || filepath.Dir(p) == p {
			return chain, nil
		}
	}
}

// storeDir is envoke's data home plus "envoke/allow" — see state.DataHome
// for why trust records are state rather than config.
func storeDir() (string, error) {
	base, err := state.DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "envoke", "allow"), nil
}

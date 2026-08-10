package state

import (
	"fmt"
	"os"
	"testing"
)

func TestHashKeyRecovery(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"reuses a valid key with a matching identifier", testRecoveryReuseValid},
		{"commits the identifier for an orphaned valid key", testRecoveryOrphanedKey},
		{"creates a key when both are absent", testRecoveryCreatesKey},
		{"defers the identifier until the first secret baseline", testRecoveryDefersID},
		{"fails on a stale identifier without a key", testRecoveryStaleIdentifier},
		{"fails on an identifier mismatch", testRecoveryMismatch},
		{"fails on a malformed key file", testRecoveryMalformedKey},
		{"fails safely when secret rows exist and the key is missing", testRecoveryRowsWithoutKey},
		{"reuses the key when secret rows exist and everything matches", testRecoveryRowsMatched},
		{"fails when secret rows exist and the identifier mismatches", testRecoveryRowsMismatch},
		{"accepts a fresh key after explicit reset", testRecoveryAfterReset},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// seedKeyID commits the given identifier directly, as an interrupted recovery
// or a migrated store would have.
func seedKeyID(t *testing.T, store *Store, idHex string) {
	t.Helper()
	execOn(t, store.Database().conn,
		fmt.Sprintf("INSERT INTO metadata (key, value) VALUES ('%s', %s)", hashKeyIDMetadataKey, idHex))
}

// baselineSeed names the pair and target of a directly inserted secret row.
type baselineSeed struct {
	root, home, target string
}

// insertSecretBaseline inserts one secret file row for the registered pair.
func insertSecretBaseline(t *testing.T, store *Store, seed baselineSeed) {
	t.Helper()
	repository, err := store.SetDefaultRepository(seed.root, seed.home)
	if err != nil {
		t.Fatalf("register repository: %v", err)
	}
	execOn(t, store.Database().conn, fmt.Sprintf(
		"INSERT INTO files (repository_id, target_path, group_name, source_path, source_kind, layer, baseline_content_hash, baseline_source_hash, executable_bits, status, applied_at) VALUES (%d, '%s', '', 'secrets/%s', 'secret', 'base', X'0101010101010101010101010101010101010101010101010101010101010101', X'0202020202020202020202020202020202020202020202020202020202020202', 384, 'active', '2026-01-02T03:04:05Z')",
		repository.ID, seed.target, seed.target))
}

func testRecoveryReuseValid(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	writeKeyFile(t, storeDependenciesFor(store), sampleKey(3))
	seedKeyID(t, store, keyIDHex(KeyIDForKey(sampleKey(3))))
	key, err := store.RecoverHashKey()
	if err != nil {
		t.Fatalf("RecoverHashKey: %v", err)
	}
	if key != sampleKey(3) {
		t.Fatal("recovery returned a different key")
	}
}

func testRecoveryOrphanedKey(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	writeKeyFile(t, storeDependenciesFor(store), sampleKey(4))
	key, err := store.RecoverHashKey()
	if err != nil {
		t.Fatalf("RecoverHashKey: %v", err)
	}
	if key != sampleKey(4) {
		t.Fatal("recovery returned a different key")
	}
	stored, err := store.HashKeyID()
	if err != nil {
		t.Fatalf("HashKeyID after recovery: %v", err)
	}
	if stored != KeyIDForKey(sampleKey(4)) {
		t.Fatal("recovery committed the wrong identifier")
	}
}

func testRecoveryCreatesKey(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	key, err := store.RecoverHashKey()
	if err != nil {
		t.Fatalf("RecoverHashKey: %v", err)
	}
	if key == [32]byte{} {
		t.Fatal("recovery returned an empty key")
	}
	info, err := os.Lstat(keyPathFor(t, storeDependenciesFor(store)))
	if err != nil {
		t.Fatalf("created key file: %v", err)
	}
	if info.Mode().Perm() != stateFileMode {
		t.Fatalf("key file mode = %v, want %v", info.Mode().Perm(), stateFileMode)
	}
}

func testRecoveryDefersID(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	if _, err := store.RecoverHashKey(); err != nil {
		t.Fatalf("RecoverHashKey: %v", err)
	}
	if _, err := store.HashKeyID(); err == nil {
		t.Fatal("recovery committed the identifier before any secret baseline")
	}
}

func testRecoveryStaleIdentifier(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	deps := storeDependenciesFor(store)
	seedKeyID(t, store, keyIDHex(KeyIDForKey(sampleKey(5))))
	if _, err := store.RecoverHashKey(); err == nil {
		t.Fatal("RecoverHashKey succeeded with a stale identifier and no key")
	}
	if _, err := os.Lstat(keyPathFor(t, deps)); !os.IsNotExist(err) {
		t.Fatal("recovery created a key for a stale identifier")
	}
}

func testRecoveryMismatch(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	writeKeyFile(t, storeDependenciesFor(store), sampleKey(1))
	seedKeyID(t, store, keyIDHex(KeyIDForKey(sampleKey(2))))
	if _, err := store.RecoverHashKey(); err == nil {
		t.Fatal("RecoverHashKey accepted a mismatched key")
	}
}

func testRecoveryMalformedKey(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	writeRawKeyFile(t, storeDependenciesFor(store), []byte("torn"))
	if _, err := store.RecoverHashKey(); err == nil {
		t.Fatal("RecoverHashKey accepted a malformed key")
	}
}

func testRecoveryRowsWithoutKey(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	insertSecretBaseline(t, store, baselineSeed{root: root, home: home, target: ".secret"})
	if _, err := store.RecoverHashKey(); err == nil {
		t.Fatal("RecoverHashKey succeeded with secret rows and no key")
	}
	if _, err := os.Lstat(keyPathFor(t, storeDependenciesFor(store))); !os.IsNotExist(err) {
		t.Fatal("recovery created a key while secret rows existed")
	}
}

func testRecoveryRowsMatched(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	insertSecretBaseline(t, store, baselineSeed{root: root, home: home, target: ".secret"})
	writeKeyFile(t, storeDependenciesFor(store), sampleKey(6))
	seedKeyID(t, store, keyIDHex(KeyIDForKey(sampleKey(6))))
	key, err := store.RecoverHashKey()
	if err != nil {
		t.Fatalf("RecoverHashKey with matched rows: %v", err)
	}
	if key != sampleKey(6) {
		t.Fatal("recovery returned a different key")
	}
}

func testRecoveryRowsMismatch(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	insertSecretBaseline(t, store, baselineSeed{root: root, home: home, target: ".secret"})
	writeKeyFile(t, storeDependenciesFor(store), sampleKey(1))
	seedKeyID(t, store, keyIDHex(KeyIDForKey(sampleKey(2))))
	if _, err := store.RecoverHashKey(); err == nil {
		t.Fatal("RecoverHashKey accepted a mismatched key with secret rows")
	}
}

func testRecoveryAfterReset(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	deps := storeDependenciesFor(store)
	writeKeyFile(t, deps, sampleKey(1))
	seedKeyID(t, store, keyIDHex(KeyIDForKey(sampleKey(1))))
	if _, err := store.RecoverHashKey(); err != nil {
		t.Fatalf("initial recovery: %v", err)
	}
	if err := os.Remove(keyPathFor(t, deps)); err != nil {
		t.Fatalf("remove key: %v", err)
	}
	execOn(t, store.Database().conn, "DELETE FROM metadata WHERE key = '"+hashKeyIDMetadataKey+"'")
	if _, err := store.RecoverHashKey(); err != nil {
		t.Fatalf("recovery after reset: %v", err)
	}
}

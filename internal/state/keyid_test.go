package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestHashKeyIdentity(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"identifier is deterministic per key", testKeyIDDeterministic},
		{"metadata read reports missing before any commit", testKeyIDMissingMetadata},
		{"commit persists the identifier", testKeyIDCommitted},
		{"replacement key produces a different identifier", testKeyIDReplacement},
		{"stored identifier length is validated", testKeyIDInvalidLength},
		{"errors never expose key material", testKeyIDNoKeyMaterial},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// sampleKey returns a deterministic 32-byte key filled with value.
func sampleKey(value byte) [32]byte {
	var key [32]byte
	for index := range key {
		key[index] = value
	}
	return key
}

// keyPathFor resolves the hash.key path beneath the store's cattery directory.
func keyPathFor(t *testing.T, deps Dependencies) string {
	t.Helper()
	return filepath.Join(catteryDirFor(t, deps), stateKeyFileName)
}

// writeKeyFile seeds the hash key file with the given key.
func writeKeyFile(t *testing.T, deps Dependencies, key [32]byte) {
	t.Helper()
	if err := os.WriteFile(keyPathFor(t, deps), key[:], stateFileMode); err != nil {
		t.Fatalf("seed hash key: %v", err)
	}
}

// writeRawKeyFile seeds the hash key file with raw bytes, including torn or
// malformed content.
func writeRawKeyFile(t *testing.T, deps Dependencies, contents []byte) {
	t.Helper()
	if err := os.WriteFile(keyPathFor(t, deps), contents, stateFileMode); err != nil {
		t.Fatalf("seed hash key: %v", err)
	}
}

// keyIDHex renders the stored identifier for SQL injection as a BLOB literal.
func keyIDHex(id deployment.Digest) string {
	return fmt.Sprintf("X'%x'", id[:])
}

func testKeyIDDeterministic(t *testing.T) {
	first := KeyIDForKey(sampleKey(7))
	second := KeyIDForKey(sampleKey(7))
	if first != second {
		t.Fatal("identifier differs across calls for the same key")
	}
	if KeyIDForKey(sampleKey(8)) == first {
		t.Fatal("identifier collides across different keys")
	}
}

func testKeyIDMissingMetadata(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	if _, err := store.HashKeyID(); err == nil {
		t.Fatal("HashKeyID succeeded with no metadata row")
	}
}

func testKeyIDCommitted(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	writeKeyFile(t, storeDependenciesFor(store), sampleKey(1))
	if _, err := store.RecoverHashKey(); err != nil {
		t.Fatalf("RecoverHashKey: %v", err)
	}
	stored, err := store.HashKeyID()
	if err != nil {
		t.Fatalf("HashKeyID after recovery: %v", err)
	}
	if stored != KeyIDForKey(sampleKey(1)) {
		t.Fatal("stored identifier differs from the derived identifier")
	}
}

func testKeyIDReplacement(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	deps := storeDependenciesFor(store)
	writeKeyFile(t, deps, sampleKey(1))
	if _, err := store.RecoverHashKey(); err != nil {
		t.Fatalf("initial recovery: %v", err)
	}
	original, err := store.HashKeyID()
	if err != nil {
		t.Fatalf("HashKeyID: %v", err)
	}
	writeKeyFile(t, deps, sampleKey(2))
	if _, err := store.RecoverHashKey(); err == nil {
		t.Fatal("RecoverHashKey accepted a replaced key")
	}
	kept, err := store.HashKeyID()
	if err != nil {
		t.Fatalf("HashKeyID after replacement: %v", err)
	}
	if kept != original {
		t.Fatal("replacement changed the stored identifier")
	}
}

func testKeyIDInvalidLength(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	execOn(t, store.Database().conn,
		fmt.Sprintf("INSERT INTO metadata (key, value) VALUES ('%s', X'010203')", hashKeyIDMetadataKey))
	if _, err := store.HashKeyID(); err == nil {
		t.Fatal("HashKeyID accepted a 3-byte stored identifier")
	}
}

func testKeyIDNoKeyMaterial(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	deps := storeDependenciesFor(store)
	writeKeyFile(t, deps, sampleKey(9))
	if _, err := store.RecoverHashKey(); err != nil {
		t.Fatalf("RecoverHashKey: %v", err)
	}
	writeKeyFile(t, deps, sampleKey(10))
	_, mismatchErr := store.RecoverHashKey()
	if mismatchErr == nil {
		t.Fatal("RecoverHashKey accepted a replaced key")
	}
	if containsAnyKey(mismatchErr.Error(), sampleKey(9), sampleKey(10)) {
		t.Fatalf("diagnostic %q exposed key material", mismatchErr.Error())
	}
}

// containsAnyKey reports whether the diagnostic contains any of the keys.
func containsAnyKey(diagnostic string, keys ...[32]byte) bool {
	for _, key := range keys {
		if containsKey(diagnostic, key) {
			return true
		}
	}
	return false
}

func containsKey(diagnostic string, key [32]byte) bool {
	for offset := 0; offset <= len(diagnostic)-len(key); offset++ {
		if diagnostic[offset:offset+len(key)] == string(key[:]) {
			return true
		}
	}
	return false
}

// storeDependenciesFor recovers the dependencies of an opened store so tests
// can seed paths beneath its state home.
func storeDependenciesFor(store *Store) Dependencies {
	return Dependencies{StateHome: store.stateHome}
}

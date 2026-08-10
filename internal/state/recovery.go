package state

import (
	"errors"
	"fmt"
	"path/filepath"
)

// secretBaselinesSQL counts every stored secret file row regardless of status,
// because retired secrets still need the key for diagnostics.
const secretBaselinesSQL = "SELECT COUNT(*) FROM files WHERE source_kind = 'secret'"

// RecoverHashKey guarantees a usable 32-byte hash key exists and matches its
// committed identifier (PLAN.md Section 8.1), returning the key. It fails
// safely whenever the stored key cannot be proven correct: it never guesses,
// and it never silently replaces a key that old baselines depend on.
func (store *Store) RecoverHashKey() ([32]byte, error) {
	key, readErr := store.keyFile().Read()
	identifier, idErr := store.HashKeyID()
	if readErr == nil && idErr == nil {
		if KeyIDForKey(key) != identifier {
			return [32]byte{}, errKeyMismatch(store.keyPath())
		}
		return key, nil
	}
	hasSecrets, err := store.secretBaselinesExist()
	if err != nil {
		return [32]byte{}, err
	}
	if hasSecrets {
		return [32]byte{}, errKeyRecoveryBlocked(store.keyPath(), readErr, idErr)
	}
	return store.recoverWithoutBaselines(key, readErr, idErr)
}

// recoverWithoutBaselines applies the no-secret-row rules: commit the derived
// identifier of an orphaned valid key, create a key only when both are absent,
// and fail on any other mismatch that requires explicit cleanup.
func (store *Store) recoverWithoutBaselines(key [32]byte, readErr, idErr error) ([32]byte, error) {
	switch {
	case readErr == nil && errors.Is(idErr, hashKeyIDMissingError{}):
		if err := store.commitHashKeyID(KeyIDForKey(key)); err != nil {
			return [32]byte{}, err
		}
		return key, nil
	case errors.Is(readErr, keyMissingError{}) && errors.Is(idErr, hashKeyIDMissingError{}):
		key, err := store.keyFile().Create()
		if err != nil {
			return [32]byte{}, err
		}
		return key, nil
	default:
		return [32]byte{}, errKeyRecoveryBlocked(store.keyPath(), readErr, idErr)
	}
}

// secretBaselinesExist reports whether any secret file row is stored.
func (store *Store) secretBaselinesExist() (bool, error) {
	var count int64
	if err := store.database.conn.QueryRow(secretBaselinesSQL).Scan(&count); err != nil {
		return false, fmt.Errorf("state: count secret baselines: %w", err)
	}
	return count > 0, nil
}

// keyPath returns the hash.key path beside the open database.
func (store *Store) keyPath() string {
	return filepath.Join(filepath.Dir(store.database.path), stateKeyFileName)
}

// keyFile returns a KeyFile bound to the store's hash.key path.
func (store *Store) keyFile() *KeyFile {
	return NewKeyFile(store.keyPath())
}

// errKeyMismatch reports a key file whose identifier contradicts the
// committed one, without exposing either.
func errKeyMismatch(path string) error {
	return fmt.Errorf(
		"state: hash key %q does not match its stored identifier; restore the matching hash.key or reset state",
		path)
}

// errKeyRecoveryBlocked reports a key state that cannot be repaired
// automatically. It names paths and statuses only, never key material.
func errKeyRecoveryBlocked(path string, keyErr, idErr error) error {
	return fmt.Errorf(
		"state: hash key %q cannot be used; restore the matching hash.key or reset state (key: %v, identifier: %v)",
		path, keyErr, idErr)
}

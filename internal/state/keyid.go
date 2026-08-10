package state

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// hashKeyIDMetadataKey names the metadata row holding the derived identifier
// of hash.key, so replacement can be detected without
// storing the key itself.
const hashKeyIDMetadataKey = "hash_key_id"

// metadataValueSQL reads one metadata value by key.
const metadataValueSQL = "SELECT value FROM metadata WHERE key = ?"

// metadataUpsertSQL writes one metadata value, replacing a prior value.
const metadataUpsertSQL = `
INSERT INTO metadata (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`

// KeyIDForKey derives the domain-separated identifier that names a 32-byte
// hash key without exposing it.
func KeyIDForKey(key [32]byte) deployment.Digest {
	return deployment.HashKeyIdentifier(key)
}

// HashKeyID reads the committed hash-key identifier, or returns a missing
// error when no identifier has been committed yet.
func (store *Store) HashKeyID() (deployment.Digest, error) {
	var raw []byte
	err := store.database.conn.QueryRow(metadataValueSQL, hashKeyIDMetadataKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return deployment.Digest{}, hashKeyIDMissingError{}
	}
	if err != nil {
		return deployment.Digest{}, fmt.Errorf("state: read hash key identifier: %w", err)
	}
	return decodeDigest(raw)
}

// commitHashKeyID writes the identifier in its own short transaction. Callers
// that must commit the identifier with a baseline row use metadataUpsertSQL
// inside that transaction instead.
func (store *Store) commitHashKeyID(id deployment.Digest) error {
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return err
	}
	if err := execIn(transaction, metadataUpsertSQL, hashKeyIDMetadataKey, id[:]); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	return nil
}

// decodeDigest validates and copies a stored 32-byte BLOB into a Digest.
func decodeDigest(raw []byte) (deployment.Digest, error) {
	var digest deployment.Digest
	if len(raw) != len(digest) {
		return deployment.Digest{}, fmt.Errorf("state: stored digest has length %d, want %d", len(raw), len(digest))
	}
	copy(digest[:], raw)
	return digest, nil
}

// hashKeyIDMissingError signals an absent hash_key_id metadata row. Its Is
// method lets recovery detect the missing case regardless of the path.
type hashKeyIDMissingError struct{}

func (hashKeyIDMissingError) Error() string {
	return "state: hash key identifier is missing"
}

func (hashKeyIDMissingError) Is(target error) bool {
	_, matched := target.(hashKeyIDMissingError)
	return matched
}

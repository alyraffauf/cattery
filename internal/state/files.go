package state

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/alyraffauf/cattery/internal/deployment"
)

const fileColumns = "repository_id, target_path, group_name, source_path, source_kind, layer, baseline_content_hash, baseline_source_hash, executable_bits, status, applied_at, retired_at"

// fileUpsertSQL registers one active file row, reactivating any retired row at
// the same target and refreshing its baseline.
const fileUpsertSQL = `
INSERT INTO files (repository_id, target_path, group_name, source_path, source_kind, layer,
                   baseline_content_hash, baseline_source_hash, executable_bits, status, applied_at)
VALUES ((SELECT id FROM repositories WHERE root_path = ? AND home_path = ?),
        ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)
ON CONFLICT(repository_id, target_path) DO UPDATE SET
    group_name = excluded.group_name, source_path = excluded.source_path,
    source_kind = excluded.source_kind, layer = excluded.layer,
    baseline_content_hash = excluded.baseline_content_hash,
    baseline_source_hash = excluded.baseline_source_hash,
    executable_bits = excluded.executable_bits, status = 'active',
    applied_at = excluded.applied_at, retired_at = NULL`

const fileRetireSQL = "UPDATE files SET status = 'retired', retired_at = ? WHERE repository_id = ? AND target_path = ?"

const fileByPairTargetSQL = `
SELECT ` + fileColumns + `
FROM files f JOIN repositories r ON r.id = f.repository_id
WHERE r.root_path = ? AND r.home_path = ? AND f.target_path = ?`

const allFileBaselinesSQL = `
SELECT ` + fileColumns + `
FROM files f JOIN repositories r ON r.id = f.repository_id
WHERE r.root_path = ? AND r.home_path = ? ORDER BY f.target_path`

// dualActiveByPairSQL lists paths active in both representations of a pair,
// which the schema cannot express as a constraint.
const dualActiveByPairSQL = `
SELECT f.target_path FROM files f
JOIN repositories r ON r.id = f.repository_id
JOIN aliases a ON a.repository_id = f.repository_id AND a.alias_path = f.target_path
WHERE r.root_path = ? AND r.home_path = ? AND f.status = 'active' AND a.status = 'active'
ORDER BY f.target_path`

const allFileGroupsSQL = `
SELECT DISTINCT group_name FROM files f JOIN repositories r ON r.id = f.repository_id
WHERE r.root_path = ? AND r.home_path = ? ORDER BY group_name`

// fileBaselineKey identifies one row by canonical pair and target.
type fileBaselineKey struct {
	root, home, target string
}

// fileBatch carries the arguments of one file-baseline transaction.
type fileBatch struct {
	root, home, now string
	baseline        FileBaseline
	keyID           *deployment.Digest
}

// UpsertFileBaseline registers the canonical pair if needed and upserts one
// active file row in a short transaction, stamping applied_at from the clock.
// For a secret row it also commits the hash-key identifier in the same
// transaction.
func (store *Store) UpsertFileBaseline(root, home string, baseline FileBaseline) (FileBaseline, error) {
	root, home, err := prepareFileBaseline(root, home, baseline)
	if err != nil {
		return FileBaseline{}, err
	}
	keyID, err := store.ensureKeyID(baseline.SourceKind)
	if err != nil {
		return FileBaseline{}, err
	}
	now := formatTimestamp(store.now())
	batch := fileBatch{root: root, home: home, now: now, baseline: baseline, keyID: keyID}
	key := fileBaselineKey{root: root, home: home, target: baseline.TargetPath}
	return runStateTransaction(store.database.conn,
		func(transaction *sql.Tx) error { return applyFileBatch(transaction, batch) },
		func(transaction *sql.Tx) (FileBaseline, error) {
			return scanFileBaseline(transaction.QueryRow(fileByPairTargetSQL, key.root, key.home, key.target))
		})
}

// RetireFileBaseline marks one active row retired in its own transaction,
// retaining the baseline for diagnostics and reactivation.
func (store *Store) RetireFileBaseline(root, home, target string) (FileBaseline, error) {
	return store.setFileStatus(fileBaselineKey{root: root, home: home, target: target}, fileRetireSQL)
}

func (store *Store) setFileStatus(key fileBaselineKey, statement string) (FileBaseline, error) {
	repository, err := store.requireRepository(key.root, key.home)
	if err != nil {
		return FileBaseline{}, err
	}
	if !IsSlashRelative(key.target) {
		return FileBaseline{}, fmt.Errorf("state: file target %q is not a slash-relative path", key.target)
	}
	now := formatTimestamp(store.now())
	return runStateTransaction(store.database.conn,
		func(transaction *sql.Tx) error { return execIn(transaction, statement, now, repository.ID, key.target) },
		func(transaction *sql.Tx) (FileBaseline, error) {
			return scanFileBaseline(transaction.QueryRow(fileByPairTargetSQL, key.root, key.home, key.target))
		})
}

// FileBaseline reads one file row of the canonical pair.
func (store *Store) FileBaseline(root, home, target string) (FileBaseline, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return FileBaseline{}, err
	}
	if !IsSlashRelative(target) {
		return FileBaseline{}, fmt.Errorf("state: file target %q is not a slash-relative path", target)
	}
	baseline, err := scanFileBaseline(store.database.conn.QueryRow(fileByPairTargetSQL, root, home, target))
	if errors.Is(err, sql.ErrNoRows) {
		return FileBaseline{}, errMissingFileBaseline(target)
	}
	if err != nil {
		return FileBaseline{}, err
	}
	return baseline, nil
}

// FileBaselines reads every file row of the canonical pair in target-path
// order, after rejecting cross-representation corruption.
func (store *Store) FileBaselines(root, home string) ([]FileBaseline, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readFileBaselines(allFileBaselinesSQL, root, home)
}

// FileGroups lists the distinct group names of any file row, so an explicitly
// selected retired-only group remains valid.
func (store *Store) FileGroups(root, home string) ([]string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readFileGroups(allFileGroupsSQL, root, home)
}

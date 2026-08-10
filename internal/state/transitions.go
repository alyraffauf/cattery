package state

import (
	"database/sql"
	"fmt"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// dualStatusSQL reads the current status of the same path in both tables,
// returning NULL for absent rows.
const dualStatusSQL = `
SELECT (SELECT status FROM files WHERE repository_id = ? AND target_path = ?),
       (SELECT status FROM aliases WHERE repository_id = ? AND alias_path = ?)`

// fileUpsertByRepositorySQL activates one file row keyed by repository id,
// reactivating any retired row at the same target.
const fileUpsertByRepositorySQL = `
INSERT INTO files (repository_id, target_path, group_name, source_path, source_kind, layer,
                   baseline_content_hash, baseline_source_hash, executable_bits, status, applied_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)
ON CONFLICT(repository_id, target_path) DO UPDATE SET
    group_name = excluded.group_name, source_path = excluded.source_path,
    source_kind = excluded.source_kind, layer = excluded.layer,
    baseline_content_hash = excluded.baseline_content_hash,
    baseline_source_hash = excluded.baseline_source_hash,
    executable_bits = excluded.executable_bits, status = 'active',
    applied_at = excluded.applied_at, retired_at = NULL`

// aliasUpsertByRepositorySQL realizes one alias row keyed by repository id,
// reactivating any retired row at the same path.
const aliasUpsertByRepositorySQL = `
INSERT INTO aliases (repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at)
VALUES (?, ?, ?, ?, ?, 'active', ?)
ON CONFLICT(repository_id, alias_path) DO UPDATE SET
    canonical_target_path = excluded.canonical_target_path, group_name = excluded.group_name,
    layer = excluded.layer, status = 'active', applied_at = excluded.applied_at, retired_at = NULL`

// aliasTransition carries the repository scope of a file-to-alias transition.
type aliasTransition struct {
	repositoryID int64
	baseline     AliasBaseline
}

// fileTransition carries the repository scope of an alias-to-file transition.
type fileTransition struct {
	repositoryID int64
	baseline     FileBaseline
	keyID        *deployment.Digest
}

// TransitionToAlias replaces one active file row with an active alias row at
// the same target path in a single transaction.
func (store *Store) TransitionToAlias(root, home string, baseline AliasBaseline) (AliasBaseline, error) {
	root, home, err := prepareAliasBaseline(root, home, baseline)
	if err != nil {
		return AliasBaseline{}, err
	}
	repository, err := store.requireRepository(root, home)
	if err != nil {
		return AliasBaseline{}, err
	}
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return AliasBaseline{}, err
	}
	if err := store.transitionToAlias(transaction, aliasTransition{repositoryID: repository.ID, baseline: baseline}); err != nil {
		_ = transaction.Rollback()
		return AliasBaseline{}, err
	}
	return scanAndCommitAlias(transaction, aliasBaselineKey{root: root, home: home, alias: baseline.AliasPath})
}

func (store *Store) transitionToAlias(transaction *sql.Tx, transition aliasTransition) error {
	if err := requireActiveFileRepresentation(transaction, transition.repositoryID, transition.baseline.AliasPath); err != nil {
		return err
	}
	return store.applyAliasTransition(transaction, transition)
}

func (store *Store) applyAliasTransition(transaction *sql.Tx, transition aliasTransition) error {
	now := formatTimestamp(store.now())
	if err := execIn(transaction, aliasUpsertByRepositorySQL,
		transition.repositoryID, transition.baseline.AliasPath, transition.baseline.CanonicalTargetPath,
		transition.baseline.GroupName, string(transition.baseline.Layer), now); err != nil {
		return err
	}
	if err := execIn(transaction, fileRetireSQL, now, transition.repositoryID, transition.baseline.AliasPath); err != nil {
		return err
	}
	return nil
}

// TransitionToFile replaces one active alias row with an active file row at
// the same target path in a single transaction.
func (store *Store) TransitionToFile(root, home string, baseline FileBaseline) (FileBaseline, error) {
	root, home, err := prepareFileBaseline(root, home, baseline)
	if err != nil {
		return FileBaseline{}, err
	}
	keyID, err := store.ensureKeyID(baseline.SourceKind)
	if err != nil {
		return FileBaseline{}, err
	}
	repository, err := store.requireRepository(root, home)
	if err != nil {
		return FileBaseline{}, err
	}
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return FileBaseline{}, err
	}
	if err := store.transitionToFile(transaction, fileTransition{repositoryID: repository.ID, baseline: baseline, keyID: keyID}); err != nil {
		_ = transaction.Rollback()
		return FileBaseline{}, err
	}
	return scanAndCommitFile(transaction, fileBaselineKey{root: root, home: home, target: baseline.TargetPath})
}

func (store *Store) transitionToFile(transaction *sql.Tx, transition fileTransition) error {
	if err := requireActiveAliasRepresentation(transaction, transition.repositoryID, transition.baseline.TargetPath); err != nil {
		return err
	}
	return store.applyFileTransition(transaction, transition)
}

func (store *Store) applyFileTransition(transaction *sql.Tx, transition fileTransition) error {
	now := formatTimestamp(store.now())
	if err := execIn(transaction, fileUpsertByRepositorySQL,
		transition.repositoryID, transition.baseline.TargetPath, transition.baseline.GroupName, transition.baseline.SourcePath,
		string(transition.baseline.SourceKind), string(transition.baseline.Layer),
		transition.baseline.BaselineContentHash[:], transition.baseline.BaselineSourceHash[:],
		transition.baseline.ExecutableBits, now); err != nil {
		return err
	}
	if transition.keyID != nil {
		if err := commitKeyIdentifier(transaction, transition.keyID); err != nil {
			return err
		}
	}
	if err := execIn(transaction, aliasRetireSQL, now, transition.repositoryID, transition.baseline.TargetPath); err != nil {
		return err
	}
	return nil
}

// requireActiveFileRepresentation fails unless the path has an active file row
// and no active alias row, rejecting pre-existing dual-active corruption.
func requireActiveFileRepresentation(transaction *sql.Tx, repositoryID int64, path string) error {
	fileStatus, aliasStatus, err := representationStatuses(transaction, repositoryID, path)
	if err != nil {
		return err
	}
	if fileStatus == string(StatusActive) && aliasStatus == string(StatusActive) {
		return fmt.Errorf("state: representation corruption at %q: active in both tables", path)
	}
	if fileStatus != string(StatusActive) {
		return fmt.Errorf("state: no active file representation at %q to transition", path)
	}
	return nil
}

// requireActiveAliasRepresentation fails unless the path has an active alias
// row and no active file row, rejecting pre-existing dual-active corruption.
func requireActiveAliasRepresentation(transaction *sql.Tx, repositoryID int64, path string) error {
	fileStatus, aliasStatus, err := representationStatuses(transaction, repositoryID, path)
	if err != nil {
		return err
	}
	if fileStatus == string(StatusActive) && aliasStatus == string(StatusActive) {
		return fmt.Errorf("state: representation corruption at %q: active in both tables", path)
	}
	if aliasStatus != string(StatusActive) {
		return fmt.Errorf("state: no active alias representation at %q to transition", path)
	}
	return nil
}

func representationStatuses(transaction *sql.Tx, repositoryID int64, path string) (string, string, error) {
	var fileStatus, aliasStatus sql.NullString
	if err := transaction.QueryRow(dualStatusSQL, repositoryID, path, repositoryID, path).Scan(&fileStatus, &aliasStatus); err != nil {
		return "", "", fmt.Errorf("state: read representation statuses: %w", err)
	}
	return fileStatus.String, aliasStatus.String, nil
}

func commitKeyIdentifier(transaction *sql.Tx, keyID *deployment.Digest) error {
	if keyID == nil {
		return nil
	}
	if err := execIn(transaction, metadataUpsertSQL, hashKeyIDMetadataKey, keyID[:]); err != nil {
		return err
	}
	return nil
}

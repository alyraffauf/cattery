package state

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func prepareFileBaseline(root, home string, baseline FileBaseline) (string, string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return "", "", err
	}
	if err := validateFileBaseline(baseline); err != nil {
		return "", "", err
	}
	return root, home, nil
}

func validateFileBaseline(baseline FileBaseline) error {
	if err := validateFilePaths(baseline); err != nil {
		return err
	}
	return validateFileMetadata(baseline)
}

func validateFilePaths(baseline FileBaseline) error {
	if !IsSlashRelative(baseline.TargetPath) {
		return fmt.Errorf("state: file baseline target %q is not a slash-relative path", baseline.TargetPath)
	}
	if !IsSlashRelative(baseline.SourcePath) {
		return fmt.Errorf("state: file baseline source %q is not a slash-relative path", baseline.SourcePath)
	}
	if baseline.GroupName != "" {
		if err := pathsafe.GroupName(baseline.GroupName); err != nil {
			return fmt.Errorf("state: file baseline group: %w", err)
		}
	}
	return nil
}

func validateFileMetadata(baseline FileBaseline) error {
	if !baseline.SourceKind.Valid() {
		return fmt.Errorf("state: file baseline has invalid source kind %q", baseline.SourceKind)
	}
	if !baseline.Layer.Valid() {
		return fmt.Errorf("state: file baseline has invalid layer %q", baseline.Layer)
	}
	if baseline.BaselineContentHash == (deployment.Digest{}) || baseline.BaselineSourceHash == (deployment.Digest{}) {
		return fmt.Errorf("state: file baseline %q has an unset digest", baseline.TargetPath)
	}
	if baseline.ExecutableBits > 0o777 {
		return fmt.Errorf("state: file baseline %q has invalid executable bits %o", baseline.TargetPath, baseline.ExecutableBits)
	}
	return nil
}

func (store *Store) ensureKeyID(kind deployment.FileKind) (*deployment.Digest, error) {
	if kind != deployment.FileSecret {
		return nil, nil
	}
	key, err := store.RecoverHashKey()
	if err != nil {
		return nil, err
	}
	identifier := KeyIDForKey(key)
	return &identifier, nil
}

func applyFileBatch(transaction *sql.Tx, batch fileBatch) error {
	if err := execIn(transaction, repositoryUpsertSQL, batch.root, batch.home, batch.now, batch.now); err != nil {
		return err
	}
	if err := execIn(transaction, fileUpsertSQL,
		batch.root, batch.home, batch.baseline.TargetPath, batch.baseline.GroupName, batch.baseline.SourcePath,
		string(batch.baseline.SourceKind), string(batch.baseline.Layer),
		batch.baseline.BaselineContentHash[:], batch.baseline.BaselineSourceHash[:],
		batch.baseline.ExecutableBits, batch.now); err != nil {
		return err
	}
	if batch.keyID != nil {
		if err := commitKeyIdentifier(transaction, batch.keyID); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) requireRepository(root, home string) (Repository, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return Repository{}, err
	}
	return store.LookupRepository(root, home)
}

func scanAndCommitFile(transaction *sql.Tx, key fileBaselineKey) (FileBaseline, error) {
	return commitStateRead(transaction, func(transaction *sql.Tx) (FileBaseline, error) {
		baseline, err := scanFileBaseline(transaction.QueryRow(fileByPairTargetSQL, key.root, key.home, key.target))
		if errors.Is(err, sql.ErrNoRows) {
			return FileBaseline{}, errMissingFileBaseline(key.target)
		}
		return baseline, err
	})
}

func (store *Store) readFileBaselines(statement, root, home string) ([]FileBaseline, error) {
	if err := store.checkRepresentationCorruption(root, home); err != nil {
		return nil, err
	}
	rows, err := store.database.conn.Query(statement, root, home)
	if err != nil {
		return nil, fmt.Errorf("state: list file baselines: %w", err)
	}
	defer rows.Close()
	var baselines []FileBaseline
	for rows.Next() {
		baseline, err := scanFileBaseline(rows)
		if err != nil {
			return nil, err
		}
		baselines = append(baselines, baseline)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list file baselines: %w", err)
	}
	return CopyFileBaselines(baselines), nil
}

func (store *Store) checkRepresentationCorruption(root, home string) error {
	rows, err := store.database.conn.Query(dualActiveByPairSQL, root, home)
	if err != nil {
		return fmt.Errorf("state: check representation corruption: %w", err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("state: check representation corruption: %w", err)
	}
	if len(paths) > 0 {
		return errDualActiveRepresentation(len(paths))
	}
	return nil
}

func (store *Store) readFileGroups(statement, root, home string) ([]string, error) {
	rows, err := store.database.conn.Query(statement, root, home)
	if err != nil {
		return nil, fmt.Errorf("state: list file groups: %w", err)
	}
	defer rows.Close()
	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list file groups: %w", err)
	}
	return groups, nil
}

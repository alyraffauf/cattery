package state

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func prepareAliasBaseline(root, home string, baseline AliasBaseline) (string, string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return "", "", err
	}
	if err := validateAliasBaseline(baseline); err != nil {
		return "", "", err
	}
	return root, home, nil
}

// validateAliasBaseline rejects rows that cannot be stored faithfully.
func validateAliasBaseline(baseline AliasBaseline) error {
	if !IsSlashRelative(baseline.AliasPath) {
		return fmt.Errorf("state: alias baseline path %q is not a slash-relative path", baseline.AliasPath)
	}
	if !IsSlashRelative(baseline.CanonicalTargetPath) {
		return fmt.Errorf("state: alias baseline target %q is not a slash-relative path", baseline.CanonicalTargetPath)
	}
	if baseline.GroupName != "" {
		if err := pathsafe.GroupName(baseline.GroupName); err != nil {
			return fmt.Errorf("state: alias baseline group: %w", err)
		}
	}
	if !baseline.Layer.Valid() {
		return fmt.Errorf("state: alias baseline has invalid layer %q", baseline.Layer)
	}
	return nil
}

func applyAliasBatch(transaction *sql.Tx, batch aliasBatch) error {
	if err := execIn(transaction, repositoryUpsertSQL, batch.root, batch.home, batch.now, batch.now); err != nil {
		return err
	}
	if err := execIn(transaction, aliasUpsertSQL,
		batch.root, batch.home, batch.baseline.AliasPath, batch.baseline.CanonicalTargetPath,
		batch.baseline.GroupName, string(batch.baseline.Layer), batch.now); err != nil {
		return err
	}
	return nil
}

func (store *Store) setAliasStatus(key aliasBaselineKey, statement string) (AliasBaseline, error) {
	repository, err := store.requireRepository(key.root, key.home)
	if err != nil {
		return AliasBaseline{}, err
	}
	if !IsSlashRelative(key.alias) {
		return AliasBaseline{}, fmt.Errorf("state: alias path %q is not a slash-relative path", key.alias)
	}
	now := formatTimestamp(store.now())
	return runStateTransaction(store.database.conn,
		func(transaction *sql.Tx) error { return execIn(transaction, statement, now, repository.ID, key.alias) },
		func(transaction *sql.Tx) (AliasBaseline, error) { return readAliasBaselineAt(transaction, key) })
}

// readAliasBaselineAt scans one alias row of the pair, translating a missing
// row into errMissingAliasBaseline so callers get the path-specific error.
func readAliasBaselineAt(transaction *sql.Tx, key aliasBaselineKey) (AliasBaseline, error) {
	baseline, err := scanAliasBaseline(transaction.QueryRow(aliasByPairPathSQL, key.root, key.home, key.alias))
	if errors.Is(err, sql.ErrNoRows) {
		return AliasBaseline{}, errMissingAliasBaseline(key.alias)
	}
	return baseline, err
}

// scanAndCommitAlias reads the row back through the transaction and commits,
// rolling back when the read fails so no open transaction leaks.
func scanAndCommitAlias(transaction *sql.Tx, key aliasBaselineKey) (AliasBaseline, error) {
	return commitStateRead(transaction, func(transaction *sql.Tx) (AliasBaseline, error) {
		return readAliasBaselineAt(transaction, key)
	})
}

func (store *Store) readAliasBaselines(statement, root, home string) ([]AliasBaseline, error) {
	if err := store.checkRepresentationCorruption(root, home); err != nil {
		return nil, err
	}
	rows, err := store.database.conn.Query(statement, root, home)
	if err != nil {
		return nil, fmt.Errorf("state: list alias baselines: %w", err)
	}
	defer rows.Close()
	var baselines []AliasBaseline
	for rows.Next() {
		baseline, err := scanAliasBaseline(rows)
		if err != nil {
			return nil, err
		}
		baselines = append(baselines, baseline)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list alias baselines: %w", err)
	}
	return CopyAliasBaselines(baselines), nil
}

func (store *Store) readAliasGroups(statement, root, home string) ([]string, error) {
	return store.queryStrings(statement, "alias groups", root, home)
}

func errMissingAliasBaseline(aliasPath string) error {
	return fmt.Errorf("state: no alias baseline for path %q", aliasPath)
}

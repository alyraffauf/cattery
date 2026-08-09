package state

import (
	"database/sql"
	"errors"
	"fmt"
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
	if baseline.GroupName != "" && !IsSlashRelative(baseline.GroupName) {
		return fmt.Errorf("state: alias baseline group %q is not a slash-relative path", baseline.GroupName)
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
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return AliasBaseline{}, err
	}
	if err := execIn(transaction, statement, now, repository.ID, key.alias); err != nil {
		return AliasBaseline{}, err
	}
	row, err := scanAndCommitAlias(transaction, key)
	if err != nil {
		return AliasBaseline{}, err
	}
	return row, nil
}

// scanAndCommitAlias reads the row back through the transaction and commits,
// rolling back when the read fails so no open transaction leaks.
func scanAndCommitAlias(transaction *sql.Tx, key aliasBaselineKey) (AliasBaseline, error) {
	baseline, err := scanAliasBaseline(transaction.QueryRow(aliasByPairPathSQL, key.root, key.home, key.alias))
	if errors.Is(err, sql.ErrNoRows) {
		_ = transaction.Rollback()
		return AliasBaseline{}, errMissingAliasBaseline(key.alias)
	}
	if err != nil {
		_ = transaction.Rollback()
		return AliasBaseline{}, err
	}
	if err := transaction.Commit(); err != nil {
		return AliasBaseline{}, err
	}
	return baseline, nil
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
	rows, err := store.database.conn.Query(statement, root, home)
	if err != nil {
		return nil, fmt.Errorf("state: list alias groups: %w", err)
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
		return nil, fmt.Errorf("state: list alias groups: %w", err)
	}
	return groups, nil
}

func errMissingAliasBaseline(aliasPath string) error {
	return fmt.Errorf("state: no alias baseline for path %q", aliasPath)
}

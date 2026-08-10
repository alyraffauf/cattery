package state

import (
	"database/sql"
	"errors"
	"fmt"
)

const aliasColumns = "repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at, retired_at"

// aliasUpsertSQL registers one active alias row, reactivating any retired row
// at the same alias path and refreshing its payload.
const aliasUpsertSQL = `
INSERT INTO aliases (repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at)
VALUES ((SELECT id FROM repositories WHERE root_path = ? AND home_path = ?),
        ?, ?, ?, ?, 'active', ?)
ON CONFLICT(repository_id, alias_path) DO UPDATE SET
    canonical_target_path = excluded.canonical_target_path,
    group_name = excluded.group_name, layer = excluded.layer,
    status = 'active', applied_at = excluded.applied_at, retired_at = NULL`

const aliasRetireSQL = "UPDATE aliases SET status = 'retired', retired_at = ? WHERE repository_id = ? AND alias_path = ?"

const aliasByPairPathSQL = `
SELECT ` + aliasColumns + `
FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? AND a.alias_path = ?`

const allAliasBaselinesSQL = `
SELECT ` + aliasColumns + `
FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? ORDER BY a.alias_path`

const allAliasGroupsSQL = `
SELECT DISTINCT group_name FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? ORDER BY group_name`

// aliasBaselineKey identifies one row by canonical pair and alias path.
type aliasBaselineKey struct {
	root, home, alias string
}

// aliasBatch carries the arguments of one alias-baseline transaction.
type aliasBatch struct {
	root, home, now string
	baseline        AliasBaseline
}

// UpsertAliasBaseline registers the canonical pair if needed and upserts one
// active alias row in a short transaction, stamping applied_at from the clock.
func (store *Store) UpsertAliasBaseline(root, home string, baseline AliasBaseline) (AliasBaseline, error) {
	root, home, err := prepareAliasBaseline(root, home, baseline)
	if err != nil {
		return AliasBaseline{}, err
	}
	now := formatTimestamp(store.now())
	batch := aliasBatch{root: root, home: home, now: now, baseline: baseline}
	key := aliasBaselineKey{root: root, home: home, alias: baseline.AliasPath}
	return runStateTransaction(store.database.conn,
		func(transaction *sql.Tx) error { return applyAliasBatch(transaction, batch) },
		func(transaction *sql.Tx) (AliasBaseline, error) {
			return scanAliasBaseline(transaction.QueryRow(aliasByPairPathSQL, key.root, key.home, key.alias))
		})
}

// RetireAliasBaseline marks one active row retired in its own transaction,
// retaining the payload for diagnostics and reactivation.
func (store *Store) RetireAliasBaseline(root, home, aliasPath string) (AliasBaseline, error) {
	return store.setAliasStatus(aliasBaselineKey{root: root, home: home, alias: aliasPath}, aliasRetireSQL)
}

// AliasBaseline reads one alias row of the canonical pair.
func (store *Store) AliasBaseline(root, home, aliasPath string) (AliasBaseline, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return AliasBaseline{}, err
	}
	if !IsSlashRelative(aliasPath) {
		return AliasBaseline{}, fmt.Errorf("state: alias path %q is not a slash-relative path", aliasPath)
	}
	baseline, err := scanAliasBaseline(store.database.conn.QueryRow(aliasByPairPathSQL, root, home, aliasPath))
	if errors.Is(err, sql.ErrNoRows) {
		return AliasBaseline{}, errMissingAliasBaseline(aliasPath)
	}
	if err != nil {
		return AliasBaseline{}, err
	}
	return baseline, nil
}

// AliasBaselines reads every alias row of the canonical pair in alias-path
// order, after rejecting cross-representation corruption.
func (store *Store) AliasBaselines(root, home string) ([]AliasBaseline, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readAliasBaselines(allAliasBaselinesSQL, root, home)
}

// AliasGroups lists the distinct group names of any alias row, so an
// explicitly selected retired-only group remains valid.
func (store *Store) AliasGroups(root, home string) ([]string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readAliasGroups(allAliasGroupsSQL, root, home)
}

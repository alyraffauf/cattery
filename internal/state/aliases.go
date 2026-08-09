package state

import (
	"database/sql"
	"errors"
	"fmt"
)

const aliasColumns = "repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at, retired_at"

// aliasUpsertSQL realizes one active alias row, reactivating any retired row at
// the same path and refreshing its payload.
const aliasUpsertSQL = `
INSERT INTO aliases (repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at)
VALUES ((SELECT id FROM repositories WHERE root_path = ? AND home_path = ?), ?, ?, ?, ?, 'active', ?)
ON CONFLICT(repository_id, alias_path) DO UPDATE SET
    canonical_target_path = excluded.canonical_target_path, group_name = excluded.group_name,
    layer = excluded.layer, status = 'active', applied_at = excluded.applied_at, retired_at = NULL`

const aliasRetireSQL = "UPDATE aliases SET status = 'retired', retired_at = ? WHERE repository_id = ? AND alias_path = ?"

const aliasReactivateSQL = "UPDATE aliases SET status = 'active', retired_at = NULL, applied_at = ? WHERE repository_id = ? AND alias_path = ?"

const aliasByPairPathSQL = `
SELECT a.repository_id, a.alias_path, a.canonical_target_path, a.group_name, a.layer,
       a.status, a.applied_at, a.retired_at
FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? AND a.alias_path = ?`

const allAliasBaselinesSQL = `
SELECT a.repository_id, a.alias_path, a.canonical_target_path, a.group_name, a.layer,
       a.status, a.applied_at, a.retired_at
FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? ORDER BY a.alias_path`

const activeAliasBaselinesSQL = `
SELECT a.repository_id, a.alias_path, a.canonical_target_path, a.group_name, a.layer,
       a.status, a.applied_at, a.retired_at
FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? AND a.status = 'active' ORDER BY a.alias_path`

const allAliasGroupsSQL = `
SELECT DISTINCT group_name FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? ORDER BY group_name`

const activeAliasGroupsSQL = `
SELECT DISTINCT group_name FROM aliases a JOIN repositories r ON r.id = a.repository_id
WHERE r.root_path = ? AND r.home_path = ? AND a.status = 'active' ORDER BY group_name`

// aliasBaselineKey identifies one row by canonical pair and alias path.
type aliasBaselineKey struct {
	root, home, alias string
}

// aliasBatch carries the arguments of one alias-baseline transaction.
type aliasBatch struct {
	root, home, now string
	baseline        AliasBaseline
}

// UpsertAliasBaseline registers the canonical pair if needed and realizes one
// active alias row in a short transaction, stamping applied_at from the clock.
func (store *Store) UpsertAliasBaseline(root, home string, baseline AliasBaseline) (AliasBaseline, error) {
	root, home, err := prepareAliasBaseline(root, home, baseline)
	if err != nil {
		return AliasBaseline{}, err
	}
	now := formatTimestamp(store.clock.Now())
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return AliasBaseline{}, err
	}
	if err := applyAliasBatch(transaction, aliasBatch{root: root, home: home, now: now, baseline: baseline}); err != nil {
		return AliasBaseline{}, err
	}
	return scanAndCommitAlias(transaction, aliasBaselineKey{root: root, home: home, alias: baseline.AliasPath})
}

// RetireAliasBaseline marks one active row retired in its own transaction,
// retaining the payload for diagnostics and reactivation.
func (store *Store) RetireAliasBaseline(root, home, alias string) (AliasBaseline, error) {
	return store.setAliasStatus(aliasBaselineKey{root: root, home: home, alias: alias}, aliasRetireSQL)
}

// ReactivateAliasBaseline restores a retired row to active without touching its
// retained payload, which the caller reconciles against.
func (store *Store) ReactivateAliasBaseline(root, home, alias string) (AliasBaseline, error) {
	return store.setAliasStatus(aliasBaselineKey{root: root, home: home, alias: alias}, aliasReactivateSQL)
}

func (store *Store) setAliasStatus(key aliasBaselineKey, statement string) (AliasBaseline, error) {
	repository, err := store.requireRepository(key.root, key.home)
	if err != nil {
		return AliasBaseline{}, err
	}
	if !IsSlashRelative(key.alias) {
		return AliasBaseline{}, fmt.Errorf("state: alias path %q is not a slash-relative path", key.alias)
	}
	now := formatTimestamp(store.clock.Now())
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return AliasBaseline{}, err
	}
	if err := execIn(transaction, statement, now, repository.ID, key.alias); err != nil {
		return AliasBaseline{}, err
	}
	return scanAndCommitAlias(transaction, key)
}

// AliasBaseline reads one alias row of the canonical pair.
func (store *Store) AliasBaseline(root, home, alias string) (AliasBaseline, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return AliasBaseline{}, err
	}
	if !IsSlashRelative(alias) {
		return AliasBaseline{}, fmt.Errorf("state: alias path %q is not a slash-relative path", alias)
	}
	baseline, err := scanAliasBaseline(store.database.conn.QueryRow(aliasByPairPathSQL, root, home, alias))
	if errors.Is(err, sql.ErrNoRows) {
		return AliasBaseline{}, errMissingAliasBaseline(alias)
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

// ActiveAliasBaselines reads the active alias rows of the canonical pair.
func (store *Store) ActiveAliasBaselines(root, home string) ([]AliasBaseline, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readAliasBaselines(activeAliasBaselinesSQL, root, home)
}

// AliasGroups lists the distinct group names of any alias row, so an explicitly
// selected retired-only group remains valid.
func (store *Store) AliasGroups(root, home string) ([]string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readAliasGroups(allAliasGroupsSQL, root, home)
}

// ActiveAliasGroups lists the distinct group names with an active alias row, so
// no-argument selection can exclude retired-only groups.
func (store *Store) ActiveAliasGroups(root, home string) ([]string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return nil, err
	}
	return store.readAliasGroups(activeAliasGroupsSQL, root, home)
}

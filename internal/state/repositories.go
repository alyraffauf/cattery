package state

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// repositoryColumns names the columns every repository read projects, in scan
// order.
const repositoryColumns = "id, root_path, home_path, is_default, created_at, last_seen_at"

// repositoryUpsertSQL registers the pair when absent and refreshes last_seen_at
// when present, never touching created_at or is_default of an existing row.
const repositoryUpsertSQL = `
INSERT INTO repositories (root_path, home_path, is_default, created_at, last_seen_at)
VALUES (?, ?, 0, ?, ?)
ON CONFLICT(root_path, home_path) DO UPDATE SET last_seen_at = excluded.last_seen_at`

// clearDefaultsSQL drops every default marker of a home so markDefaultSQL can
// promote exactly one row within the same transaction.
const clearDefaultsSQL = "UPDATE repositories SET is_default = 0 WHERE home_path = ?"

// markDefaultSQL promotes the canonical pair to the sole default of its home.
const markDefaultSQL = `
UPDATE repositories SET is_default = 1, last_seen_at = ?
WHERE root_path = ? AND home_path = ?`

// repositoryByPairSQL reads the single row of a canonical pair.
const repositoryByPairSQL = `
SELECT ` + repositoryColumns + `
FROM repositories
WHERE root_path = ? AND home_path = ?`

// defaultRepositorySQL reads the sole default row of a home.
const defaultRepositorySQL = `
SELECT ` + repositoryColumns + `
FROM repositories
WHERE home_path = ? AND is_default = 1`

// allRepositoriesSQL lists every row in deterministic bytewise order.
const allRepositoriesSQL = `
SELECT ` + repositoryColumns + `
FROM repositories
ORDER BY root_path, home_path`

// RegisterRepository records the canonical (root, home) pair and refreshes its
// last-seen timestamp, returning the stored row. Re-registering an existing
// pair is idempotent: the row id, creation time, and default flag survive.
func (store *Store) RegisterRepository(root, home string) (Repository, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return Repository{}, err
	}
	now := formatTimestamp(store.now())
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return Repository{}, err
	}
	if err := execIn(transaction, repositoryUpsertSQL, root, home, now, now); err != nil {
		return Repository{}, err
	}
	repository, err := scanAndCommit(transaction, root, home)
	if err != nil {
		return Repository{}, err
	}
	return repository, nil
}

// SetDefaultRepository registers the canonical pair and promotes it to the
// sole default of its home in one transaction, demoting any previous default.
func (store *Store) SetDefaultRepository(root, home string) (Repository, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return Repository{}, err
	}
	now := formatTimestamp(store.now())
	transaction, err := store.database.conn.Begin()
	if err != nil {
		return Repository{}, err
	}
	batches := [][]any{{root, home, now, now}, {home}, {now, root, home}}
	if err := execBatch(transaction, []string{repositoryUpsertSQL, clearDefaultsSQL, markDefaultSQL}, batches); err != nil {
		return Repository{}, err
	}
	repository, err := scanAndCommit(transaction, root, home)
	if err != nil {
		return Repository{}, err
	}
	return repository, nil
}

// LookupRepository reads the canonical pair without registering it. A missing
// pair returns an error so apply and add can treat absence as "no baselines".
func (store *Store) LookupRepository(root, home string) (Repository, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return Repository{}, err
	}
	repository, err := scanRepository(store.database.conn.QueryRow(repositoryByPairSQL, root, home))
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, errUnknownRepository(root, home)
	}
	if err != nil {
		return Repository{}, err
	}
	return repository, nil
}

// DefaultRepository reads the sole default repository of a canonical home.
func (store *Store) DefaultRepository(home string) (Repository, error) {
	home, err := pathsafe.CanonicalRoot(home)
	if err != nil {
		return Repository{}, fmt.Errorf("state: home: %w", err)
	}
	repository, err := scanRepository(store.database.conn.QueryRow(defaultRepositorySQL, home))
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, errDefaultMissing(home)
	}
	if err != nil {
		return Repository{}, err
	}
	return repository, nil
}

// Repositories returns every repository row sorted by root path then home
// path, as a defensive copy.
func (store *Store) Repositories() ([]Repository, error) {
	rows, err := store.database.conn.Query(allRepositoriesSQL)
	if err != nil {
		return nil, fmt.Errorf("state: list repositories: %w", err)
	}
	defer rows.Close()
	var repositories []Repository
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list repositories: %w", err)
	}
	return CopyRepositories(repositories), nil
}

// scanner abstracts sql.Row and sql.Rows so one row decoder serves both.
type scanner interface {
	Scan(dest ...any) error
}

// scanRepository decodes one repository row from the scanner.
func scanRepository(source scanner) (Repository, error) {
	var repository Repository
	var created, lastSeen string
	err := source.Scan(&repository.ID, &repository.RootPath, &repository.HomePath,
		&repository.IsDefault, &created, &lastSeen)
	if err != nil {
		return Repository{}, fmt.Errorf("state: scan repository: %w", err)
	}
	return decodeRepositoryTimes(repository, created, lastSeen)
}

// decodeRepositoryTimes parses the stored RFC3339Nano timestamps onto row.
func decodeRepositoryTimes(row Repository, created, lastSeen string) (Repository, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Repository{}, fmt.Errorf("state: repository created_at: %w", err)
	}
	lastSeenAt, err := time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil {
		return Repository{}, fmt.Errorf("state: repository last_seen_at: %w", err)
	}
	row.CreatedAt = createdAt
	row.LastSeenAt = lastSeenAt
	return row, nil
}

// scanAndCommit reads the pair back and commits, rolling back when the read
// fails so a caller never leaves an open transaction behind.
func scanAndCommit(transaction *sql.Tx, root, home string) (Repository, error) {
	repository, err := scanRepository(transaction.QueryRow(repositoryByPairSQL, root, home))
	if err != nil {
		_ = transaction.Rollback()
		return Repository{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Repository{}, err
	}
	return repository, nil
}

// execBatch executes each statement with its own argument batch in order,
// stopping at the first failure. Statement and batch indices correspond.
func execBatch(transaction *sql.Tx, statements []string, batches [][]any) error {
	for index, statement := range statements {
		if err := execIn(transaction, statement, batches[index]...); err != nil {
			return err
		}
	}
	return nil
}

// execIn executes one statement in the transaction, rolling back and wrapping
// the error when the statement fails.
func execIn(transaction *sql.Tx, statement string, arguments ...any) error {
	if _, err := transaction.Exec(statement, arguments...); err != nil {
		_ = transaction.Rollback()
		return fmt.Errorf("state: %s: %w", statement, err)
	}
	return nil
}

// canonicalRepositoryPair resolves both paths to canonical absolute form so
// only canonical pairs are ever stored or matched.
func canonicalRepositoryPair(root, home string) (string, string, error) {
	canonicalRoot, err := pathsafe.CanonicalRoot(root)
	if err != nil {
		return "", "", fmt.Errorf("state: repository root: %w", err)
	}
	canonicalHome, err := pathsafe.CanonicalRoot(home)
	if err != nil {
		return "", "", fmt.Errorf("state: home: %w", err)
	}
	return canonicalRoot, canonicalHome, nil
}

// formatTimestamp renders a clock reading in the schema's UTC text form.
func formatTimestamp(when time.Time) string {
	return when.UTC().Format(time.RFC3339Nano)
}

func errUnknownRepository(root, home string) error {
	return fmt.Errorf("state: repository %q for home %q is not registered", root, home)
}

func errDefaultMissing(home string) error {
	return fmt.Errorf(
		"state: no default repository for home %q; run `cattery init PATH` or pass --repo",
		home)
}

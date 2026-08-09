package state

import (
	"database/sql"
	"fmt"

	_ "embed"
)

// initialMigrationSQL is the embedded Section 8.4 schema. It is one of the two
// package-variable exceptions permitted by Section 12.1.
//
//go:embed migrations/001_initial.sql
var initialMigrationSQL string

// currentSchemaVersion is the schema version this build's migrations produce.
// PRAGMA user_version is managed against it.
const currentSchemaVersion = 1

// Migrate applies the embedded schema migration to the database when needed.
// Re-running on a current database is a no-op. An unknown newer schema
// (user_version greater than current) is rejected so a newer Cattery never
// silently corrupts an older binary's state.
func Migrate(database *Database) error {
	version, err := readUserVersion(database)
	if err != nil {
		return err
	}
	if version > currentSchemaVersion {
		return errUnknownSchema(version)
	}
	if version == currentSchemaVersion {
		return nil
	}
	return applyMigration(database)
}

func applyMigration(database *Database) error {
	transaction, err := beginExclusive(database)
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(initialMigrationSQL); err != nil {
		_ = transaction.Rollback()
		return fmt.Errorf("state: apply migration: %w", err)
	}
	if err := setUserVersion(transaction, currentSchemaVersion); err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Commit()
}

func beginExclusive(database *Database) (*sql.Tx, error) {
	if _, err := database.conn.Exec("PRAGMA locking_mode = EXCLUSIVE"); err != nil {
		return nil, fmt.Errorf("state: exclusive locking mode: %w", err)
	}
	return database.conn.Begin()
}

func readUserVersion(database *Database) (int, error) {
	var version int
	if err := database.conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("state: read user_version: %w", err)
	}
	return version, nil
}

func setUserVersion(transaction *sql.Tx, version int) error {
	statement := fmt.Sprintf("PRAGMA user_version = %d", version)
	if _, err := transaction.Exec(statement); err != nil {
		return fmt.Errorf("state: set user_version: %w", err)
	}
	return nil
}

func errUnknownSchema(version int) error {
	return fmt.Errorf(
		"state: database schema version %d is newer than supported version %d",
		version, currentSchemaVersion)
}

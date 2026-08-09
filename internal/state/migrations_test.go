package state

import (
	"database/sql"
	"fmt"
	"testing"
)

func TestStateMigration(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"fresh database applies", testMigrationFresh},
		{"current database is no-op", testMigrationIdempotent},
		{"forced failure rolls back", testMigrationRollback},
		{"interrupted migration recovers", testMigrationRecoversAfterInterruption},
		{"newer schema rejected", testMigrationRejectsNewer},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testMigrationFresh(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	assertSchemaCurrent(t, database.conn)
}

func testMigrationIdempotent(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	if err := Migrate(database); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(database); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	assertSchemaCurrent(t, database.conn)
}

func testMigrationRollback(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	execOn(t, database.conn, "CREATE TABLE metadata (key TEXT)")
	if err := Migrate(database); err == nil {
		t.Fatal("Migrate succeeded against a conflicting schema")
	}
	if version := userVersion(t, database.conn); version != 0 {
		t.Fatalf("user_version = %d after rollback, want 0", version)
	}
	if tableExists(t, database.conn, "repositories") {
		t.Fatal("rollback left a partial repositories table")
	}
}

func testMigrationRecoversAfterInterruption(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	execOn(t, database.conn, "CREATE TABLE metadata (key TEXT)")
	if err := Migrate(database); err == nil {
		t.Fatal("Migrate succeeded against an interrupted schema")
	}
	execOn(t, database.conn, "DROP TABLE metadata")
	if err := Migrate(database); err != nil {
		t.Fatalf("Migrate after interruption cleanup: %v", err)
	}
	assertSchemaCurrent(t, database.conn)
}

func testMigrationRejectsNewer(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	statement := fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion+1)
	execOn(t, database.conn, statement)
	if err := Migrate(database); err == nil {
		t.Fatal("Migrate accepted a newer schema version")
	}
	if tableExists(t, database.conn, "metadata") {
		t.Fatal("newer-schema rejection must not create any tables")
	}
}

func assertSchemaCurrent(t *testing.T, conn *sql.DB) {
	t.Helper()
	if version := userVersion(t, conn); version != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", version, currentSchemaVersion)
	}
	for _, table := range []string{"metadata", "repositories", "files", "aliases"} {
		if !tableExists(t, conn, table) {
			t.Fatalf("table %q missing after migration", table)
		}
	}
}

func userVersion(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var version int
	if err := conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return version
}

func tableExists(t *testing.T, conn *sql.DB, name string) bool {
	t.Helper()
	var found string
	query := "SELECT name FROM sqlite_master WHERE type = ? AND name = ?"
	if err := conn.QueryRow(query, "table", name).Scan(&found); err != nil {
		if err == sql.ErrNoRows {
			return false
		}
		t.Fatalf("query table %s: %v", name, err)
	}
	return found == name
}

func execOn(t *testing.T, conn *sql.DB, statement string) {
	t.Helper()
	if _, err := conn.Exec(statement); err != nil {
		t.Fatalf("%q: %v", statement, err)
	}
}

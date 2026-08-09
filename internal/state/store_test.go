package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLifecycle(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"construction touches no filesystem", testStoreConstructionClean},
		{"acquire opens and closes", testStoreAcquireAndClose},
		{"acquire creates no managed rows", testStoreAcquireLeavesNoRows},
		{"path resolve failure", testStorePathResolveFailure},
		{"lock failure leaves no database", testStoreLockFailure},
		{"open failure releases lock", testStoreOpenFailure},
		{"migrate failure releases lock", testStoreMigrateFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testStoreConstructionClean(t *testing.T) {
	deps := tempDependencies(t)
	store := NewStore(deps)
	if store.Database() != nil {
		t.Fatal("Database non-nil before Acquire")
	}
	if store.Clock() == nil {
		t.Fatal("Clock nil")
	}
	if _, err := os.Stat(catteryDirFor(t, deps)); !os.IsNotExist(err) {
		t.Fatalf("NewStore created the cattery directory before Acquire: %v", err)
	}
}

func testStoreAcquireAndClose(t *testing.T) {
	store := NewStore(tempDependencies(t))
	if err := store.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if store.Database() == nil {
		t.Fatal("Database nil after Acquire")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if store.Database() != nil {
		t.Fatal("Database non-nil after Close")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func testStoreAcquireLeavesNoRows(t *testing.T) {
	store := NewStore(tempDependencies(t))
	if err := store.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer store.Close()
	conn := store.Database().conn
	for _, table := range []string{"repositories", "files", "aliases"} {
		if count := rowCount(t, conn, table); count != 0 {
			t.Fatalf("table %s has %d rows after Acquire, want 0", table, count)
		}
	}
}

func testStorePathResolveFailure(t *testing.T) {
	store := NewStore(Dependencies{StateHome: "relative/path"})
	if err := store.Acquire(context.Background()); err == nil {
		t.Fatal("Acquire accepted a relative state home")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close after failed Acquire: %v", err)
	}
}

func testStoreLockFailure(t *testing.T) {
	deps := tempDependencies(t)
	directory := ensureCatteryDir(t, deps)
	holder := NewLock(filepath.Join(directory, stateLockFileName))
	if err := holder.Acquire(); err != nil {
		t.Fatalf("holder Acquire: %v", err)
	}
	defer holder.Release()
	store := NewStore(deps)
	if err := store.Acquire(context.Background()); err == nil {
		t.Fatal("Acquire succeeded while another holder held the lock")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if pathExists(t, databasePathFor(t, deps)) {
		t.Fatal("database file created while the lock was held")
	}
}

func testStoreOpenFailure(t *testing.T) {
	deps := tempDependencies(t)
	directory := ensureCatteryDir(t, deps)
	if err := os.Mkdir(databasePathFor(t, deps), stateFileMode); err != nil {
		t.Fatal(err)
	}
	store := NewStore(deps)
	if err := store.Acquire(context.Background()); err == nil {
		t.Fatal("Acquire accepted a non-regular database entry")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	replacer := NewLock(filepath.Join(directory, stateLockFileName))
	if err := replacer.Acquire(); err != nil {
		t.Fatalf("lock not released after open failure: %v", err)
	}
	_ = replacer.Release()
}

func testStoreMigrateFailure(t *testing.T) {
	deps := tempDependencies(t)
	seedNewerSchema(t, deps)
	store := NewStore(deps)
	if err := store.Acquire(context.Background()); err == nil {
		t.Fatal("Acquire succeeded against a newer schema")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	replacer := NewLock(filepath.Join(catteryDirFor(t, deps), stateLockFileName))
	if err := replacer.Acquire(); err != nil {
		t.Fatalf("lock not released after migrate failure: %v", err)
	}
	_ = replacer.Release()
}

func tempDependencies(t *testing.T) Dependencies {
	t.Helper()
	return Dependencies{StateHome: t.TempDir(), Clock: SystemClock{}}
}

func catteryDirFor(t *testing.T, deps Dependencies) string {
	t.Helper()
	directory, err := resolveCatteryDirectory(deps.StateHome)
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func ensureCatteryDir(t *testing.T, deps Dependencies) string {
	t.Helper()
	directory := catteryDirFor(t, deps)
	if err := os.MkdirAll(directory, stateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, stateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	return directory
}

func databasePathFor(t *testing.T, deps Dependencies) string {
	t.Helper()
	return filepath.Join(catteryDirFor(t, deps), stateDatabaseFileName)
}

func seedNewerSchema(t *testing.T, deps Dependencies) {
	t.Helper()
	database := NewDatabase(databasePathFor(t, deps))
	if err := database.Open(); err != nil {
		t.Fatal(err)
	}
	statement := fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion+1)
	execOn(t, database.conn, statement)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}

func pathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("Lstat %q: %v", path, err)
	return false
}

func rowCount(t *testing.T, conn *sql.DB, table string) int64 {
	t.Helper()
	var count int64
	query := "SELECT COUNT(*) FROM " + table
	if err := conn.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

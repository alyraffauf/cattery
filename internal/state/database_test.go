package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDatabaseOpen(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"construction touches no filesystem", testConstructionTouchesNoFilesystem},
		{"creates private modes", testCreatesPrivateModes},
		{"restrictive umask keeps modes", testUmaskStaysRestrictive},
		{"pragmas applied", testPragmasApplied},
		{"rejects symlink database", testRejectsSymlinkDatabase},
		{"rejects non-regular database", testRejectsNonRegularDatabase},
		{"rejects wrong directory mode", testRejectsWrongDirectoryMode},
		{"rejects wrong file mode", testRejectsWrongFileMode},
		{"open failure leaves no connection", testOpenFailureLeavesNoConnection},
		{"rejects relative state home", testRejectsRelativeStateHome},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testConstructionTouchesNoFilesystem(t *testing.T) {
	path := tempDatabasePath(t)
	database := NewDatabase(path)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("NewDatabase created files: %v", err)
	}
	if database.Path() != path {
		t.Fatalf("Path = %q, want %q", database.Path(), path)
	}
}

func testCreatesPrivateModes(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	directoryMode := modeOf(t, filepath.Dir(database.Path()))
	if directoryMode != stateDirectoryMode {
		t.Fatalf("directory mode %o, want %o", directoryMode, stateDirectoryMode)
	}
	if modeOf(t, database.Path()) != stateFileMode {
		t.Fatalf("file mode %o, want %o", modeOf(t, database.Path()), stateFileMode)
	}
}

func testUmaskStaysRestrictive(t *testing.T) {
	saved := syscall.Umask(0o077)
	defer syscall.Umask(saved)
	database := openTestDatabase(t)
	defer database.Close()
	directoryMode := modeOf(t, filepath.Dir(database.Path()))
	if directoryMode != stateDirectoryMode {
		t.Fatalf("directory mode %o under restrictive umask, want %o", directoryMode, stateDirectoryMode)
	}
	if modeOf(t, database.Path()) != stateFileMode {
		t.Fatalf("file mode under restrictive umask, want %o", stateFileMode)
	}
}

func testPragmasApplied(t *testing.T) {
	database := openTestDatabase(t)
	defer database.Close()
	expectations := []pragmaExpectation{
		{name: "foreign_keys", want: "1"},
		{name: "busy_timeout", want: "5000"},
		{name: "journal_mode", want: "wal"},
		{name: "synchronous", want: "2"},
	}
	for _, expectation := range expectations {
		assertPragma(t, database.conn, expectation)
	}
}

func testRejectsSymlinkDatabase(t *testing.T) {
	target := prepareCatteryChild(t)
	if err := os.Symlink("/dev/null", target); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := NewDatabase(target).Open(); err == nil {
		t.Fatal("Open accepted a symlink database file")
	}
}

func testRejectsNonRegularDatabase(t *testing.T) {
	target := prepareCatteryChild(t)
	if err := os.Mkdir(target, stateFileMode); err != nil {
		t.Fatal(err)
	}
	if err := NewDatabase(target).Open(); err == nil {
		t.Fatal("Open accepted a non-regular database entry")
	}
}

func testRejectsWrongDirectoryMode(t *testing.T) {
	directory := filepath.Join(t.TempDir(), catteryDirectoryName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, stateDatabaseFileName)
	if err := NewDatabase(target).Open(); err == nil {
		t.Fatal("Open accepted a directory with the wrong mode")
	}
}

func testRejectsWrongFileMode(t *testing.T) {
	target := prepareCatteryChild(t)
	createFile(t, target, 0o644)
	if err := NewDatabase(target).Open(); err == nil {
		t.Fatal("Open accepted a database file with the wrong mode")
	}
}

func testOpenFailureLeavesNoConnection(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "blocker")
	createFile(t, parent, 0o600)
	target := filepath.Join(parent, catteryDirectoryName, stateDatabaseFileName)
	database := NewDatabase(target)
	if err := database.Open(); err == nil {
		t.Fatal("Open succeeded against a path whose parent is a file")
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close after failed Open: %v", err)
	}
}

func testRejectsRelativeStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative/path")
	if _, err := resolveStateHome("relative/path"); err == nil {
		t.Fatal("resolveStateHome accepted a relative explicit home")
	}
	home := t.TempDir()
	got, err := resolveStateHome(home)
	if err != nil {
		t.Fatalf("resolveStateHome absolute: %v", err)
	}
	if got != home {
		t.Fatalf("resolveStateHome = %q, want %q", got, home)
	}
}

func tempDatabasePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), catteryDirectoryName, stateDatabaseFileName)
}

func openTestDatabase(t *testing.T) *Database {
	t.Helper()
	database := NewDatabase(tempDatabasePath(t))
	if err := database.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return database
}

func prepareCatteryChild(t *testing.T) string {
	t.Helper()
	return filepath.Join(prepareCatteryDirectory(t), stateDatabaseFileName)
}

func createFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

type pragmaExpectation struct {
	name string
	want string
}

func assertPragma(t *testing.T, conn *sql.DB, expectation pragmaExpectation) {
	t.Helper()
	var got any
	query := "PRAGMA " + expectation.name
	if err := conn.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s: %v", expectation.name, err)
	}
	rendered := fmt.Sprint(got)
	if rendered != expectation.want {
		t.Fatalf("PRAGMA %s = %q, want %q", expectation.name, rendered, expectation.want)
	}
}

package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"

	"github.com/alyraffauf/cattery/internal/pathsafe"

	// The modernc.org/sqlite driver registers itself under the name "sqlite"
	// so database/sql can open a pure-Go SQLite connection.
	_ "modernc.org/sqlite"
)

// Filesystem placement and required modes for the Cattery state directory
// (PLAN.md Section 8.1). The directory is private to the owning user; the
// database and lock files are read-write but never searchable by others.
const (
	catteryDirectoryName              = "cattery"
	stateDatabaseFileName             = "state.db"
	stateLockFileName                 = "cattery.lock"
	stateDirectoryMode    os.FileMode = 0o700
	stateFileMode         os.FileMode = 0o600
)

// sqliteDriverName is the registration name modernc.org/sqlite uses.
const sqliteDriverName = "sqlite"

// Database is an open SQLite state connection. Construction stores the resolved
// path only; Open performs every filesystem effect so a freshly constructed
// Database touches nothing on disk.
type Database struct {
	path string
	conn *sql.DB
}

// NewDatabase constructs a handle bound to path. No filesystem access occurs
// until Open runs.
func NewDatabase(path string) *Database {
	return &Database{path: path}
}

// Path returns the resolved database path the handle is bound to.
func (database *Database) Path() string {
	return database.path
}

// Open prepares the private state directory and database file, opens SQLite with
// a single connection, and applies the Section 8.5 PRAGMAs. It performs no
// locking and no schema migration.
func (database *Database) Open() error {
	if err := prepareStateDirectory(filepath.Dir(database.path)); err != nil {
		return err
	}
	if err := preparePrivateFile(database.path); err != nil {
		return err
	}
	conn, err := openConnection(database.path)
	if err != nil {
		return err
	}
	database.conn = conn
	return nil
}

// Close releases the underlying connection pool and is safe to call on a handle
// that was never opened or whose Open failed.
func (database *Database) Close() error {
	if database.conn == nil {
		return nil
	}
	err := database.conn.Close()
	database.conn = nil
	return err
}

// ResolveDatabasePath resolves the canonical absolute path of the SQLite state
// database beneath $XDG_STATE_HOME/cattery/state.db. A relative XDG_STATE_HOME
// is rejected rather than resolved against the working directory.
func ResolveDatabasePath() (string, error) {
	home, err := resolveStateHome("")
	if err != nil {
		return "", err
	}
	directory, err := resolveCatteryDirectory(home)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, stateDatabaseFileName), nil
}

// resolveStateHome returns the validated state home. An explicit absolute path
// overrides the environment; an empty explicit reads XDG_STATE_HOME and rejects
// a relative value before falling back to the XDG default.
func resolveStateHome(explicit string) (string, error) {
	if explicit == "" {
		return resolveEnvStateHome()
	}
	if !filepath.IsAbs(explicit) {
		return "", errRelativeStateHome(explicit)
	}
	return explicit, nil
}

func resolveEnvStateHome() (string, error) {
	raw := os.Getenv("XDG_STATE_HOME")
	if raw != "" && !filepath.IsAbs(raw) {
		return "", errRelativeStateHome(raw)
	}
	return xdg.StateHome, nil
}

func errRelativeStateHome(value string) error {
	return fmt.Errorf("state: state home %q is not absolute", value)
}

// resolveCatteryDirectory resolves the cattery directory through its nearest
// existing canonical ancestor so a missing path is pinned before creation.
func resolveCatteryDirectory(home string) (string, error) {
	return pathsafe.CanonicalRoot(filepath.Join(home, catteryDirectoryName))
}

// prepareStateDirectory creates the private directory when absent and rejects an
// existing entry that is not a real directory with the required mode.
func prepareStateDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err == nil {
		return verifyStateDirectory(directory, info)
	}
	if !os.IsNotExist(err) {
		return err
	}
	return createPrivateDirectory(directory)
}

func verifyStateDirectory(directory string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errNotDirectory(directory, info.Mode())
	}
	if info.Mode().Perm() != stateDirectoryMode {
		return errWrongDirectoryMode(directory, info.Mode().Perm())
	}
	return nil
}

func createPrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, stateDirectoryMode); err != nil {
		return err
	}
	return os.Chmod(directory, stateDirectoryMode)
}

func openConnection(path string) (*sql.DB, error) {
	conn, err := sql.Open(sqliteDriverName, path)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err := applyPragmas(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func applyPragmas(conn *sql.DB) error {
	for _, pragma := range sqlitePragmas() {
		if _, err := conn.Exec(pragma); err != nil {
			return fmt.Errorf("state: %s: %w", pragma, err)
		}
	}
	return nil
}

func sqlitePragmas() []string {
	return []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
	}
}

func errNotDirectory(directory string, mode os.FileMode) error {
	return fmt.Errorf(
		"state: %q is not a regular directory (mode %v); expected %v",
		directory, mode, stateDirectoryMode)
}

func errWrongDirectoryMode(directory string, mode os.FileMode) error {
	return fmt.Errorf(
		"state: %q has mode %v; run `chmod %o %q` to correct it",
		directory, mode, stateDirectoryMode, directory)
}

func errNotRegular(path string, mode os.FileMode) error {
	return fmt.Errorf(
		"state: %q is not a regular file (mode %v); remove the non-regular entry",
		path, mode)
}

func errWrongFileMode(path string, mode os.FileMode) error {
	return fmt.Errorf(
		"state: %q has mode %v; run `chmod %o %q` to correct it",
		path, mode, stateFileMode, path)
}

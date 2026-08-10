package state

import (
	"context"
	"path/filepath"
	"time"
)

// Dependencies bundles the injectable seams of a Store. StateHome, when set,
// overrides the XDG-derived state directory; when empty, resolution reads
// XDG_STATE_HOME and rejects a relative value. Now supplies timestamps and
// defaults to the wall clock when nil.
type Dependencies struct {
	StateHome string
	Now       func() time.Time
}

// Store coordinates the state lifecycle: path resolution, advisory locking,
// database opening, and schema migration. Construction performs no filesystem
// access; Acquire performs all of it in the only permitted order and Close
// reverses it.
type Store struct {
	stateHome string
	now       func() time.Time
	lock      *Lock
	database  *Database
}

// NewStore constructs a lazy Store bound to the injected dependencies. It opens
// no SQLite connection, acquires no lock, creates no path, and inspects no
// repository: every effect begins inside Acquire.
func NewStore(deps Dependencies) *Store {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &Store{stateHome: deps.StateHome, now: now}
}

// Database returns the opened database handle, or nil before Acquire succeeds.
func (store *Store) Database() *Database {
	return store.database
}

// EnsureAcquired lazily opens the state store for application adapters that do
// not receive a context. Repeated calls after a successful acquisition are
// no-ops; construction and version/help paths remain side-effect-free.
func (store *Store) EnsureAcquired() error {
	if store.database != nil {
		return nil
	}
	return store.Acquire(context.Background())
}

// Acquire resolves the canonical state directory, creates it, acquires the
// advisory lock, opens the database, and applies any required migration. On any
// failure it releases what was acquired so the Store is safe to drop.
func (store *Store) Acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := store.catteryDirectory()
	if err != nil {
		return err
	}
	if err := prepareStateDirectory(directory); err != nil {
		return err
	}
	store.lock = NewLock(filepath.Join(directory, stateLockFileName))
	if err := store.lock.Acquire(); err != nil {
		store.lock = nil
		return err
	}
	if err := store.openDatabase(directory); err != nil {
		store.releaseAfterFailedAcquire()
		return err
	}
	return nil
}

// Close releases the database connection and advisory lock in reverse
// acquisition order. It is safe to call on a Store that never Acquired.
func (store *Store) Close() error {
	var first error
	if store.database != nil {
		first = store.database.Close()
		store.database = nil
	}
	if store.lock != nil {
		releaseErr := store.lock.Release()
		if releaseErr != nil && first == nil {
			first = releaseErr
		}
		store.lock = nil
	}
	return first
}

func (store *Store) openDatabase(directory string) error {
	store.database = NewDatabase(filepath.Join(directory, stateDatabaseFileName))
	if err := store.database.Open(); err != nil {
		return err
	}
	if err := Migrate(store.database); err != nil {
		return err
	}
	return nil
}

func (store *Store) releaseAfterFailedAcquire() {
	if store.database != nil {
		_ = store.database.Close()
		store.database = nil
	}
	if store.lock != nil {
		_ = store.lock.Release()
		store.lock = nil
	}
}

func (store *Store) catteryDirectory() (string, error) {
	home, err := resolveStateHome(store.stateHome)
	if err != nil {
		return "", err
	}
	return resolveCatteryDirectory(home)
}

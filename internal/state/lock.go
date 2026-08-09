package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gofrs/flock"
)

// Lock is an advisory process lock over the Cattery state directory. Only one
// Cattery process may hold it at a time so concurrent commands never race on
// state. Construction is side-effect-free; Acquire performs the filesystem
// work and immediate exclusive acquisition.
type Lock struct {
	path  string
	flock *flock.Flock
}

// NewLock constructs a handle bound to the lock file path. No filesystem access
// occurs until Acquire runs.
func NewLock(path string) *Lock {
	return &Lock{path: path}
}

// Path returns the resolved lock file path the handle is bound to.
func (lock *Lock) Path() string {
	return lock.path
}

// Acquire prepares the lock file with the required mode and acquires an
// exclusive advisory lock, failing immediately when another Cattery process
// holds it. After acquisition it writes the current PID to the lock file for
// diagnostics.
func (lock *Lock) Acquire() error {
	return lock.acquire(writeProcessID)
}

func (lock *Lock) acquire(writePID func(string) error) error {
	if err := preparePrivateFile(lock.path); err != nil {
		return err
	}
	lock.flock = flock.New(lock.path)
	locked, err := lock.flock.TryLock()
	if err != nil {
		return fmt.Errorf("state: lock %q: %w", lock.path, err)
	}
	if !locked {
		return errLockHeld(lock.path)
	}
	if err := writePID(lock.path); err != nil {
		_ = lock.Release()
		return err
	}
	return nil
}

// Release releases the advisory lock. It is idempotent: calling Release on a
// lock that was never acquired or already released is a no-op.
func (lock *Lock) Release() error {
	if lock.flock == nil {
		return nil
	}
	err := lock.flock.Unlock()
	lock.flock = nil
	return err
}

// ResolveLockPath resolves the canonical absolute path of the advisory lock
// beneath $XDG_STATE_HOME/cattery/cattery.lock. A relative XDG_STATE_HOME is
// rejected rather than resolved against the working directory.
func ResolveLockPath() (string, error) {
	home, err := resolveStateHome("")
	if err != nil {
		return "", err
	}
	directory, err := resolveCatteryDirectory(home)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, stateLockFileName), nil
}

// writeProcessID writes the current PID to the lock file for diagnostics and
// restores the required file mode so a restrictive umask cannot widen it.
func writeProcessID(path string) error {
	content := []byte(strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(path, content, stateFileMode); err != nil {
		return err
	}
	return os.Chmod(path, stateFileMode)
}

func errLockHeld(path string) error {
	return fmt.Errorf("state: another Cattery process holds %q", path)
}

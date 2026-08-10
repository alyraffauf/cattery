package state

import (
	"fmt"
	"os"
	"strconv"
	"syscall"

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
	held := flock.New(lock.path)
	locked, err := held.TryLock()
	if err != nil {
		return fmt.Errorf("state: lock %q: %w", lock.path, err)
	}
	if !locked {
		return errLockHeld(lock.path)
	}
	lock.flock = held
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

// writeProcessID writes the current PID to the lock file for diagnostics and
// restores the required file mode so a restrictive umask cannot widen it.
func writeProcessID(path string) error {
	content := []byte(strconv.Itoa(os.Getpid()))
	handle, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NOFOLLOW, stateFileMode)
	if err != nil {
		return err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return err
	}
	if err := verifyPrivateFile(path, info); err != nil {
		return err
	}
	if err := writePIDContent(handle, content, path); err != nil {
		return err
	}
	if err := handle.Chmod(stateFileMode); err != nil {
		return err
	}
	return nil
}

func writePIDContent(handle *os.File, content []byte, path string) error {
	if err := handle.Truncate(0); err != nil {
		return err
	}
	written, err := handle.Write(content)
	if err != nil {
		return err
	}
	if written != len(content) {
		return fmt.Errorf("state: short PID write to %q", path)
	}
	return nil
}

func errLockHeld(path string) error {
	return fmt.Errorf("state: another Cattery process holds %q", path)
}

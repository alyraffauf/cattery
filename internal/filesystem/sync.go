package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// SyncHandle is the sync/close lifecycle of one opened entry. *os.File
// satisfies it; tests inject fakes to fail the sync and close steps.
type SyncHandle interface {
	Sync() error
	Close() error
}

// CommitFile makes a still-open temporary file durable: sync while open so
// bytes and final mode precede the barrier, then close. It always closes so
// no descriptor leaks; a sync failure is reported because the write is not
// durable (PLAN.md Section 7.2 steps 7-8).
func CommitFile(ctx context.Context, handle SyncHandle) error {
	if err := ctx.Err(); err != nil {
		_ = handle.Close()
		return err
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return fmt.Errorf("filesystem: sync temporary file: %w", err)
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("filesystem: close temporary file: %w", err)
	}
	return nil
}

// SyncError reports a failed directory durability barrier. Unsupported is
// true when the filesystem refused directory sync itself, which callers
// report as a partial operation rather than a racing mutation
// (PLAN.md Section 7.2 step 11).
type SyncError struct {
	Unsupported bool
	Op          string
	Cause       error
}

func (e *SyncError) Error() string {
	if e.Unsupported {
		return fmt.Sprintf("filesystem: directory sync unsupported: %v", e.Cause)
	}
	return fmt.Sprintf("filesystem: directory %s failed: %v", e.Op, e.Cause)
}

// Unwrap exposes Cause to errors.Is and errors.As.
func (e *SyncError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// DirectorySyncer opens, syncs, and closes one parent directory after a
// rename so the caller may commit state only once the entry is durable.
type DirectorySyncer struct {
	open func(path string) (SyncHandle, error)
}

// NewDirectorySyncer returns a syncer backed by os.Open.
func NewDirectorySyncer() *DirectorySyncer {
	return &DirectorySyncer{open: func(path string) (SyncHandle, error) {
		return os.Open(path)
	}}
}

// Sync makes the parent directory of a completed rename durable: open,
// sync, close. Any failure returns *SyncError; an unsupported sync
// (EINVAL/ENOTSUP/EOPNOTSUPP) is flagged separately for diagnostics.
func (s *DirectorySyncer) Sync(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := s.open(path)
	if err != nil {
		return &SyncError{Op: "open", Cause: err}
	}
	if err := handle.Sync(); err != nil {
		_ = handle.Close()
		return &SyncError{Op: "sync", Unsupported: unsupportedSync(err), Cause: err}
	}
	if err := handle.Close(); err != nil {
		return &SyncError{Op: "close", Cause: err}
	}
	return nil
}

// unsupportedSync classifies filesystems that refuse directory fsync, which
// cannot be repaired by retrying.
func unsupportedSync(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}

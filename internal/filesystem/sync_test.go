package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

// fakeSyncHandle is a SyncHandle whose steps fail on demand and whose close
// is observable, so tests inject open/sync/close failures deterministically.
type fakeSyncHandle struct {
	syncErr  error
	closeErr error
	closed   bool
}

func (f *fakeSyncHandle) Sync() error { return f.syncErr }

func (f *fakeSyncHandle) Close() error {
	f.closed = true
	return f.closeErr
}

func TestDirectorySync(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"real directory syncs durably", testRealDirectorySync},
		{"open failure reports the step", testOpenFailure},
		{"sync failure reports the step", testSyncFailure},
		{"unsupported sync is flagged", testUnsupportedSync},
		{"close failure reports the step", testCloseFailure},
		{"cancellation prevents sync", testSyncCancellation},
		{"file commit syncs then closes", testFileCommit},
		{"file commit closes after sync failure", testFileCommitSyncFailure},
		{"file commit reports close failure", testFileCommitCloseFailure},
		{"file commit honors cancellation", testFileCommitCancellation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// failingOpener returns a syncer whose opener yields the fixed handle and
// error for every path.
func failingOpener(handle SyncHandle, err error) *DirectorySyncer {
	return &DirectorySyncer{open: func(path string) (SyncHandle, error) {
		return handle, err
	}}
}

func testRealDirectorySync(t *testing.T) {
	syncer := NewDirectorySyncer()
	must(t, syncer.Sync(context.Background(), t.TempDir()))
}

func testOpenFailure(t *testing.T) {
	syncer := failingOpener(nil, fs.ErrPermission)
	err := syncer.Sync(context.Background(), "anything")
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("err type = %T, want *SyncError", err)
	}
	if syncErr.Op != "open" || syncErr.Unsupported {
		t.Fatalf("op = %q, unsupported = %v, want open failure", syncErr.Op, syncErr.Unsupported)
	}
}

func testSyncFailure(t *testing.T) {
	handle := &fakeSyncHandle{syncErr: syscall.EIO}
	err := failingOpener(handle, nil).Sync(context.Background(), "dir")
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("err type = %T, want *SyncError", err)
	}
	if syncErr.Op != "sync" || syncErr.Unsupported {
		t.Fatalf("op = %q, unsupported = %v, want sync failure", syncErr.Op, syncErr.Unsupported)
	}
	if !syncErr.Result.Opened || syncErr.Result.Synced || !syncErr.Result.Closed {
		t.Fatalf("sync result = %+v, want opened/closed without sync", syncErr.Result)
	}
	if !handle.closed {
		t.Fatal("failed sync must still close the handle")
	}
}

func testUnsupportedSync(t *testing.T) {
	handle := &fakeSyncHandle{syncErr: syscall.EINVAL}
	err := failingOpener(handle, nil).Sync(context.Background(), "dir")
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("err type = %T, want *SyncError", err)
	}
	if !syncErr.Unsupported || syncErr.Op != "sync" {
		t.Fatalf("op = %q, unsupported = %v, want flagged unsupported", syncErr.Op, syncErr.Unsupported)
	}
	if !handle.closed {
		t.Fatal("unsupported sync must still close the handle")
	}
}

func testCloseFailure(t *testing.T) {
	handle := &fakeSyncHandle{closeErr: syscall.EIO}
	err := failingOpener(handle, nil).Sync(context.Background(), "dir")
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("err type = %T, want *SyncError", err)
	}
	if syncErr.Op != "close" || syncErr.Unsupported {
		t.Fatalf("op = %q, unsupported = %v, want close failure", syncErr.Op, syncErr.Unsupported)
	}
	if !handle.closed {
		t.Fatal("close failure must still mark the handle closed")
	}
}

func testSyncCancellation(t *testing.T) {
	opened := false
	syncer := &DirectorySyncer{open: func(path string) (SyncHandle, error) {
		opened = true
		return &fakeSyncHandle{}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := syncer.Sync(ctx, "dir"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if opened {
		t.Fatal("canceled sync must not open the directory")
	}
}

func testFileCommit(t *testing.T) {
	handle := &fakeSyncHandle{}
	must(t, CommitFile(context.Background(), handle))
	if !handle.closed {
		t.Fatal("commit must close the handle")
	}
}

func testFileCommitSyncFailure(t *testing.T) {
	handle := &fakeSyncHandle{syncErr: syscall.EIO}
	if err := CommitFile(context.Background(), handle); err == nil {
		t.Fatal("sync failure must fail the commit")
	}
	if !handle.closed {
		t.Fatal("commit must close even after a sync failure")
	}
}

func testFileCommitCloseFailure(t *testing.T) {
	handle := &fakeSyncHandle{closeErr: syscall.EIO}
	if err := CommitFile(context.Background(), handle); err == nil {
		t.Fatal("close failure must fail the commit")
	}
}

func testFileCommitCancellation(t *testing.T) {
	handle := &fakeSyncHandle{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CommitFile(ctx, handle); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !handle.closed {
		t.Fatal("canceled commit must still close")
	}
}

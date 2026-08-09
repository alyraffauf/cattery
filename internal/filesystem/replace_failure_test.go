package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// failTemp fails one of the write-path steps while leaving the real entry
// on disk, so cleanup after the failure is observable.
type failTemp struct {
	path    string
	write   error
	chmod   error
	written bool
}

func (f *failTemp) Write(p []byte) (int, error) {
	f.written = true
	if f.write != nil {
		return 0, f.write
	}
	return len(p), nil
}

func (f *failTemp) Chmod(mode fs.FileMode) error { return f.chmod }

func (f *failTemp) Name() string { return f.path }

// realTempOpener opens a genuine temporary entry and keeps the raw handle
// for tests that must observe its lifecycle.
func realTempOpener(dir, pattern string) (TempFile, SyncHandle, *os.File, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, nil, nil, err
	}
	return file, file, file, nil
}

func testWriteFailurePreservesOldTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, nil, err
			}
			return &failTemp{path: file.Name(), write: syscall.EIO}, file, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	mustFail(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, target); content != "old" {
		t.Fatalf("target = %q, want old preserved", content)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testModeFailurePreservesOldTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, nil, err
			}
			return &failTemp{path: file.Name(), chmod: syscall.EIO}, file, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	mustFail(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, target); content != "old" {
		t.Fatalf("target = %q, want old preserved", content)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testSyncFailurePreservesOldTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	var opened []*os.File
	defer func() {
		for _, file := range opened {
			_ = file.Close()
		}
	}()
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, nil, err
			}
			opened = append(opened, file)
			return file, &fakeSyncHandle{syncErr: syscall.EIO}, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	mustFail(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, target); content != "old" {
		t.Fatalf("target = %q, want old preserved", content)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testRenameFailurePreservesOldTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			temp, handle, _, err := realTempOpener(dir, pattern)
			return temp, handle, err
		},
		rename: func(_, _ string) error { return syscall.EIO },
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	mustFail(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, target); content != "old" {
		t.Fatalf("target = %q, want old preserved", content)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testDirectoryBarrierPartial(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			temp, handle, _, err := realTempOpener(dir, pattern)
			return temp, handle, err
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: failingOpener(&fakeSyncHandle{syncErr: syscall.EIO}, nil),
	}
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	err := replacer.Replace(context.Background(), precondition, spec)
	var syncErr *SyncError
	if !errors.As(err, &syncErr) || !errors.Is(syncErr, syscall.EIO) {
		t.Fatalf("err = %v, want *SyncError wrapping EIO", err)
	}
	var partial *ReplaceError
	if !errors.As(err, &partial) || !partial.Result.Renamed || partial.Result.DirectorySynced {
		t.Fatalf("result = %+v, want renamed but unsynced", partial.Result)
	}
	if content := readFile(t, target); content != "new" {
		t.Fatalf("target = %q, want renamed content", content)
	}
}

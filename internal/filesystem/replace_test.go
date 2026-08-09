package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// countTemp counts Write calls while delegating to the real entry, so tests
// prove the validated bytes are written exactly once.
type countTemp struct {
	file   *os.File
	writes int
}

func (c *countTemp) Write(p []byte) (int, error) {
	c.writes++
	return c.file.Write(p)
}

func (c *countTemp) Chmod(mode fs.FileMode) error { return c.file.Chmod(mode) }

func (c *countTemp) Name() string { return c.file.Name() }

// cancelTemp cancels the context at the first write, simulating a
// cancellation landing mid-replacement.
type cancelTemp struct {
	file   *os.File
	cancel context.CancelFunc
}

func (c *cancelTemp) Write(p []byte) (int, error) {
	c.cancel()
	return 0, nil
}

func (c *cancelTemp) Chmod(mode fs.FileMode) error { return c.file.Chmod(mode) }

func (c *cancelTemp) Name() string { return c.file.Name() }

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAtomicReplace(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"publishes the exact bytes once", testPublishesExactBytesOnce},
		{"replaces an existing target atomically", testReplacesExistingTarget},
		{"replaces a final symlink without following it", testFinalSymlinkReplaced},
		{"tolerates parents created after freeze", testCreatedParentsTolerated},
		{"fails on a stale precondition without mutating", testStalePrecondition},
		{"cancellation removes the temporary entry", testCancellationCleanup},
		{"cancellation before replace mutates nothing", testCanceledBeforeReplace},
		{"preserves the old target on write failure", testWriteFailurePreservesOldTarget},
		{"preserves the old target on mode failure", testModeFailurePreservesOldTarget},
		{"preserves the old target on sync failure", testSyncFailurePreservesOldTarget},
		{"preserves the old target on rename failure", testRenameFailurePreservesOldTarget},
		{"reports a partial result on directory barrier failure", testDirectoryBarrierPartial},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testPublishesExactBytesOnce(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "app.conf")
	var counting *countTemp
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, nil, err
			}
			counting = &countTemp{file: file}
			return counting, file, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	spec := ReplacementSpec{Content: []byte("exact bytes\n"), Mode: 0o640}
	must(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, filepath.Join(root, "app.conf")); content != "exact bytes\n" {
		t.Fatalf("content = %q, want exact bytes", content)
	}
	info, err := os.Stat(filepath.Join(root, "app.conf"))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode())
	}
	if counting.writes != 1 {
		t.Fatalf("writes = %d, want exactly one", counting.writes)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testReplacesExistingTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	replacer := NewReplacer()
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	must(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, target); content != "new" {
		t.Fatalf("content = %q, want new", content)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testFinalSymlinkReplaced(t *testing.T) {
	root := t.TempDir()
	referent := filepath.Join(root, "referent")
	must(t, os.WriteFile(referent, []byte("referent"), 0o600))
	must(t, os.Symlink("referent", filepath.Join(root, "app.conf")))
	precondition := mustFreeze(t, root, "app.conf")
	replacer := NewReplacer()
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	must(t, replacer.Replace(context.Background(), precondition, spec))
	info, err := os.Lstat(filepath.Join(root, "app.conf"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("target kind = %v, want regular file", info.Mode())
	}
	if content := readFile(t, filepath.Join(root, "app.conf")); content != "new" {
		t.Fatalf("target content = %q, want new", content)
	}
	if content := readFile(t, referent); content != "referent" {
		t.Fatalf("referent = %q, want untouched", content)
	}
}

func testCreatedParentsTolerated(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "dir/sub/app.conf")
	must(t, os.MkdirAll(filepath.Join(root, "dir/sub"), 0o755))
	replacer := NewReplacer()
	spec := ReplacementSpec{Content: []byte("deep"), Mode: 0o644}
	must(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, filepath.Join(root, "dir/sub/app.conf")); content != "deep" {
		t.Fatalf("content = %q, want deep", content)
	}
}

func testStalePrecondition(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app.conf")
	must(t, os.WriteFile(target, []byte("old"), 0o600))
	precondition := mustFreeze(t, root, "app.conf")
	must(t, os.WriteFile(target, []byte("modified"), 0o600))
	replacer := NewReplacer()
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	mustFail(t, replacer.Replace(context.Background(), precondition, spec))
	if content := readFile(t, target); content != "modified" {
		t.Fatalf("target = %q, want untouched modified content", content)
	}
	if names := dirEntries(t, root); len(names) != 1 || names[0] != "app.conf" {
		t.Fatalf("dir entries = %v, want only app.conf", names)
	}
}

func testCancellationCleanup(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "app.conf")
	ctx, cancel := context.WithCancel(context.Background())
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, nil, err
			}
			return &cancelTemp{file: file, cancel: cancel}, file, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	err := replacer.Replace(ctx, precondition, spec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if names := dirEntries(t, root); len(names) != 0 {
		t.Fatalf("dir entries = %v, want temp removed", names)
	}
}

func testCanceledBeforeReplace(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "app.conf")
	opened := false
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			opened = true
			return nil, nil, nil
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spec := ReplacementSpec{Content: []byte("new"), Mode: 0o600}
	err := replacer.Replace(ctx, precondition, spec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if opened {
		t.Fatal("canceled replace must not create a temporary entry")
	}
	if names := dirEntries(t, root); len(names) != 0 {
		t.Fatalf("dir entries = %v, want none", names)
	}
}

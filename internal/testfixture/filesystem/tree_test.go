package filesystem

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemFixture(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"regular files with content mode and type", testFileCreation},
		{"directories are structural", testDirectoryCreation},
		{"symlinks including dangling targets", testSymlinkCreation},
		{"hard link shares inode", testHardLinkIdentity},
		{"mutation hooks fire", testMutationHooksFire},
		{"fixtures are isolated", testFixtureIsolation},
		{"cleanup removes created paths", testCleanupRemovesPaths},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testFileCreation(t *testing.T) {
	root := t.TempDir()
	payload := []byte{0x00, 0xFF, 0x0A, 0x42}
	builder := New(root).
		File("empty.txt", nil, 0o644).
		File("binary.bin", payload, 0o755).
		File("private.txt", []byte("secret"), 0o600)
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(root, "empty.txt"), nil)
	assertFileContent(t, filepath.Join(root, "binary.bin"), payload)
	assertFileMode(t, filepath.Join(root, "binary.bin"), 0o755)
	assertFileMode(t, filepath.Join(root, "private.txt"), 0o600)
	assertRegularFile(t, filepath.Join(root, "empty.txt"))
}

func testDirectoryCreation(t *testing.T) {
	root := t.TempDir()
	builder := New(root).
		Dir("config", 0o755).
		File("config/app.toml", []byte("v = 1"), 0o644)
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("recorded directory was not created")
	}
}

func testSymlinkCreation(t *testing.T) {
	root := t.TempDir()
	builder := New(root).
		File("target.toml", []byte("ok"), 0o600).
		Symlink("live.toml", "target.toml").
		Symlink("dangling.toml", "does/not/exist")
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	live, err := os.Readlink(filepath.Join(root, "live.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if live != "target.toml" {
		t.Fatalf("live link target = %s", live)
	}
	dangling, err := os.Readlink(filepath.Join(root, "dangling.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if dangling != "does/not/exist" {
		t.Fatalf("dangling link target = %s", dangling)
	}
}

func testHardLinkIdentity(t *testing.T) {
	root := t.TempDir()
	builder := New(root).
		File("original.txt", []byte("linked"), 0o644).
		HardLink("alias.txt", "original.txt")
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	original, err := os.Lstat(filepath.Join(root, "original.txt"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := os.Lstat(filepath.Join(root, "alias.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, alias) {
		t.Fatal("hard link does not share inode with original")
	}
}

func testMutationHooksFire(t *testing.T) {
	root := t.TempDir()
	var fired bool
	var received string
	builder := New(root).
		File("app.conf", []byte("base"), 0o600).
		MutationPoint("app.conf", func(materialized string) error {
			fired = true
			received = materialized
			return os.WriteFile(materialized, []byte("mutated"), 0o600)
		})
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	if !fired {
		t.Fatal("mutation hook did not fire")
	}
	if received != filepath.Join(root, "app.conf") {
		t.Fatalf("mutation received path = %s", received)
	}
	got, err := os.ReadFile(filepath.Join(root, "app.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mutated" {
		t.Fatalf("mutation effect = %q", got)
	}
}

func testFixtureIsolation(t *testing.T) {
	first := New(t.TempDir()).File("shared.txt", []byte("first"), 0o644)
	second := New(t.TempDir()).File("shared.txt", []byte("second"), 0o644)
	if err := first.Materialize(); err != nil {
		t.Fatal(err)
	}
	if err := second.Materialize(); err != nil {
		t.Fatal(err)
	}
	if first.Root == second.Root {
		t.Fatal("fixtures share a root directory")
	}
	firstBytes, err := os.ReadFile(filepath.Join(first.Root, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(second.Root, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != "first" || string(secondBytes) != "second" {
		t.Fatalf("isolation broken: %q vs %q", firstBytes, secondBytes)
	}
}

func testCleanupRemovesPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	builder := New(root).File("note.txt", []byte("gone"), 0o644)
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("created path missing before cleanup: %v", err)
	}
	if err := builder.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err == nil {
		t.Fatal("cleanup did not remove created path")
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content at %s = %q, want %q", path, got, want)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode at %s = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func assertRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", path)
	}
}

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
		{"hard link shares inode", testHardLinkIdentity},
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

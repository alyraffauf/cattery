package pathsafe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemIdentity(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"same path matches itself", testSamePathIdentity},
		{"hard links share identity", testHardLinksShareIdentity},
		{"distinct files differ", testDistinctFilesDiffer},
		{"missing path errors", testMissingIdentityErrors},
		{"identity reports mode and size", testIdentityAccessors},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSamePathIdentity(t *testing.T) {
	path := writeFile(t, "target")
	first, err := FilesystemIdentity(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := FilesystemIdentity(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !SameIdentity(first, second) {
		t.Fatal("the same path must share filesystem identity")
	}
}

func testHardLinksShareIdentity(t *testing.T) {
	original := writeFile(t, "original")
	linked := filepath.Join(filepath.Dir(original), "linked")
	if err := os.Link(original, linked); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}
	first, err := FilesystemIdentity(original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := FilesystemIdentity(linked)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !SameIdentity(first, second) {
		t.Fatal("hard links must share filesystem identity")
	}
}

func testDistinctFilesDiffer(t *testing.T) {
	a := writeFile(t, "first")
	b := writeFile(t, "second")
	first, err := FilesystemIdentity(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := FilesystemIdentity(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if SameIdentity(first, second) {
		t.Fatal("distinct files must not share identity")
	}
}

func testMissingIdentityErrors(t *testing.T) {
	if _, err := FilesystemIdentity(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing path must return an error")
	}
}

func testIdentityAccessors(t *testing.T) {
	path := writeFile(t, "target")
	identity, err := FilesystemIdentity(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.Path() != path {
		t.Fatalf("Path = %q, want %q", identity.Path(), path)
	}
	if identity.IsDir() {
		t.Fatal("captured regular file must not report IsDir")
	}
	if identity.Mode()&0o600 != 0o600 {
		t.Fatalf("Mode = %v, expected 0600 bits", identity.Mode())
	}
	if identity.Size() != 1 {
		t.Fatalf("Size = %d, want 1", identity.Size())
	}
	var zero Identity
	if zero.IsDir() || zero.Size() != 0 || zero.Mode() != 0 {
		t.Fatal("zero-value Identity must report empty defaults")
	}
	if SameIdentity(zero, identity) {
		t.Fatal("zero-value Identity must never match")
	}
}

func writeFile(t *testing.T, name string) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

package pathsafe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalRoot(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"resolves existing directory", testCanonicalExisting},
		{"appends missing suffix", testCanonicalMissingSuffix},
		{"climbs to deepest ancestor", testCanonicalDeepestAncestor},
		{"resolves symlinked ancestor", testCanonicalSymlinkAncestor},
		{"rejects empty input", testCanonicalEmpty},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testCanonicalExisting(t *testing.T) {
	root := expectCanonical(t.TempDir())
	got, err := CanonicalRoot(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Fatalf("CanonicalRoot = %q, want %q", got, root)
	}
}

func testCanonicalMissingSuffix(t *testing.T) {
	root := expectCanonical(t.TempDir())
	target := filepath.Join(root, "a", "b", "c")
	got, err := CanonicalRoot(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Fatalf("CanonicalRoot = %q, want %q", got, target)
	}
}

func testCanonicalDeepestAncestor(t *testing.T) {
	root := expectCanonical(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "config", "git", "ignore")
	got, err := CanonicalRoot(target)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != target {
		t.Fatalf("CanonicalRoot = %q, want %q", got, target)
	}
}

func testCanonicalSymlinkAncestor(t *testing.T) {
	realDir := expectCanonical(t.TempDir())
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := CanonicalRoot(filepath.Join(link, "child"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(realDir, "child")
	if got != want {
		t.Fatalf("CanonicalRoot = %q, want %q", got, want)
	}
}

func testCanonicalEmpty(t *testing.T) {
	if _, err := CanonicalRoot(""); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

// expectCanonical returns the fully resolved canonical form of path, since
// temporary directories may themselves live beneath a symlink.
func expectCanonical(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

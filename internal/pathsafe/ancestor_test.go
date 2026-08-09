package pathsafe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAncestorWalk(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"walks clean directory tree", testWalksCleanTree},
		{"walks single segment parent", testWalksSingleSegment},
		{"rejects symlink component", testWalkRejectsSymlink},
		{"rejects file component", testWalkRejectsFile},
		{"rejects missing parent", testWalkRejectsMissing},
		{"rejects dot-dot escape", testWalkRejectsDotDot},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testWalksCleanTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AncestorWalk(root, "config/git/ignore"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testWalksSingleSegment(t *testing.T) {
	root := t.TempDir()
	if err := AncestorWalk(root, "bashrc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testWalkRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := AncestorWalk(root, "linked/file"); err == nil {
		t.Fatal("symlink component must be rejected")
	}
}

func testWalkRejectsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AncestorWalk(root, "blocker/file"); err == nil {
		t.Fatal("file component must be rejected")
	}
}

func testWalkRejectsMissing(t *testing.T) {
	root := t.TempDir()
	if err := AncestorWalk(root, "missing/file"); err == nil {
		t.Fatal("missing parent component must be rejected")
	}
}

func testWalkRejectsDotDot(t *testing.T) {
	root := t.TempDir()
	if err := AncestorWalk(root, "../escape"); err == nil {
		t.Fatal("dot-dot escape must be rejected")
	}
}

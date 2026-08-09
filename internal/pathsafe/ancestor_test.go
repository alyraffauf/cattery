package pathsafe

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAncestorWalk(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"walks clean directory tree", testWalksCleanTree},
		{"walks single segment parent", testWalksSingleSegment},
		{"rejects internal symlink component", testWalkRejectsSymlink},
		{"rejects escaping symlink component", testWalkRejectsEscapingSymlink},
		{"rejects file component", testWalkRejectsFile},
		{"rejects special component", testWalkRejectsSpecial},
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

func testWalkRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := AncestorWalk(root, "escape/file"); err == nil {
		t.Fatal("symlink escaping the root must be rejected")
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

func testWalkRejectsSpecial(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("FIFO unsupported: %v", err)
	}
	if err := AncestorWalk(root, "pipe/file"); err == nil {
		t.Fatal("special parent component must be rejected")
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

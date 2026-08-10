package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func TestSnapshotPrecondition(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"blocking symlink ancestor", testPreconditionSymlinkAncestor},
		{"blocking non-directory ancestor", testPreconditionFileAncestor},
		{"missing parents tolerated", testPreconditionMissingParents},
		{"parent race", testPreconditionParentRace},
		{"final symlink never followed", testPreconditionFinalSymlink},
		{"absent to present", testPreconditionAbsentTransition},
		{"mode change", testPreconditionModeChange},
		{"object replacement", testPreconditionIdentityReplacement},
		{"replacement by symlink", testPreconditionSymlinkReplacement},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testPreconditionSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	mustTargetMkdir(t, real)
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatalf("make symlink: %v", err)
	}
	if _, err := CaptureTarget(Destination{Root: root, Relative: "link/file.txt"}); err == nil {
		t.Fatal("precondition must reject a symlinked parent component")
	}
}

func testPreconditionFileAncestor(t *testing.T) {
	root := t.TempDir()
	mustTargetFile(t, filepath.Join(root, "block"), []byte("x"))
	if _, err := CaptureTarget(Destination{Root: root, Relative: "block/file.txt"}); err == nil {
		t.Fatal("precondition must reject a non-directory parent component")
	}
}

func testPreconditionMissingParents(t *testing.T) {
	root := t.TempDir()
	snapshot := captureAt(t, root, "missing/deep/file.txt")
	if snapshot.Kind() != KindAbsent {
		t.Fatalf("kind = %v, want KindAbsent", snapshot.Kind())
	}
	if snapshot.Parent().Path() != "" {
		t.Fatal("missing parents must freeze as the zero parent identity")
	}
}

func testPreconditionParentRace(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "dir")
	mustTargetMkdir(t, parent)
	mustTargetFile(t, filepath.Join(parent, "file"), []byte("x"))
	first := captureAt(t, root, "dir/file")
	if err := os.Rename(parent, filepath.Join(root, "gone")); err != nil {
		t.Fatalf("move parent aside: %v", err)
	}
	mustTargetMkdir(t, parent)
	second := captureAt(t, root, "dir/file")
	if pathsafe.SameIdentity(first.Parent(), second.Parent()) ||
		first.Kind() != KindFile || second.Kind() != KindAbsent {
		t.Fatal("parent race must be visible through differing parent and target facts")
	}
}

func testPreconditionFinalSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("referent", filepath.Join(root, "link")); err != nil {
		t.Fatalf("make symlink: %v", err)
	}
	snapshot := captureAt(t, root, "link")
	if snapshot.Kind() != KindSymlink || snapshot.Payload() != "referent" {
		t.Fatalf("kind = %v payload %q, want KindSymlink with exact referent", snapshot.Kind(), snapshot.Payload())
	}
	if snapshot.Mode()&os.ModeSymlink != 0 {
		t.Fatal("permission mode must exclude type bits")
	}
}

func testPreconditionAbsentTransition(t *testing.T) {
	root := t.TempDir()
	first := captureAt(t, root, "file")
	mustTargetFile(t, filepath.Join(root, "file"), []byte("x"))
	second := captureAt(t, root, "file")
	if first.Kind() != KindAbsent || second.Kind() != KindFile {
		t.Fatal("absent-to-present transition must be visible between captures")
	}
}

func testPreconditionModeChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	mustTargetFile(t, path, []byte("x"))
	first := captureAt(t, root, "file")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod target: %v", err)
	}
	second := captureAt(t, root, "file")
	if first.Mode() != 0o644 || second.Mode() != 0o600 {
		t.Fatal("mode change must be visible while the frozen precondition stays put")
	}
}

func testPreconditionIdentityReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	mustTargetFile(t, path, []byte("x"))
	first := captureAt(t, root, "file")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove target: %v", err)
	}
	mustTargetFile(t, path, []byte("x"))
	second := captureAt(t, root, "file")
	if pathsafe.SameIdentity(first.Identity(), second.Identity()) {
		t.Fatal("an identical-content replacement must still expose a new object identity")
	}
}

func testPreconditionSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	mustTargetFile(t, path, []byte("x"))
	first := captureAt(t, root, "file")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove target: %v", err)
	}
	if err := os.Symlink("elsewhere", path); err != nil {
		t.Fatalf("replace target with symlink: %v", err)
	}
	second := captureAt(t, root, "file")
	if first.Kind() != KindFile || second.Kind() != KindSymlink || second.Payload() != "elsewhere" {
		t.Fatal("replacement by symlink must not be read as regular content")
	}
}

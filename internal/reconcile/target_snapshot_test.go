package reconcile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func TestTargetSnapshot(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"absent target", testTargetAbsent},
		{"regular file", testTargetRegularFile},
		{"regular file hashes", testTargetRegularHashes},
		{"executable mode", testTargetExecutableMode},
		{"directory target", testTargetDirectory},
		{"symlink target", testTargetSymlink},
		{"special target", testTargetSpecial},
		{"nested relative path", testTargetNested},
		{"hard links share identity", testTargetHardLinks},
		{"missing root", testTargetMissingRoot},
		{"non-directory root", testTargetFileRoot},
		{"symlink parent is rejected", testTargetSymlinkParent},
		{"non-directory parent is rejected", testTargetNonDirectoryParent},
		{"missing parent is absent", testTargetMissingParent},
		{"parent replacement is visible", testTargetParentReplacement},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func captureAt(t *testing.T, root, relative string) TargetSnapshot {
	t.Helper()
	snapshot, err := CaptureTarget(Destination{Root: root, Relative: relative})
	if err != nil {
		t.Fatalf("capture %s: %v", relative, err)
	}
	return snapshot
}

func mustTargetFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustTargetMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func testTargetAbsent(t *testing.T) {
	root := t.TempDir()
	snapshot := captureAt(t, root, "missing.txt")
	if snapshot.Kind() != KindAbsent || snapshot.Identity().Path() != "" {
		t.Fatalf("absent target facts = %v, want KindAbsent with zero identity", snapshot.Kind())
	}
	if snapshot.Digest() != (deployment.Digest{}) {
		t.Fatal("absent target must carry a zero digest")
	}
	if snapshot.Mode() != 0 || snapshot.Payload() != "" {
		t.Fatal("absent target must carry zero mode and payload")
	}
	parent, err := pathsafe.FilesystemIdentity(root)
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if !pathsafe.SameIdentity(snapshot.Parent(), parent) {
		t.Fatal("parent identity must match the real parent directory")
	}
}

func testTargetRegularFile(t *testing.T) {
	root := t.TempDir()
	mustTargetFile(t, filepath.Join(root, "file.txt"), []byte("managed content"))
	snapshot := captureAt(t, root, "file.txt")
	if snapshot.Kind() != KindFile || snapshot.Mode() != 0o644 {
		t.Fatalf("regular file facts = %v mode %o, want KindFile 0644", snapshot.Kind(), snapshot.Mode())
	}
	if snapshot.Payload() != "" {
		t.Fatal("regular file must carry no link payload")
	}
	identity, err := pathsafe.FilesystemIdentity(filepath.Join(root, "file.txt"))
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if !pathsafe.SameIdentity(snapshot.Identity(), identity) {
		t.Fatal("object identity must match the real entry")
	}
}

func testTargetRegularHashes(t *testing.T) {
	root := t.TempDir()
	content := []byte("managed content")
	mustTargetFile(t, filepath.Join(root, "file.txt"), content)
	snapshot := captureAt(t, root, "file.txt")
	if snapshot.Digest() != deployment.Ordinary(content) {
		t.Fatal("digest must match the exact bytes")
	}
}

func testTargetExecutableMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}
	snapshot := captureAt(t, root, "run.sh")
	if snapshot.Kind() != KindFile || snapshot.Mode() != 0o755 {
		t.Fatalf("executable facts = %v mode %o, want KindFile 0755", snapshot.Kind(), snapshot.Mode())
	}
	if snapshot.Mode()&0o111 == 0 {
		t.Fatal("executable bits must be preserved in the precondition mode")
	}
}

func testTargetDirectory(t *testing.T) {
	root := t.TempDir()
	mustTargetMkdir(t, filepath.Join(root, "sub"))
	snapshot := captureAt(t, root, "sub")
	if snapshot.Kind() != KindDirectory || snapshot.Mode() != 0o700 {
		t.Fatalf("directory facts = %v mode %o, want KindDirectory 0700", snapshot.Kind(), snapshot.Mode())
	}
	if snapshot.Payload() != "" {
		t.Fatal("directory must carry zero payload")
	}
}

func testTargetSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink("elsewhere", filepath.Join(root, "link")); err != nil {
		t.Fatalf("make symlink: %v", err)
	}
	snapshot := captureAt(t, root, "link")
	if snapshot.Kind() != KindSymlink || snapshot.Payload() != "elsewhere" {
		t.Fatalf("symlink facts = %v payload %q, want KindSymlink with exact referent", snapshot.Kind(), snapshot.Payload())
	}
	identity, err := pathsafe.FilesystemIdentity(filepath.Join(root, "link"))
	if err != nil {
		t.Fatalf("stat link: %v", err)
	}
	if !pathsafe.SameIdentity(snapshot.Identity(), identity) {
		t.Fatal("object identity must be the link itself, never the referent")
	}
}

func testTargetSpecial(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatalf("make fifo: %v", err)
	}
	snapshot := captureAt(t, root, "pipe")
	if snapshot.Kind() != KindSpecial {
		t.Fatalf("fifo kind = %v, want KindSpecial", snapshot.Kind())
	}
	if snapshot.Payload() != "" {
		t.Fatal("special target must carry zero payload")
	}
}

func testTargetNested(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o700); err != nil {
		t.Fatalf("make parents: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "file.txt"), []byte("deep"), 0o600); err != nil {
		t.Fatalf("write deep target: %v", err)
	}
	snapshot := captureAt(t, root, "a/b/file.txt")
	if snapshot.Kind() != KindFile {
		t.Fatal("nested target must freeze its file kind")
	}
}

func testTargetHardLinks(t *testing.T) {
	root := t.TempDir()
	mustTargetFile(t, filepath.Join(root, "first"), []byte("shared"))
	if err := os.Link(filepath.Join(root, "first"), filepath.Join(root, "second")); err != nil {
		t.Fatalf("make hard link: %v", err)
	}
	first := captureAt(t, root, "first")
	second := captureAt(t, root, "second")
	if !pathsafe.SameIdentity(first.Identity(), second.Identity()) {
		t.Fatal("hard links must share one object identity")
	}
}

func testTargetMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	if _, err := CaptureTarget(Destination{Root: root, Relative: "file.txt"}); err == nil {
		t.Fatal("capture must reject a missing root")
	}
}

func testTargetFileRoot(t *testing.T) {
	root := t.TempDir()
	block := filepath.Join(root, "block")
	mustTargetFile(t, block, []byte("x"))
	if _, err := CaptureTarget(Destination{Root: block, Relative: "file.txt"}); err == nil {
		t.Fatal("capture must reject a non-directory root")
	}
}

func testTargetSymlinkParent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	mustTargetMkdir(t, real)
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatalf("make symlink: %v", err)
	}
	if _, err := CaptureTarget(Destination{Root: root, Relative: "link/file.txt"}); err == nil {
		t.Fatal("capture must reject a symlinked parent component")
	}
}

func testTargetNonDirectoryParent(t *testing.T) {
	root := t.TempDir()
	mustTargetFile(t, filepath.Join(root, "block"), []byte("x"))
	if _, err := CaptureTarget(Destination{Root: root, Relative: "block/file.txt"}); err == nil {
		t.Fatal("capture must reject a non-directory parent component")
	}
}

func testTargetMissingParent(t *testing.T) {
	root := t.TempDir()
	snapshot := captureAt(t, root, "missing/deep/file.txt")
	if snapshot.Kind() != KindAbsent {
		t.Fatalf("kind = %v, want KindAbsent", snapshot.Kind())
	}
	if snapshot.Parent().Path() != "" {
		t.Fatal("missing parents must freeze as the zero parent identity")
	}
}

func testTargetParentReplacement(t *testing.T) {
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
		t.Fatal("parent replacement must expose differing parent and target facts")
	}
}

package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func testParentIdentityRevalidate(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "conf")
	must(t, os.Mkdir(parent, 0o755))
	precondition := mustFreeze(t, root, "conf/file.txt")
	if precondition.Parent().Path() != parent {
		t.Fatal("existing parent must be captured")
	}
	must(t, os.Rename(parent, filepath.Join(root, "moved")))
	must(t, os.Symlink(filepath.Join(root, "moved"), parent))
	mustFail(t, precondition.Revalidate())
}

func testStaleIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	must(t, os.WriteFile(path, []byte("value=1"), 0o600))
	facts := mustCapture(t, path)
	must(t, os.Remove(path))
	must(t, os.WriteFile(path, []byte("value=1"), 0o600))
	mustFail(t, facts.Revalidate())
}

func testStaleContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	must(t, os.WriteFile(path, []byte("value=1"), 0o600))
	facts := mustCapture(t, path)
	must(t, os.WriteFile(path, []byte("value=2"), 0o600))
	mustFail(t, facts.Revalidate())
}

func testStaleMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	must(t, os.WriteFile(path, []byte("value=1"), 0o600))
	facts := mustCapture(t, path)
	must(t, os.Chmod(path, 0o700))
	mustFail(t, facts.Revalidate())
}

func testStalePayload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alias")
	must(t, os.Symlink("first", path))
	facts := mustCapture(t, path)
	must(t, os.Remove(path))
	must(t, os.Symlink("second", path))
	mustFail(t, facts.Revalidate())
}

func testKindChange(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "late.txt")
	must(t, os.WriteFile(filepath.Join(root, "late.txt"), []byte("late"), 0o600))
	mustFail(t, precondition.Target().Revalidate())
}

func testBlockingParents(t *testing.T) {
	root := t.TempDir()
	must(t, testfs.New(root).
		Dir("conf", 0o755).
		File("conf/app.conf", []byte("ok"), 0o644).
		File("plain", []byte("file"), 0o644).
		Materialize())
	must(t, os.Symlink("conf", filepath.Join(root, "linked")))
	mustRejectFreeze(t, root, "linked/app.conf")
	mustRejectFreeze(t, root, "plain/x")
	mustFreeze(t, root, "conf/app.conf")
}

func testFreezeNoMutation(t *testing.T) {
	root := t.TempDir()
	mustFreeze(t, root, "created/by/freeze.txt")
	if _, err := os.Lstat(filepath.Join(root, "created")); err == nil {
		t.Fatal("freeze must not create parent directories")
	}
	if _, err := os.Lstat(filepath.Join(root, "created", "by", "freeze.txt")); err == nil {
		t.Fatal("freeze must not create the target")
	}
}

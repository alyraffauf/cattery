package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/pathsafe"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func TestFilesystemPrecondition(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"absent target captures as absent", testAbsentTargetFreeze},
		{"regular file facts freeze", testRegularFileFacts},
		{"symlink payload freezes", testSymlinkPayload},
		{"hard links share identity", testHardLinkTargets},
		{"content token equality", testContentToken},
		{"absent to present transition", testAbsentPresentTransition},
		{"parent identity revalidates", testParentIdentityRevalidate},
		{"stale identity detected", testStaleIdentity},
		{"stale content detected", testStaleContent},
		{"stale mode detected", testStaleMode},
		{"stale payload detected", testStalePayload},
		{"kind change detected", testKindChange},
		{"blocking parents are rejected", testBlockingParents},
		{"freeze never mutates", testFreezeNoMutation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testAbsentTargetFreeze(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "missing.txt")
	facts := precondition.Target()
	if facts.Kind() != KindAbsent {
		t.Fatalf("kind = %v, want absent", facts.Kind())
	}
	if facts.Identity().Path() != "" || facts.Mode() != 0 || facts.Token() != (ContentToken{}) {
		t.Fatal("absent target must carry zero identity, mode, and token")
	}
	if precondition.Destination() != (Destination{Root: root, Relative: "missing.txt"}) {
		t.Fatal("precondition must echo its destination")
	}
	var zero TargetFacts
	if zero.Kind() != KindAbsent || zero.Identity().Path() != "" {
		t.Fatal("zero-value TargetFacts must report absent facts")
	}
}

func testRegularFileFacts(t *testing.T) {
	root := t.TempDir()
	must(t, testfs.New(root).File("app.conf", []byte("value=1"), 0o755).Materialize())
	precondition := mustFreeze(t, root, "app.conf")
	facts := precondition.Target()
	if facts.Kind() != KindFile {
		t.Fatalf("kind = %v, want regular file", facts.Kind())
	}
	if facts.Mode() != 0o755 {
		t.Fatalf("mode = %o, want 755", facts.Mode())
	}
	if facts.Token() != TokenOfContent([]byte("value=1")) {
		t.Fatal("content token must match the exact bytes")
	}
	if !facts.Identity().Mode().IsRegular() {
		t.Fatal("identity must describe a regular file")
	}
	if precondition.Parent().Path() != root {
		t.Fatalf("parent = %s, want root", precondition.Parent().Path())
	}
	if KindOfIdentity(facts.Identity()) != KindFile {
		t.Fatal("KindOfIdentity must classify a regular file")
	}
}

func testSymlinkPayload(t *testing.T) {
	root := t.TempDir()
	must(t, testfs.New(root).
		File("target.toml", []byte("ok"), 0o600).
		Symlink("alias.toml", "target.toml").
		Materialize())
	facts := mustFreeze(t, root, "alias.toml").Target()
	if facts.Kind() != KindSymlink {
		t.Fatalf("kind = %v, want symlink", facts.Kind())
	}
	if facts.Payload() != "target.toml" {
		t.Fatalf("payload = %q, want %q", facts.Payload(), "target.toml")
	}
	if KindOfIdentity(facts.Identity()) != KindSymlink {
		t.Fatal("KindOfIdentity must classify a symlink")
	}
}

func testHardLinkTargets(t *testing.T) {
	root := t.TempDir()
	must(t, testfs.New(root).
		File("original.txt", []byte("shared"), 0o644).
		HardLink("alias.txt", "original.txt").
		Materialize())
	first := mustCapture(t, filepath.Join(root, "original.txt"))
	second := mustCapture(t, filepath.Join(root, "alias.txt"))
	if !pathsafe.SameIdentity(first.Identity(), second.Identity()) {
		t.Fatal("hard-linked targets must share frozen identity")
	}
}

func testContentToken(t *testing.T) {
	token := TokenOfContent([]byte("payload"))
	if token != TokenOfContent([]byte("payload")) {
		t.Fatal("equal bytes must produce equal tokens")
	}
	if TokenOfContent(nil) != TokenOfContent([]byte{}) {
		t.Fatal("nil and empty bytes must share one token")
	}
	if token == TokenOfContent([]byte("other")) {
		t.Fatal("distinct bytes must produce distinct tokens")
	}
}

func testAbsentPresentTransition(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "new/dir/file.txt")
	if precondition.Parent().Path() != "" {
		t.Fatal("missing parent must freeze as absent")
	}
	mustFail(t, precondition.Revalidate())
	must(t, os.MkdirAll(filepath.Join(root, "new", "dir"), 0o755))
	must(t, precondition.Revalidate())
	must(t, testfs.New(root).File("new/dir/file.txt", []byte("x"), 0o600).Materialize())
	mustFail(t, precondition.Revalidate())
}

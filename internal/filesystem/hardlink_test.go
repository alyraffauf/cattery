package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func TestHardLinkMode(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"multiply linked target is replaced, not chmod'd", testLinkedTargetReplaced},
		{"replacement leaves the other link untouched", testOtherLinkUntouched},
		{"replacement preserves the target bytes", testLinkedReplacementBytes},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// linkSpec names two hard links to one inode inside a test root.
type linkSpec struct {
	root, first, second string
}

// linkPair creates two hard links to one inode.
func linkPair(t *testing.T, spec linkSpec) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(spec.root, spec.first), []byte("shared\n"), 0o644))
	must(t, os.Link(filepath.Join(spec.root, spec.first), filepath.Join(spec.root, spec.second)))
}

func testLinkedTargetReplaced(t *testing.T) {
	root := t.TempDir()
	linkPair(t, linkSpec{root: root, first: "app", second: "alias.conf"})
	precondition := mustFreeze(t, root, "app")
	replacer := NewReplacer()
	must(t, replacer.ApplyMode(context.Background(), precondition, 0o755))
	app, err := os.Stat(filepath.Join(root, "app"))
	if err != nil {
		t.Fatalf("stat app: %v", err)
	}
	alias, err := os.Stat(filepath.Join(root, "alias.conf"))
	if err != nil {
		t.Fatalf("stat alias: %v", err)
	}
	if os.SameFile(app, alias) {
		t.Fatal("linked target must be replaced by a fresh inode")
	}
	if app.Mode().Perm() != 0o755 {
		t.Fatalf("app mode = %04o, want 0755", app.Mode().Perm())
	}
	if alias.Mode().Perm() != 0o644 {
		t.Fatalf("alias mode = %04o, want 0644 untouched", alias.Mode().Perm())
	}
}

func testOtherLinkUntouched(t *testing.T) {
	root := t.TempDir()
	linkPair(t, linkSpec{root: root, first: "app", second: "alias.conf"})
	appBefore, err := pathsafe.FilesystemIdentity(filepath.Join(root, "app"))
	if err != nil {
		t.Fatalf("identity app: %v", err)
	}
	precondition := mustFreeze(t, root, "app")
	replacer := NewReplacer()
	must(t, replacer.ApplyMode(context.Background(), precondition, 0o700))
	alias := mustCapture(t, filepath.Join(root, "alias.conf"))
	if alias.Mode() != 0o644 {
		t.Fatalf("alias mode = %04o, want 0644", alias.Mode())
	}
	appAfter := mustCapture(t, filepath.Join(root, "app"))
	if pathsafe.SameIdentity(appBefore, appAfter.Identity()) {
		t.Fatal("multiply linked target must be replaced, not chmod'd")
	}
}

func testLinkedReplacementBytes(t *testing.T) {
	root := t.TempDir()
	linkPair(t, linkSpec{root: root, first: "app", second: "alias.conf"})
	precondition := mustFreeze(t, root, "app")
	replacer := NewReplacer()
	must(t, replacer.ApplyMode(context.Background(), precondition, 0o711))
	if content := readFile(t, filepath.Join(root, "app")); content != "shared\n" {
		t.Fatalf("app content = %q, want preserved bytes", content)
	}
	if content := readFile(t, filepath.Join(root, "alias.conf")); content != "shared\n" {
		t.Fatalf("alias content = %q, want preserved bytes", content)
	}
}

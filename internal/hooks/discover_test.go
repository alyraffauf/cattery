package hooks

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestHookDiscovery(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"valid hooks discovered", testDiscoveryValid},
		{"missing hooks tree is empty", testDiscoveryMissing},
		{"empty hooks tree is empty", testDiscoveryEmpty},
		{"nonexecutable rejected", testDiscoveryNonexecutable},
		{"nested directory rejected", testDiscoveryNested},
		{"symlink rejected", testDiscoverySymlink},
		{"special file rejected", testDiscoverySpecial},
		{"group hooks scoped", testDiscoveryGroup},
		{"phase not a directory rejected", testDiscoveryPhaseFile},
		{"hooks root not a directory rejected", testDiscoveryHooksFile},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testDiscoveryValid(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "_hooks", "before", "a.sh"), 0o755)
	writeMode(t, filepath.Join(root, "_hooks", "before", "b.sh"), 0o700)
	writeMode(t, filepath.Join(root, "_hooks", "after", "z.sh"), 0o755)
	got, err := Discover(root, deployment.NewScope(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHooks(t, got, []deployment.Hook{
		{
			Scope: deployment.NewScope(""), Phase: deployment.HookAfter, Name: "z.sh",
			AbsolutePath:   filepath.Join(root, "_hooks", "after", "z.sh"),
			RepositoryPath: "_hooks/after/z.sh",
		},
		{
			Scope: deployment.NewScope(""), Phase: deployment.HookBefore, Name: "a.sh",
			AbsolutePath:   filepath.Join(root, "_hooks", "before", "a.sh"),
			RepositoryPath: "_hooks/before/a.sh",
		},
		{
			Scope: deployment.NewScope(""), Phase: deployment.HookBefore, Name: "b.sh",
			AbsolutePath:   filepath.Join(root, "_hooks", "before", "b.sh"),
			RepositoryPath: "_hooks/before/b.sh",
		},
	})
}

func testDiscoveryMissing(t *testing.T) {
	root := t.TempDir()
	got, err := Discover(root, deployment.NewScope(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("hooks = %d, want none", len(got))
	}
}

func testDiscoveryEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(root, deployment.NewScope(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("hooks = %d, want none", len(got))
	}
}

func testDiscoveryNonexecutable(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "_hooks", "before", "stale.sh"), 0o644)
	if _, err := Discover(root, deployment.NewScope("")); err == nil {
		t.Fatal("nonexecutable hook was accepted")
	}
}

func testDiscoveryNested(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "_hooks", "before", "sub", "x.sh"), 0o755)
	if _, err := Discover(root, deployment.NewScope("")); err == nil {
		t.Fatal("nested hook directory was accepted")
	}
}

func testDiscoverySymlink(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "target.sh"), 0o755)
	if err := os.MkdirAll(filepath.Join(root, "_hooks", "before"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../target.sh", filepath.Join(root, "_hooks", "before", "link.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root, deployment.NewScope("")); err == nil {
		t.Fatal("symlinked hook was accepted")
	}
}

func testDiscoverySpecial(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_hooks", "before"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "_hooks", "before", "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root, deployment.NewScope("")); err == nil {
		t.Fatal("special hook entry was accepted")
	}
}

func testDiscoveryGroup(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "_hooks", "before", "root.sh"), 0o755)
	writeMode(t, filepath.Join(root, "atuin", "_hooks", "after", "group.sh"), 0o755)
	got, err := Discover(root, deployment.NewScope("atuin"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHooks(t, got, []deployment.Hook{
		{
			Scope: deployment.NewScope("atuin"), Phase: deployment.HookAfter, Name: "group.sh",
			AbsolutePath:   filepath.Join(root, "atuin", "_hooks", "after", "group.sh"),
			RepositoryPath: "atuin/_hooks/after/group.sh",
		},
	})
}

func testDiscoveryPhaseFile(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "_hooks", "before"), 0o644)
	if _, err := Discover(root, deployment.NewScope("")); err == nil {
		t.Fatal("file phase path was accepted as a hook directory")
	}
}

func testDiscoveryHooksFile(t *testing.T) {
	root := t.TempDir()
	writeMode(t, filepath.Join(root, "_hooks"), 0o644)
	if _, err := Discover(root, deployment.NewScope("")); err == nil {
		t.Fatal("file hooks root was accepted")
	}
}

func assertHooks(t *testing.T, got []deployment.Hook, want []deployment.Hook) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("hooks = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("hook %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func writeMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

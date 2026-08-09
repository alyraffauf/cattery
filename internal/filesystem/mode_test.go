package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func TestTargetMode(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"new ordinary target defaults to 0644 plus source executable bits", testNewOrdinaryMode},
		{"existing ordinary target preserves its read/write bits", testExistingOrdinaryMode},
		{"executable-only correction replaces only the executable bits", testExecutableOnlyCorrection},
		{"secret target is exactly 0600 or 0700", testSecretMode},
		{"mode-only correction chmods a singly linked target in place", testModeOnlyChmod},
		{"restrictive umask cannot clamp the derived mode", testRestrictiveUmask},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testNewOrdinaryMode(t *testing.T) {
	if mode := OrdinaryTargetMode(0, 0o111, true); mode != 0o755 {
		t.Fatalf("exec 0111 new = %04o, want 0755", mode)
	}
	if mode := OrdinaryTargetMode(0, 0, true); mode != 0o644 {
		t.Fatalf("exec 000 new = %04o, want 0644", mode)
	}
}

func testExistingOrdinaryMode(t *testing.T) {
	if mode := OrdinaryTargetMode(0o600, 0o111, false); mode != 0o711 {
		t.Fatalf("existing 0600 exec 0111 = %04o, want 0711", mode)
	}
	if mode := OrdinaryTargetMode(0o640, 0o001, false); mode != 0o641 {
		t.Fatalf("existing 0640 exec 001 = %04o, want 0641", mode)
	}
	if mode := OrdinaryTargetMode(0o755, 0, false); mode != 0o644 {
		t.Fatalf("existing 0755 exec 000 = %04o, want 0644", mode)
	}
}

func testExecutableOnlyCorrection(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app")
	must(t, os.WriteFile(target, []byte("run\n"), 0o755))
	precondition := mustFreeze(t, root, "app")
	replacer := NewReplacer()
	must(t, replacer.ApplyMode(context.Background(), precondition, 0o644))
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode())
	}
	if content := readFile(t, target); content != "run\n" {
		t.Fatalf("content = %q, want untouched", content)
	}
}

func testSecretMode(t *testing.T) {
	if mode := SecretTargetMode(0); mode != 0o600 {
		t.Fatalf("non-executable secret = %04o, want 0600", mode)
	}
	if mode := SecretTargetMode(0o111); mode != 0o700 {
		t.Fatalf("executable secret = %04o, want 0700", mode)
	}
	if mode := SecretTargetMode(0o100); mode != 0o700 {
		t.Fatalf("owner-executable secret = %04o, want 0700", mode)
	}
}

func testModeOnlyChmod(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app")
	must(t, os.WriteFile(target, []byte("run\n"), 0o644))
	precondition := mustFreeze(t, root, "app")
	before := mustCapture(t, target)
	replacer := NewReplacer()
	must(t, replacer.ApplyMode(context.Background(), precondition, 0o755))
	after := mustCapture(t, target)
	if !pathsafe.SameIdentity(before.Identity(), after.Identity()) {
		t.Fatal("mode-only correction must preserve the target identity")
	}
	if after.Mode() != 0o755 {
		t.Fatalf("mode = %04o, want 0755", after.Mode())
	}
}

func testRestrictiveUmask(t *testing.T) {
	root := t.TempDir()
	previous := syscall.Umask(0o077)
	defer syscall.Umask(previous)
	precondition := mustFreeze(t, root, "app")
	replacer := NewReplacer()
	spec := ReplacementSpec{Content: []byte("run\n"), Mode: OrdinaryTargetMode(0, 0o111, true)}
	must(t, replacer.Replace(context.Background(), precondition, spec))
	info, err := os.Stat(filepath.Join(root, "app"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755 despite restrictive umask", info.Mode())
	}
}

package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAliasRealization(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"missing alias is created", testMissingAliasCreated},
		{"exact link is a no-op", testExactLinkNoOp},
		{"absolute link is drift", testAbsoluteLinkDrift},
		{"wrong or dangling link is drift", testWrongLinkDrift},
		{"dangling exact link is a no-op", testDanglingExactNoOp},
		{"occupied file without confirmation is drift", testOccupiedFileDrift},
		{"confirmed replacement replaces the entry atomically", testConfirmedReplacement},
		{"replacement never follows the old referent", testOldReferentUntouched},
		{"directory requires manual intervention", testDirectoryManualIntervention},
		{"special entry requires manual intervention", testSpecialManualIntervention},
		{"parent race fails before any mutation", testParentRace},
		{"cancellation creates nothing", testAliasCancellation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func assertLink(t *testing.T, path, payload string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", path)
	}
	if actual, err := os.Readlink(path); err != nil || actual != payload {
		t.Fatalf("payload = %q, want %q", actual, payload)
	}
}

func assertDrift(t *testing.T, err error, path string) {
	t.Helper()
	var drift *AliasDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("err = %T, want *AliasDriftError", err)
	}
	if drift.Path != path {
		t.Fatalf("drift path = %q, want %q", drift.Path, path)
	}
}

func testMissingAliasCreated(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	realization, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	if err != nil || realization != AliasCreated {
		t.Fatalf("realization = %v, err = %v, want created", realization, err)
	}
	assertLink(t, filepath.Join(root, ".config/x"), "dotfiles/x")
	if names := dirEntries(t, root); len(names) != 1 {
		t.Fatalf("dir entries = %v, want only .config", names)
	}
}

func testExactLinkNoOp(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	must(t, os.Symlink("dotfiles/x", filepath.Join(root, ".config/x")))
	precondition := mustFreeze(t, root, ".config/x")
	before := mustCapture(t, filepath.Join(root, ".config/x"))
	replacer := NewReplacer()
	realization, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	if err != nil || realization != AliasExact {
		t.Fatalf("realization = %v, err = %v, want exact", realization, err)
	}
	after := mustCapture(t, filepath.Join(root, ".config/x"))
	if before.Identity().Path() != after.Identity().Path() || before.Identity().Mode() != after.Identity().Mode() {
		t.Fatal("exact link must not be touched")
	}
}

func testAbsoluteLinkDrift(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	path := filepath.Join(root, ".config/x")
	must(t, os.Symlink(filepath.Join(root, "dotfiles/x"), path))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	assertDrift(t, err, path)
	assertLink(t, path, filepath.Join(root, "dotfiles/x"))
}

func testWrongLinkDrift(t *testing.T) {
	for _, payload := range []string{"elsewhere", "gone"} {
		root := t.TempDir()
		must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
		path := filepath.Join(root, ".config/x")
		must(t, os.Symlink(payload, path))
		precondition := mustFreeze(t, root, ".config/x")
		replacer := NewReplacer()
		_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
		assertDrift(t, err, path)
		assertLink(t, path, payload)
	}
}

func testDanglingExactNoOp(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	path := filepath.Join(root, ".config/x")
	must(t, os.Symlink("dotfiles/x", path))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	realization, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	if err != nil || realization != AliasExact {
		t.Fatalf("realization = %v, err = %v, want exact", realization, err)
	}
	assertLink(t, path, "dotfiles/x")
}

func testOccupiedFileDrift(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	path := filepath.Join(root, ".config/x")
	must(t, os.WriteFile(path, []byte("occupied"), 0o644))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	assertDrift(t, err, path)
	if content := readFile(t, path); content != "occupied" {
		t.Fatalf("occupied file content = %q, want untouched", content)
	}
}

func testConfirmedReplacement(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	path := filepath.Join(root, ".config/x")
	must(t, os.WriteFile(path, []byte("occupied"), 0o644))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	realization, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x", Overwrite: true})
	if err != nil || realization != AliasReplaced {
		t.Fatalf("realization = %v, err = %v, want replaced", realization, err)
	}
	assertLink(t, path, "dotfiles/x")
	if names := dirEntries(t, filepath.Join(root, ".config")); len(names) != 1 {
		t.Fatalf("dir entries = %v, want only x", names)
	}
}

func testOldReferentUntouched(t *testing.T) {
	root := t.TempDir()
	referent := filepath.Join(root, "other")
	must(t, os.WriteFile(referent, []byte("referent"), 0o600))
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	path := filepath.Join(root, ".config/x")
	must(t, os.Symlink("../other", path))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x", Overwrite: true})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	assertLink(t, path, "dotfiles/x")
	if content := readFile(t, referent); content != "referent" {
		t.Fatalf("referent = %q, want untouched", content)
	}
}

func testDirectoryManualIntervention(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config/x"), 0o755))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	if err == nil {
		t.Fatal("directory alias path must fail")
	}
	if names := dirEntries(t, filepath.Join(root, ".config")); len(names) != 1 {
		t.Fatalf("dir entries = %v, want untouched", names)
	}
}

func testSpecialManualIntervention(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, ".config"), 0o755))
	path := filepath.Join(root, ".config/x")
	must(t, syscall.Mkfifo(path, 0o600))
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	if err == nil {
		t.Fatal("special alias path must fail")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("special entry was mutated: %v", info.Mode())
	}
}

func testParentRace(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, ".config")
	must(t, os.MkdirAll(parent, 0o755))
	precondition := mustFreeze(t, root, ".config/x")
	must(t, os.Rename(parent, filepath.Join(root, ".moved")))
	must(t, os.MkdirAll(parent, 0o755))
	replacer := NewReplacer()
	_, err := replacer.RealizeAlias(context.Background(), precondition, AliasSpec{Payload: "dotfiles/x"})
	if err == nil {
		t.Fatal("parent race must fail")
	}
	if names := dirEntries(t, parent); len(names) != 0 {
		t.Fatalf("dir entries = %v, want nothing created", names)
	}
}

func testAliasCancellation(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, ".config/x")
	replacer := NewReplacer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := replacer.RealizeAlias(ctx, precondition, AliasSpec{Payload: "dotfiles/x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if names := dirEntries(t, root); len(names) != 0 {
		t.Fatalf("dir entries = %v, want none", names)
	}
}

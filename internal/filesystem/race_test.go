package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidAliasPayload(t *testing.T) {
	root := t.TempDir()
	must(t, os.Mkdir(filepath.Join(root, ".config"), 0o755))
	precondition := mustFreeze(t, root, ".config/x")
	for _, payload := range []string{"", ".", "../x", "a/../x", "/absolute"} {
		if _, err := NewReplacer().RealizeAlias(context.Background(), precondition, AliasSpec{Payload: payload}); err == nil {
			t.Fatalf("payload %q must be rejected", payload)
		}
	}
}

func testDestinationRaceBeforeRename(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "app.conf")
	created := false
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			file, handle, _, err := realTempOpener(dir, pattern)
			if err == nil {
				must(t, os.WriteFile(filepath.Join(root, "app.conf"), []byte("raced"), 0o600))
				created = true
			}
			return file, handle, err
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	mustFail(t, replacer.Replace(context.Background(), precondition, ReplacementSpec{Content: []byte("new"), Mode: 0o600}))
	if !created || readFile(t, filepath.Join(root, "app.conf")) != "raced" {
		t.Fatal("destination race was not preserved")
	}
}

func testMissingParentSymlinkRace(t *testing.T) {
	root := t.TempDir()
	precondition := mustFreeze(t, root, "nested/app.conf")
	moved := filepath.Join(root, "moved")
	must(t, os.Mkdir(moved, 0o755))
	replacer := &Replacer{
		create: func(dir, pattern string) (TempFile, SyncHandle, error) {
			parent := filepath.Join(root, "nested")
			must(t, os.Remove(parent))
			must(t, os.Symlink(moved, parent))
			temp, handle, _, err := realTempOpener(dir, pattern)
			return temp, handle, err
		},
		rename: os.Rename,
		remove: os.Remove,
		syncer: NewDirectorySyncer(),
	}
	mustFail(t, replacer.Replace(context.Background(), precondition, ReplacementSpec{Content: []byte("new"), Mode: 0o600}))
	if _, err := os.Lstat(filepath.Join(moved, "app.conf")); err == nil {
		t.Fatal("replacement escaped through raced symlink parent")
	}
}

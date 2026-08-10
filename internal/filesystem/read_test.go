package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFrozenReadsExactRegularFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source")
	must(t, os.WriteFile(path, []byte("ciphertext"), 0o640))
	frozen := mustFreeze(t, root, "source")
	content, err := ReadFrozen(frozen)
	if err != nil || string(content) != "ciphertext" {
		t.Fatalf("content = %q, error = %v", content, err)
	}
}

func TestReadFrozenRejectsReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source")
	must(t, os.WriteFile(path, []byte("first"), 0o600))
	frozen := mustFreeze(t, root, "source")
	replacement := filepath.Join(root, "replacement")
	must(t, os.WriteFile(replacement, []byte("second"), 0o600))
	must(t, os.Rename(replacement, path))
	if _, err := ReadFrozen(frozen); err == nil {
		t.Fatal("replacement was accepted")
	}
}

package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
)

func TestValidateFrozenTargetRejectsReplacedFile(t *testing.T) {
	home := t.TempDir()
	relative := "config/app.conf"
	path := targetPath(home, relative)
	writeTarget(t, path, []byte("frozen"))

	precondition, err := filesystem.Freeze(filesystem.Destination{Root: home, Relative: relative})
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	writeTarget(t, replacement, []byte("substituted"))
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace target: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open replacement: %v", err)
	}
	defer file.Close()
	if err := validateFrozenTarget(file, precondition); err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("validate opened replacement: %v, want operational target-changed error", err)
	}
}

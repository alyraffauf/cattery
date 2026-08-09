package filesystem

import (
	"os"
	"path/filepath"
	"testing"

	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func testSourceFacts(t *testing.T) {
	root := t.TempDir()
	must(t, testfs.New(root).File("src.sh", []byte("#!/bin/sh\necho hi\n"), 0o755).Materialize())
	path := filepath.Join(root, "src.sh")
	facts := mustSource(t, path)
	if facts.Executable() != 0o111 {
		t.Fatalf("executable = %o, want 111", facts.Executable())
	}
	if facts.Token() != TokenOfContent([]byte("#!/bin/sh\necho hi\n")) {
		t.Fatal("source token must match exact content")
	}
	must(t, facts.Revalidate())
	must(t, os.Chmod(path, 0o600))
	mustFail(t, facts.Revalidate())
	must(t, os.WriteFile(path, []byte("#!/bin/sh\necho bye\n"), 0o600))
	mustFail(t, facts.Revalidate())
}

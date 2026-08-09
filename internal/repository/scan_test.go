package repository

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestRepositoryScan(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"root files", testScanRootFiles},
		{"root dot trees", testScanDotTrees},
		{"groups", testScanGroups},
		{"empty files", testScanEmptyFiles},
		{"controls are excluded", testScanControlsExcluded},
		{"raw hook candidates", testScanHookCandidates},
		{"symlinks rejected", testScanSymlinkRejected},
		{"specials rejected", testScanSpecialRejected},
		{"group collisions", testScanGroupCollisions},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testScanRootFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".bashrc"), 0o644)
	writeFile(t, filepath.Join(root, "Brewfile"), 0o755)
	result, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("groups = %v, want none", result.Groups)
	}
	assertCandidate(t, result.Files[0], newCandidate(root, wantFile{repoPath: ".bashrc"}))
	assertCandidate(t, result.Files[1], newCandidate(root, wantFile{repoPath: "Brewfile", exec: 0o111}))
	if len(result.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(result.Files))
	}
}

func testScanDotTrees(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".config", "starship.toml"), 0o644)
	writeFile(t, filepath.Join(root, ".config", "app", "_internal", "value"), 0o600)
	result, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("dot tree mistaken for a group: %v", result.Groups)
	}
	assertCandidate(t, result.Files[0], newCandidate(root, wantFile{repoPath: ".config/app/_internal/value"}))
	assertCandidate(t, result.Files[1], newCandidate(root, wantFile{repoPath: ".config/starship.toml"}))
}

func testScanGroups(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "atuin", ".config", "atuin", "config.toml"), 0o644)
	writeFile(t, filepath.Join(root, "atuin", "Brewfile"), 0o644)
	writeFile(t, filepath.Join(root, "ghostty", "config"), 0o755)
	result, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 2 || result.Groups[0] != "atuin" || result.Groups[1] != "ghostty" {
		t.Fatalf("groups = %v, want [atuin ghostty]", result.Groups)
	}
	assertCandidate(t, result.Files[0], newCandidate(root, wantFile{scope: deployment.NewScope("atuin"), repoPath: "atuin/.config/atuin/config.toml"}))
	assertCandidate(t, result.Files[1], newCandidate(root, wantFile{scope: deployment.NewScope("atuin"), repoPath: "atuin/Brewfile"}))
	assertCandidate(t, result.Files[2], newCandidate(root, wantFile{scope: deployment.NewScope("ghostty"), repoPath: "ghostty/config", exec: 0o111}))
}

func testScanEmptyFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "empty.txt"), 0o644)
	writeFile(t, filepath.Join(root, "atuin", "also-empty"), 0o600)
	result, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	assertCandidate(t, result.Files[0], newCandidate(root, wantFile{scope: deployment.NewScope("atuin"), repoPath: "atuin/also-empty"}))
	assertCandidate(t, result.Files[1], newCandidate(root, wantFile{repoPath: "empty.txt"}))
}

func testScanControlsExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "_darwin", "x"), 0o644)
	writeFile(t, filepath.Join(root, "_linux", "y"), 0o644)
	writeFile(t, filepath.Join(root, "_secrets", "token"), 0o600)
	writeFile(t, filepath.Join(root, "_routes.toml"), 0o644)
	writeFile(t, filepath.Join(root, "_notes", "junk.txt"), 0o644)
	writeFile(t, filepath.Join(root, ".git", "HEAD"), 0o644)
	writeFile(t, filepath.Join(root, ".gitignore"), 0o644)
	writeFile(t, filepath.Join(root, ".sops.yaml"), 0o644)
	result, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("files = %d, want only the secret candidate", len(result.Files))
	}
	assertCandidate(t, result.Files[0], newCandidate(root, wantFile{repoPath: "_secrets/token", secret: true}))
}

func testScanHookCandidates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "_hooks", "before", "install.sh"), 0o755)
	writeFile(t, filepath.Join(root, "atuin", "_hooks", "after", "finish.sh"), 0o755)
	result, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hooks) != 2 {
		t.Fatalf("hooks = %d, want 2", len(result.Hooks))
	}
	assertHook(t, result.Hooks[0], HookCandidate{Scope: deployment.NewScope(""), Phase: deployment.HookBefore, Name: "install.sh", RepositoryPath: "_hooks/before/install.sh", AbsolutePath: filepath.Join(root, "_hooks", "before", "install.sh")})
	assertHook(t, result.Hooks[1], HookCandidate{Scope: deployment.NewScope("atuin"), Phase: deployment.HookAfter, Name: "finish.sh", RepositoryPath: "atuin/_hooks/after/finish.sh", AbsolutePath: filepath.Join(root, "atuin", "_hooks", "after", "finish.sh")})
}

func testScanSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "target"), 0o644)
	if err := os.MkdirAll(filepath.Join(root, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, ".config", "live")); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(root); err == nil {
		t.Fatal("symlink inside a target tree was accepted")
	}
	symlinkRoot := t.TempDir()
	writeFile(t, filepath.Join(symlinkRoot, "target"), 0o644)
	if err := os.Symlink("target", filepath.Join(symlinkRoot, "live")); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(symlinkRoot); err == nil {
		t.Fatal("root-level symlink was accepted")
	}
}

func testScanSpecialRejected(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(root); err == nil {
		t.Fatal("special file was accepted")
	}
}

func testScanGroupCollisions(t *testing.T) {
	scenarios := []struct {
		name   string
		first  string
		second string
	}{
		{"case-fold equivalent", "atuin", "Atuin"},
		{"NFC and NFD equivalent", "café", "cafe\u0301"},
	}
	for _, scenario := range scenarios {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, scenario.first, "x"), 0o644)
		writeFile(t, filepath.Join(root, scenario.second, "y"), 0o644)
		if _, err := Scan(root); err == nil {
			t.Fatalf("%s: colliding groups were accepted", scenario.name)
		}
	}
	invalidRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(invalidRoot, "\xff"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Scan(invalidRoot); err == nil {
		t.Fatal("invalid-utf-8 group name was accepted")
	}
}

type wantFile struct {
	scope    deployment.Scope
	repoPath string
	exec     fs.FileMode
	secret   bool
}

func newCandidate(root string, want wantFile) Candidate {
	kind := deployment.FileOrdinary
	if want.secret {
		kind = deployment.FileSecret
	}
	return Candidate{
		Scope: want.scope, Layer: deployment.LayerBase, Kind: kind,
		SourceRepoPath: want.repoPath, SourceAbsPath: filepath.Join(root, want.repoPath),
		ExecutableBits: want.exec,
	}
}

func assertCandidate(t *testing.T, got Candidate, want Candidate) {
	t.Helper()
	if got != want {
		t.Fatalf("candidate = %+v, want %+v", got, want)
	}
}

func assertHook(t *testing.T, got HookCandidate, want HookCandidate) {
	t.Helper()
	if got != want {
		t.Fatalf("hook candidate = %+v, want %+v", got, want)
	}
}

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, mode); err != nil {
		t.Fatal(err)
	}
}

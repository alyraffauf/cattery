package repository

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestRepositoryOverlay(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"base only", testOverlayBaseOnly},
		{"platform replaces file", testOverlayFileReplaced},
		{"platform adds paths", testOverlayAdditions},
		{"platform dir replaces file", testOverlayDirReplacesFile},
		{"empty platform dir replaces file", testOverlayEmptyDirReplacesFile},
		{"platform file replaces dir", testOverlayFileReplacesDir},
		{"recursive merge", testOverlayRecursiveMerge},
		{"inactive layers ignored", testOverlayInactiveLayers},
		{"executable bits carried", testOverlayExecutableBits},
		{"malformed layers rejected", testOverlayMalformed},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testOverlayBaseOnly(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin, ".config/a", "Brewfile")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: ".config/a", target: ".config/a"},
		{repoPath: "Brewfile", target: "Brewfile"},
	}})
}

func testOverlayFileReplaced(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin,
		".config/ghostty/config", "_darwin/.config/ghostty/config")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_darwin/.config/ghostty/config", target: ".config/ghostty/config", layer: deployment.LayerDarwin},
	}})
}

func testOverlayAdditions(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin,
		".config/base", "_darwin/.config/extra", "_darwin/Library/app/init")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: ".config/base", target: ".config/base"},
		{repoPath: "_darwin/.config/extra", target: ".config/extra", layer: deployment.LayerDarwin},
		{repoPath: "_darwin/Library/app/init", target: "Library/app/init", layer: deployment.LayerDarwin},
	}})
}

func testOverlayDirReplacesFile(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin, "readme", "_darwin/readme/notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_darwin/readme/notes.txt", target: "readme/notes.txt", layer: deployment.LayerDarwin},
	}})
}

func testOverlayEmptyDirReplacesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_darwin", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	records, err := resolvePaths(root, deployment.LayerDarwin, "app")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: nil})
}

func testOverlayFileReplacesDir(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin, "bin/tool", "bin/helper", "_darwin/bin")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_darwin/bin", target: "bin", layer: deployment.LayerDarwin},
	}})
}

func testOverlayRecursiveMerge(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin,
		".config/a", ".config/shared", "_darwin/.config/b", "_darwin/.config/shared")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: ".config/a", target: ".config/a"},
		{repoPath: "_darwin/.config/b", target: ".config/b", layer: deployment.LayerDarwin},
		{repoPath: "_darwin/.config/shared", target: ".config/shared", layer: deployment.LayerDarwin},
	}})
}

func testOverlayInactiveLayers(t *testing.T) {
	root := t.TempDir()
	paths := []string{".config/a", "_linux/.config/b", "_darwin/.config/c"}
	darwinRecords, err := resolvePaths(root, deployment.LayerDarwin, paths...)
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, darwinRecords, wantRecords{root: root, records: []wantRecord{
		{repoPath: ".config/a", target: ".config/a"},
		{repoPath: "_darwin/.config/c", target: ".config/c", layer: deployment.LayerDarwin},
	}})
	linuxRecords, err := resolvePaths(root, deployment.LayerLinux, paths...)
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, linuxRecords, wantRecords{root: root, records: []wantRecord{
		{repoPath: ".config/a", target: ".config/a"},
		{repoPath: "_linux/.config/b", target: ".config/b", layer: deployment.LayerLinux},
	}})
}

func testOverlayExecutableBits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "_darwin", "bin", "tool"), 0o755)
	records, err := resolvePaths(root, deployment.LayerDarwin, "plain")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_darwin/bin/tool", target: "bin/tool", layer: deployment.LayerDarwin, exec: 0o111},
		{repoPath: "plain", target: "plain"},
	}})
}

func testOverlayMalformed(t *testing.T) {
	fileLayer := t.TempDir()
	writeFile(t, filepath.Join(fileLayer, "_darwin"), 0o644)
	if _, err := resolvePaths(fileLayer, deployment.LayerDarwin); err == nil {
		t.Fatal("file _darwin layer was accepted")
	}
	symlinkLayer := t.TempDir()
	writeFile(t, filepath.Join(symlinkLayer, "_darwin", "target"), 0o644)
	if err := os.MkdirAll(filepath.Join(symlinkLayer, "_darwin", ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(symlinkLayer, "_darwin", ".config", "live")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePaths(symlinkLayer, deployment.LayerDarwin); err == nil {
		t.Fatal("symlink inside a platform layer was accepted")
	}
}

func resolvePaths(root string, platform deployment.Layer, paths ...string) ([]deployment.ManagedFile, error) {
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(root, path), nil, 0o644); err != nil {
			return nil, err
		}
	}
	result, err := Scan(root)
	if err != nil {
		return nil, err
	}
	return ResolvePlatform(root, result, platform)
}

type wantRecord struct {
	scope    deployment.Scope
	layer    deployment.Layer
	kind     deployment.FileKind
	repoPath string
	target   string
	exec     fs.FileMode
}

func newRecord(root string, want wantRecord) deployment.ManagedFile {
	layer := want.layer
	if layer == "" {
		layer = deployment.LayerBase
	}
	kind := want.kind
	if kind == "" {
		kind = deployment.FileOrdinary
	}
	return deployment.ManagedFile{
		Scope: want.scope, Layer: layer, Kind: kind,
		SourceAbsolutePath:   filepath.Join(root, want.repoPath),
		SourceRepositoryPath: want.repoPath,
		TargetRelativePath:   want.target,
		SourceExecutableBits: want.exec,
	}
}

type wantRecords struct {
	root    string
	records []wantRecord
}

func assertRecords(t *testing.T, got []deployment.ManagedFile, want wantRecords) {
	t.Helper()
	if len(got) != len(want.records) {
		t.Fatalf("records = %d, want %d", len(got), len(want.records))
	}
	for index, wantRecord := range want.records {
		expected := newRecord(want.root, wantRecord)
		if got[index] != expected {
			t.Fatalf("record %d = %+v, want %+v", index, got[index], expected)
		}
	}
}

package repository

import (
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestSecretOverlay(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"base secret", testSecretBase},
		{"group secret unrestricted", testSecretGroup},
		{"platform secret replaces ordinary", testSecretPlatformReplacesOrdinary},
		{"platform ordinary replaces secret", testSecretOrdinaryReplacesSecret},
		{"same-layer kind collision", testSecretSameLayerCollision},
		{"platform same-layer kind collision", testSecretPlatformCollision},
		{"root secret representability", testSecretRootRepresentability},
		{"platform secret representability", testSecretPlatformRepresentability},
		{"inactive layer secrets", testSecretInactiveLayers},
		{"secret executable bits", testSecretExecutableBits},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSecretBase(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin, "_secrets/.config/app/token")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_secrets/.config/app/token", target: ".config/app/token", kind: deployment.FileSecret},
	}})
}

func testSecretGroup(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin,
		"atuin/_secrets/bin/token", "atuin/_secrets/x")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{scope: deployment.NewScope("atuin"), repoPath: "atuin/_secrets/bin/token", target: "bin/token", kind: deployment.FileSecret},
		{scope: deployment.NewScope("atuin"), repoPath: "atuin/_secrets/x", target: "x", kind: deployment.FileSecret},
	}})
}

func testSecretPlatformReplacesOrdinary(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin,
		".config/app/credentials", "_darwin/_secrets/.config/app/credentials")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_darwin/_secrets/.config/app/credentials", target: ".config/app/credentials", layer: deployment.LayerDarwin, kind: deployment.FileSecret},
	}})
}

func testSecretOrdinaryReplacesSecret(t *testing.T) {
	root := t.TempDir()
	records, err := resolvePaths(root, deployment.LayerDarwin, "_secrets/token", "_darwin/token")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_darwin/token", target: "token", layer: deployment.LayerDarwin},
	}})
}

func testSecretSameLayerCollision(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePaths(root, deployment.LayerDarwin, ".config/x", "_secrets/.config/x"); err == nil {
		t.Fatal("same-layer ordinary/secret collision was accepted")
	}
}

func testSecretPlatformCollision(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePaths(root, deployment.LayerDarwin, "_darwin/.config/x", "_darwin/_secrets/.config/x"); err == nil {
		t.Fatal("same-layer platform ordinary/secret collision was accepted")
	}
}

func testSecretRootRepresentability(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePaths(root, deployment.LayerDarwin, "_secrets/bin/token"); err == nil {
		t.Fatal("unrepresentable root secret target was accepted")
	}
	underscoreRoot := t.TempDir()
	if _, err := resolvePaths(underscoreRoot, deployment.LayerDarwin, "_secrets/_token"); err == nil {
		t.Fatal("underscore root secret target was accepted")
	}
	validRoot := t.TempDir()
	records, err := resolvePaths(validRoot, deployment.LayerDarwin, "_secrets/token")
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: validRoot, records: []wantRecord{
		{repoPath: "_secrets/token", target: "token", kind: deployment.FileSecret},
	}})
}

func testSecretPlatformRepresentability(t *testing.T) {
	root := t.TempDir()
	if _, err := resolvePaths(root, deployment.LayerDarwin, "_darwin/_secrets/bin/token"); err == nil {
		t.Fatal("unrepresentable platform secret target was accepted")
	}
}

func testSecretInactiveLayers(t *testing.T) {
	root := t.TempDir()
	paths := []string{"_secrets/base", "_linux/_secrets/x"}
	darwinRecords, err := resolvePaths(root, deployment.LayerDarwin, paths...)
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, darwinRecords, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_secrets/base", target: "base", kind: deployment.FileSecret},
	}})
	linuxRecords, err := resolvePaths(root, deployment.LayerLinux, paths...)
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, linuxRecords, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_secrets/base", target: "base", kind: deployment.FileSecret},
		{repoPath: "_linux/_secrets/x", target: "x", layer: deployment.LayerLinux, kind: deployment.FileSecret},
	}})
}

func testSecretExecutableBits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "_secrets", ".config", "app", "tool"), 0o755)
	records, err := resolvePaths(root, deployment.LayerDarwin)
	if err != nil {
		t.Fatal(err)
	}
	assertRecords(t, records, wantRecords{root: root, records: []wantRecord{
		{repoPath: "_secrets/.config/app/tool", target: ".config/app/tool", kind: deployment.FileSecret, exec: 0o111},
	}})
}

package state

import (
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestStateContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"enum validity", testEnumValidity},
		{"digest width", testDigestWidth},
		{"defensive copies", testDefensiveCopy},
		{"slash-relative path forms", testPathForms},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testEnumValidity(t *testing.T) {
	if !StatusActive.Valid() || !StatusRetired.Valid() {
		t.Fatal("known source statuses must be valid")
	}
	if _, err := ParseSourceStatus("active"); err != nil {
		t.Fatalf("ParseSourceStatus active: %v", err)
	}
	if _, err := ParseSourceStatus("garbage"); err == nil {
		t.Fatal("ParseSourceStatus accepted an unknown value")
	}
	if !LayerAll.Valid() || !LayerDarwin.Valid() || !LayerLinux.Valid() {
		t.Fatal("known alias layers must be valid")
	}
	if _, err := ParseAliasLayer("all"); err != nil {
		t.Fatalf("ParseAliasLayer all: %v", err)
	}
	if _, err := ParseAliasLayer("base"); err == nil {
		t.Fatal("ParseAliasLayer accepted a file-only layer")
	}
}

func testDigestWidth(t *testing.T) {
	var hash deployment.Digest
	if len(hash) != 32 {
		t.Fatalf("deployment.Digest width = %d, want 32", len(hash))
	}
	if len(deployment.Digest{}) != 32 {
		t.Fatal("zero Digest must still be 32 bytes wide")
	}
}

func testDefensiveCopy(t *testing.T) {
	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	files := []FileBaseline{{TargetPath: "git/config", RetiredAt: &when}}
	copiedFiles := CopyFileBaselines(files)
	copiedFiles[0].TargetPath = "moved"
	*copiedFiles[0].RetiredAt = time.Time{}
	if files[0].TargetPath != "git/config" || !files[0].RetiredAt.Equal(when) {
		t.Fatal("source file baseline mutated through its defensive copy")
	}
	aliases := []AliasBaseline{{AliasPath: "bin/x", RetiredAt: &when}}
	copiedAliases := CopyAliasBaselines(aliases)
	copiedAliases[0].AliasPath = "moved"
	*copiedAliases[0].RetiredAt = time.Time{}
	if aliases[0].AliasPath != "bin/x" || !aliases[0].RetiredAt.Equal(when) {
		t.Fatal("source alias baseline mutated through its defensive copy")
	}
	repositories := []Repository{{RootPath: "/repo"}}
	copiedRepos := CopyRepositories(repositories)
	copiedRepos[0].RootPath = "/other"
	if repositories[0].RootPath != "/repo" {
		t.Fatal("source repository mutated through its defensive copy")
	}
}

func testPathForms(t *testing.T) {
	good := []string{"git/config", "shell/bashrc", "group/file"}
	for _, path := range good {
		if !IsSlashRelative(path) {
			t.Fatalf("IsSlashRelative(%q) = false, want true", path)
		}
	}
	bad := []string{"", "/absolute", "back\\slash"}
	for _, path := range bad {
		if IsSlashRelative(path) {
			t.Fatalf("IsSlashRelative(%q) = true, want false", path)
		}
	}
}

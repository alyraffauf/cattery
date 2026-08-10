package repository

import "testing"

func TestRepositoryControls(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"known controls", testKnownControls},
		{"ignored underscore entries", testIgnoredUnderscore},
		{"repository metadata", testMetadata},
		{"ordinary names", testOrdinaryNames},
		{"platform layer classification", testPlatformLayer},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testKnownControls(t *testing.T) {
	scenarios := []struct {
		name string
		want Control
	}{
		{"_darwin", ControlDarwin},
		{"_linux", ControlLinux},
		{"_secrets", ControlSecrets},
		{"_hooks", ControlHooks},
		{"_routes.toml", ControlRoutes},
	}
	for _, scenario := range scenarios {
		if got := ClassifyRoot(scenario.name); got != scenario.want {
			t.Fatalf("ClassifyRoot(%q) = %d, want %d", scenario.name, got, scenario.want)
		}
	}
}

func testIgnoredUnderscore(t *testing.T) {
	for _, name := range []string{"_notes", "_README.md", "_experiments"} {
		if got := ClassifyRoot(name); got != ControlIgnoredUnderscore {
			t.Fatalf("ClassifyRoot(%q) = %d, want ignored-underscore", name, got)
		}
	}
}

func testMetadata(t *testing.T) {
	names := []string{
		".git", ".github", ".gitignore",
		".gitattributes", ".gitmodules", ".sops.yaml",
	}
	for _, name := range names {
		if got := ClassifyRoot(name); got != ControlMetadata {
			t.Fatalf("ClassifyRoot(%q) = %d, want metadata", name, got)
		}
	}
}

func testOrdinaryNames(t *testing.T) {
	for _, name := range []string{"Brewfile", ".config", "atuin", "README.md"} {
		if got := ClassifyRoot(name); got != ControlNone {
			t.Fatalf("ClassifyRoot(%q) = %d, want none", name, got)
		}
	}
}

func testPlatformLayer(t *testing.T) {
	if got := ClassifyPlatformLayer("_secrets"); got != ControlSecrets {
		t.Fatalf("ClassifyPlatformLayer(_secrets) = %d, want secrets", got)
	}
	for _, name := range []string{"_darwin", "_hooks", "_routes.toml", "_notes"} {
		if got := ClassifyPlatformLayer(name); got != ControlIgnoredUnderscore {
			t.Fatalf("ClassifyPlatformLayer(%q) = %d, want ignored-underscore", name, got)
		}
	}
	for _, name := range []string{".config", "Brewfile"} {
		if got := ClassifyPlatformLayer(name); got != ControlNone {
			t.Fatalf("ClassifyPlatformLayer(%q) = %d, want none", name, got)
		}
	}
}

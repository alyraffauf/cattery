package routes

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestRouteActivation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"union activates", testActivationUnion},
		{"platform section activates", testActivationPlatformSection},
		{"inactive section skipped", testActivationInactiveSection},
		{"absent canonical fails", testActivationAbsentCanonical},
		{"wrong-layer canonical fails", testActivationWrongLayer},
		{"directory canonical fails", testActivationDirectoryCanonical},
		{"self alias fails", testActivationSelfAlias},
		{"duplicate union destination fails", testActivationDuplicateDestination},
		{"unknown platform fails", testActivationUnknownPlatform},
		{"sorted by destination", testActivationSorting},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testActivationUnion(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/app/config", []string{".example/app/config"}, SectionAll),
		declaration(".config/app/other", []string{".local/share/app/other"}, SectionLinux),
	}}
	aliases, err := Activate(config, deployment.LayerLinux,
		[]string{".config/app/config", ".config/app/other"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, aliases, []deployment.Alias{
		activated("linux", ".config/app/config", ".example/app/config"),
		activated("linux", ".config/app/other", ".local/share/app/other"),
	})
}

func testActivationPlatformSection(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/darwin-only", []string{"Library/App Support/darwin-only"}, SectionDarwin),
	}}
	aliases, err := Activate(config, deployment.LayerDarwin, []string{".config/darwin-only"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, aliases, []deployment.Alias{
		activated("darwin", ".config/darwin-only", "Library/App Support/darwin-only"),
	})
}

func testActivationInactiveSection(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/darwin-only", []string{"Library/App Support/darwin-only"}, SectionDarwin),
	}}
	aliases, err := Activate(config, deployment.LayerLinux, []string{".config/darwin-only"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("aliases = %d, want none for an inactive section", len(aliases))
	}
}

func testActivationAbsentCanonical(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/missing", []string{".example/missing"}, SectionAll),
	}}
	if _, err := Activate(config, deployment.LayerLinux, []string{".config/other"}); err == nil {
		t.Fatal("absent canonical was accepted")
	}
}

func testActivationWrongLayer(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/darwin-only", []string{".example/darwin-only"}, SectionAll),
	}}
	resolved := []string{".bashrc"}
	if _, err := Activate(config, deployment.LayerLinux, resolved); err == nil {
		t.Fatal("canonical present only in the inactive platform layer was accepted")
	}
}

func testActivationDirectoryCanonical(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration("config", []string{"config/alias"}, SectionAll),
	}}
	if _, err := Activate(config, deployment.LayerLinux, []string{"config/x"}); err == nil {
		t.Fatal("directory canonical was accepted as a file source")
	}
}

func testActivationSelfAlias(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/x", []string{".config/x"}, SectionAll),
	}}
	if _, err := Activate(config, deployment.LayerLinux, []string{".config/x"}); err == nil {
		t.Fatal("alias equal to its canonical target was accepted")
	}
}

func testActivationDuplicateDestination(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/first", []string{"shared/alias"}, SectionAll),
		declaration(".config/second", []string{"shared/alias"}, SectionLinux),
	}}
	if _, err := Activate(config, deployment.LayerLinux,
		[]string{".config/first", ".config/second"}); err == nil {
		t.Fatal("duplicate destination in the active union was accepted")
	}
}

func testActivationUnknownPlatform(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/x", []string{".example/x"}, SectionAll),
	}}
	if _, err := Activate(config, deployment.Layer("windows"), []string{".config/x"}); err == nil {
		t.Fatal("unknown platform was accepted")
	}
}

func testActivationSorting(t *testing.T) {
	config := Config{Version: 1, Declarations: []Declaration{
		declaration(".config/ghostty/config", []string{".config/ghostty/alias"}, SectionAll),
		declaration(".config/app/config", []string{".example/app/config"}, SectionAll),
	}}
	aliases, err := Activate(config, deployment.LayerLinux,
		[]string{".config/app/config", ".config/ghostty/config"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, aliases, []deployment.Alias{
		activated("linux", ".config/ghostty/config", ".config/ghostty/alias"),
		activated("linux", ".config/app/config", ".example/app/config"),
	})
}

func declaration(canonical string, aliases []string, section Section) Declaration {
	return Declaration{Canonical: canonical, Aliases: aliases, Section: section}
}

func activated(platform, canonical, destination string) deployment.Alias {
	return deployment.Alias{
		Platform: platform, CanonicalTargetRelativePath: canonical, AliasRelativePath: destination,
	}
}

func assertActivated(t *testing.T, got []deployment.Alias, want []deployment.Alias) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("aliases = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("alias %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}

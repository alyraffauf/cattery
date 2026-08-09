package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// thirdPartyOwner maps a third-party import path prefix to the single internal
// directory permitted to import it, per Section 12.5.
func thirdPartyOwner(importPath string) string {
	switch {
	case strings.HasPrefix(importPath, "github.com/spf13/cobra"),
		strings.HasPrefix(importPath, "github.com/spf13/pflag"),
		strings.HasPrefix(importPath, "golang.org/x/term"):
		return "internal/cli"
	case strings.HasPrefix(importPath, "modernc.org/sqlite"),
		strings.HasPrefix(importPath, "github.com/adrg/xdg"),
		strings.HasPrefix(importPath, "github.com/gofrs/flock"):
		return "internal/state"
	case strings.HasPrefix(importPath, "github.com/pelletier/go-toml"):
		return "internal/routes"
	case strings.HasPrefix(importPath, "github.com/zeebo/blake3"):
		return "internal/deployment"
	case strings.HasPrefix(importPath, "golang.org/x/text"):
		return "internal/pathsafe"
	case strings.HasPrefix(importPath, "github.com/pmezard/go-difflib"):
		return "internal/diff"
	}
	return ""
}

// thirdPartyViolations reports any third-party import outside its owner.
func thirdPartyViolations(root string) []violation {
	var breaches []violation
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return skipDirectory(path)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		breaches = append(breaches, fileThirdPartyViolations(root, path)...)
		return nil
	})
	if err != nil {
		return nil
	}
	return breaches
}

func fileThirdPartyViolations(root, path string) []violation {
	_, file, err := parseSource(path)
	if err != nil {
		return nil
	}
	relative := fileRelativePath(root, path)
	directory := filepath.Dir(relative)
	var breaches []violation
	for _, importSpec := range file.Imports {
		importPath := strings.Trim(importSpec.Path.Value, "\"")
		owner := thirdPartyOwner(importPath)
		if owner == "" {
			continue
		}
		if directory != owner && !strings.HasPrefix(directory, owner) {
			breaches = append(breaches, violation{file: path, rule: importPath + " outside " + owner})
		}
	}
	return breaches
}

// workflowPinViolations requires every workflow `uses:` entry to pin an exact
// 40-character commit SHA rather than a floating tag.
func workflowPinViolations(root string) []violation {
	workflows := filepath.Join(root, ".github", "workflows")
	var breaches []violation
	_ = filepath.WalkDir(workflows, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		breaches = append(breaches, workflowFileViolations(path)...)
		return nil
	})
	return breaches
}

func workflowFileViolations(path string) []violation {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var breaches []violation
	for _, line := range strings.Split(string(bytes), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- uses:") && !strings.HasPrefix(trimmed, "uses:") {
			continue
		}
		declaration := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- uses:"), "uses:"))
		if !isShaPinned(declaration) {
			breaches = append(breaches, violation{file: path, rule: "floating workflow action " + declaration})
		}
	}
	return breaches
}

func isShaPinned(declaration string) bool {
	at := strings.LastIndex(declaration, "@")
	if at < 0 {
		return false
	}
	commit := declaration[at+1:]
	if len(commit) != 40 {
		return false
	}
	for _, character := range commit {
		if !isHex(byte(character)) {
			return false
		}
	}
	return true
}

func isHex(character byte) bool {
	return strings.IndexByte("0123456789abcdef", character) >= 0
}

func TestThirdPartyChecker(t *testing.T) {
	t.Run("third-party ownership", func(t *testing.T) {
		scenarios := []struct {
			name   string
			source string
		}{
			{"cobra outside cli", "package state\nimport _ \"github.com/spf13/cobra\"\n"},
			{"sqlite outside state", "package cli\nimport _ \"modernc.org/sqlite\"\n"},
			{"blake3 outside deployment", "package state\nimport _ \"github.com/zeebo/blake3\"\n"},
		}
		for _, scenario := range scenarios {
			assertThirdPartyFails(t, scenario.name, scenario.source)
		}
	})

	t.Run("workflow pins", func(t *testing.T) {
		scenarios := []struct {
			name  string
			uses  string
			valid bool
		}{
			{"floating tag", "actions/checkout@v4", false},
			{"short sha", "actions/checkout@abc123", false},
			{"full sha", "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1", true},
		}
		for _, scenario := range scenarios {
			if isShaPinned(scenario.uses) != scenario.valid {
				t.Fatalf("%s: pin check mismatch for %s", scenario.name, scenario.uses)
			}
		}
	})

	t.Run("live tree is clean", func(t *testing.T) {
		root := repositoryRoot(t)
		failOn(t, "live third-party violations", thirdPartyViolations(root))
		failOn(t, "live workflow pin violations", workflowPinViolations(root))
	})
}

func assertThirdPartyFails(t *testing.T, name, source string) {
	t.Helper()
	root := t.TempDir()
	writeModule(t, root, "example.com/test")
	directory := filepath.Join(root, "internal", "wrong")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "f.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	breaches := thirdPartyViolations(root)
	if len(breaches) == 0 {
		t.Fatalf("%s: expected a third-party ownership violation", name)
	}
}

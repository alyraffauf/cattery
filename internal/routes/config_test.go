package routes

import "testing"

func TestRouteDecode(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"valid form decodes", testValidDecode},
		{"three sections accepted", testThreeSections},
		{"unknown top-level key fails", testUnknownTopLevel},
		{"unknown section fails", testUnknownSection},
		{"wrong version fails", testWrongVersion},
		{"missing version fails", testMissingVersion},
		{"cross-section canonical decodes", testCrossSectionCanonical},
		{"duplicate destination fails", testDuplicateDestination},
		{"absolute path fails", testAbsolutePath},
		{"dot-dot path fails", testDotDotPath},
		{"empty path fails", testEmptyPath},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testValidDecode(t *testing.T) {
	source := `version = 1

[symlinks.all]
".config/example/config" = [".example/config"]
`
	config, err := Decode([]byte(source))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Version != 1 {
		t.Fatalf("version = %d, want 1", config.Version)
	}
	if len(config.Declarations) != 1 {
		t.Fatalf("declarations = %d, want 1", len(config.Declarations))
	}
	declaration := config.Declarations[0]
	if declaration.Section != SectionAll {
		t.Fatalf("section = %q, want %q", declaration.Section, SectionAll)
	}
	if declaration.Canonical != ".config/example/config" {
		t.Fatalf("canonical = %q", declaration.Canonical)
	}
	if len(declaration.Aliases) != 1 || declaration.Aliases[0] != ".example/config" {
		t.Fatalf("aliases = %v", declaration.Aliases)
	}
}

func testThreeSections(t *testing.T) {
	source := `version = 1

[symlinks.all]
"all/target" = ["all/alias"]

[symlinks.darwin]
"darwin/target" = ["darwin/alias"]

[symlinks.linux]
"linux/target" = ["linux/alias"]
`
	config, err := Decode([]byte(source))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(config.Declarations) != 3 {
		t.Fatalf("declarations = %d, want 3", len(config.Declarations))
	}
}

func testUnknownTopLevel(t *testing.T) {
	source := `version = 1
bogus = true

[symlinks.all]
"x/y" = ["z"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for unknown top-level key, got nil")
	}
}

func testUnknownSection(t *testing.T) {
	source := `version = 1

[symlinks.windows]
"x/y" = ["z"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for unknown section, got nil")
	}
}

func testWrongVersion(t *testing.T) {
	source := `version = 2

[symlinks.all]
"x/y" = ["z"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for unsupported version, got nil")
	}
}

func testMissingVersion(t *testing.T) {
	source := `[symlinks.all]
"x/y" = ["z"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
}

func testCrossSectionCanonical(t *testing.T) {
	source := `version = 1

[symlinks.all]
".config/example/config" = [".example/config"]

[symlinks.linux]
".config/example/config" = [".local/share/example/config"]
`
	config, err := Decode([]byte(source))
	if err != nil {
		t.Fatalf("cross-section canonical must decode (plan union semantics): %v", err)
	}
	if len(config.Declarations) != 2 {
		t.Fatalf("declarations = %d, want 2", len(config.Declarations))
	}
}

func testDuplicateDestination(t *testing.T) {
	source := `version = 1

[symlinks.all]
"first/target" = ["shared/alias"]
"second/target" = ["shared/alias"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for duplicate alias destination within a section, got nil")
	}
}

func testAbsolutePath(t *testing.T) {
	source := `version = 1

[symlinks.all]
"/etc/passwd" = ["a/b"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for absolute canonical path, got nil")
	}
}

func testDotDotPath(t *testing.T) {
	source := `version = 1

[symlinks.all]
"a/../b" = ["c/d"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for dot-dot path, got nil")
	}
}

func testEmptyPath(t *testing.T) {
	source := `version = 1

[symlinks.all]
"" = ["a/b"]
`
	if _, err := Decode([]byte(source)); err == nil {
		t.Fatal("expected error for empty canonical path, got nil")
	}
}

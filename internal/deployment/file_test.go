package deployment

import "testing"

func TestManagedFileContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"accepts valid file", testAcceptsValidFile},
		{"rejects invalid layer", testFileRejectsLayer},
		{"rejects invalid kind", testFileRejectsKind},
		{"rejects empty source absolute", testFileRejectsSourceAbsolute},
		{"rejects empty repository path", testFileRejectsRepositoryPath},
		{"rejects empty target path", testFileRejectsEmptyTarget},
		{"parses file kinds", testParsesFileKinds},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func validFileCandidate() ManagedFile {
	return ManagedFile{
		Scope:                NewScope("atuin"),
		Layer:                LayerBase,
		Kind:                 FileOrdinary,
		SourceAbsolutePath:   "/repo/atuin/config.toml",
		SourceRepositoryPath: "atuin/config.toml",
		TargetRelativePath:   ".config/atuin/config.toml",
		SourceExecutableBits: 0o755,
	}
}

func testAcceptsValidFile(t *testing.T) {
	candidate := validFileCandidate()
	file, err := NewManagedFile(candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file != candidate {
		t.Fatal("valid candidate must round-trip")
	}
}

func testFileRejectsLayer(t *testing.T) {
	candidate := validFileCandidate()
	candidate.Layer = Layer("windows")
	if _, err := NewManagedFile(candidate); err == nil {
		t.Fatal("expected error for invalid layer")
	}
}

func testFileRejectsKind(t *testing.T) {
	candidate := validFileCandidate()
	candidate.Kind = FileKind("encrypted")
	if _, err := NewManagedFile(candidate); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func testFileRejectsSourceAbsolute(t *testing.T) {
	candidate := validFileCandidate()
	candidate.SourceAbsolutePath = ""
	if _, err := NewManagedFile(candidate); err == nil {
		t.Fatal("expected error for empty source absolute path")
	}
}

func testFileRejectsRepositoryPath(t *testing.T) {
	candidate := validFileCandidate()
	candidate.SourceRepositoryPath = ""
	if _, err := NewManagedFile(candidate); err == nil {
		t.Fatal("expected error for empty repository path")
	}
}

func testFileRejectsEmptyTarget(t *testing.T) {
	candidate := validFileCandidate()
	candidate.TargetRelativePath = ""
	if _, err := NewManagedFile(candidate); err == nil {
		t.Fatal("expected error for empty target path")
	}
}

func testParsesFileKinds(t *testing.T) {
	scenarios := []struct {
		input string
		want  FileKind
	}{
		{"ordinary", FileOrdinary},
		{"secret", FileSecret},
	}
	for _, scenario := range scenarios {
		got, err := ParseFileKind(scenario.input)
		if err != nil {
			t.Fatalf("ParseFileKind(%q) err = %v", scenario.input, err)
		}
		if got != scenario.want {
			t.Fatalf("ParseFileKind(%q) = %v, want %v", scenario.input, got, scenario.want)
		}
	}
}

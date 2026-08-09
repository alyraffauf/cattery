package deployment

import "testing"

func TestAliasContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"accepts valid alias", testAcceptsValidAlias},
		{"rejects empty platform", testAliasRejectsPlatform},
		{"rejects empty canonical", testAliasRejectsCanonical},
		{"rejects empty alias path", testAliasRejectsAliasPath},
		{"accepts valid hook", testAcceptsValidHook},
		{"rejects hook invalid phase", testHookRejectsPhase},
		{"rejects hook empty name", testHookRejectsName},
		{"rejects hook empty absolute", testHookRejectsAbsolute},
		{"rejects hook empty repo path", testHookRejectsRepoPath},
		{"parses hook phases", testParsesHookPhases},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func validAliasCandidate() Alias {
	return Alias{
		Scope:                       NewScope("atuin"),
		Platform:                    "linux",
		CanonicalTargetRelativePath: ".config/atuin/config.toml",
		AliasRelativePath:           ".config/alt-config.toml",
	}
}

func testAcceptsValidAlias(t *testing.T) {
	candidate := validAliasCandidate()
	alias, err := NewAlias(candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alias != candidate {
		t.Fatal("valid candidate must round-trip")
	}
}

func testAliasRejectsPlatform(t *testing.T) {
	candidate := validAliasCandidate()
	candidate.Platform = ""
	if _, err := NewAlias(candidate); err == nil {
		t.Fatal("expected error for empty platform")
	}
}

func testAliasRejectsCanonical(t *testing.T) {
	candidate := validAliasCandidate()
	candidate.CanonicalTargetRelativePath = ""
	if _, err := NewAlias(candidate); err == nil {
		t.Fatal("expected error for empty canonical target")
	}
}

func testAliasRejectsAliasPath(t *testing.T) {
	candidate := validAliasCandidate()
	candidate.AliasRelativePath = ""
	if _, err := NewAlias(candidate); err == nil {
		t.Fatal("expected error for empty alias path")
	}
}

func validHookCandidate() Hook {
	return Hook{
		Scope:          NewScope("atuin"),
		Phase:          HookBefore,
		Name:           "install",
		AbsolutePath:   "/repo/_hooks/install.sh",
		RepositoryPath: "_hooks/install.sh",
	}
}

func testAcceptsValidHook(t *testing.T) {
	candidate := validHookCandidate()
	hook, err := NewHook(candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hook != candidate {
		t.Fatal("valid candidate must round-trip")
	}
}

func testHookRejectsPhase(t *testing.T) {
	candidate := validHookCandidate()
	candidate.Phase = HookPhase("during")
	if _, err := NewHook(candidate); err == nil {
		t.Fatal("expected error for invalid phase")
	}
}

func testHookRejectsName(t *testing.T) {
	candidate := validHookCandidate()
	candidate.Name = ""
	if _, err := NewHook(candidate); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func testHookRejectsAbsolute(t *testing.T) {
	candidate := validHookCandidate()
	candidate.AbsolutePath = ""
	if _, err := NewHook(candidate); err == nil {
		t.Fatal("expected error for empty absolute path")
	}
}

func testHookRejectsRepoPath(t *testing.T) {
	candidate := validHookCandidate()
	candidate.RepositoryPath = ""
	if _, err := NewHook(candidate); err == nil {
		t.Fatal("expected error for empty repository path")
	}
}

func testParsesHookPhases(t *testing.T) {
	scenarios := []struct {
		input string
		want  HookPhase
	}{
		{"before", HookBefore},
		{"after", HookAfter},
	}
	for _, scenario := range scenarios {
		got, err := ParseHookPhase(scenario.input)
		if err != nil {
			t.Fatalf("ParseHookPhase(%q) err = %v", scenario.input, err)
		}
		if got != scenario.want {
			t.Fatalf("ParseHookPhase(%q) = %v, want %v", scenario.input, got, scenario.want)
		}
	}
}

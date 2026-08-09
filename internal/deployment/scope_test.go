package deployment

import "testing"

func TestScopeContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"root scope is explicit", testRootScopeIsExplicit},
		{"group scope preserved", testGroupScopePreserved},
		{"rejects unknown layer", testRejectsUnknownLayer},
		{"accepts known layers", testAcceptsKnownLayers},
		{"layer valid flag", testLayerValidFlag},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRootScopeIsExplicit(t *testing.T) {
	root := NewScope("")
	if !root.IsRoot() {
		t.Fatal("empty group must identify the root scope")
	}
}

func testGroupScopePreserved(t *testing.T) {
	scope := NewScope("atuin")
	if scope.Group != "atuin" {
		t.Fatalf("group = %q", scope.Group)
	}
	if scope.IsRoot() {
		t.Fatal("non-empty group must not report as root")
	}
}

func testRejectsUnknownLayer(t *testing.T) {
	if _, err := ParseLayer("windows"); err == nil {
		t.Fatal("expected error for unknown layer")
	}
}

func testAcceptsKnownLayers(t *testing.T) {
	scenarios := []struct {
		input string
		want  Layer
	}{
		{"base", LayerBase},
		{"darwin", LayerDarwin},
		{"linux", LayerLinux},
	}
	for _, scenario := range scenarios {
		got, err := ParseLayer(scenario.input)
		if err != nil {
			t.Fatalf("ParseLayer(%q) err = %v", scenario.input, err)
		}
		if got != scenario.want {
			t.Fatalf("ParseLayer(%q) = %v, want %v", scenario.input, got, scenario.want)
		}
	}
}

func testLayerValidFlag(t *testing.T) {
	if !LayerBase.Valid() {
		t.Fatal("LayerBase must be valid")
	}
	if Layer("windows").Valid() {
		t.Fatal("unknown layer must not be valid")
	}
}

package pathsafe

import "testing"

func TestEquivalence(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"identical segments are equivalent", testIdenticalSegments},
		{"case differences are equivalent", testCaseDifferences},
		{"nfc and nfd forms are equivalent", testNormalForms},
		{"distinct segments are not equivalent", testDistinctSegments},
		{"equal length paths collide", testPathsCollideEqual},
		{"different length paths do not collide", testPathsDifferentLength},
		{"differing segment prevents collision", testPathsDifferingSegment},
		{"parent is strict prefix", testParentStrictPrefix},
		{"equal paths are not parent", testEqualNotParent},
		{"longer parent is rejected", testLongerParentRejected},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testIdenticalSegments(t *testing.T) {
	if !SegmentsEquivalent("config", "config") {
		t.Fatal("identical segments must be equivalent")
	}
}

func testCaseDifferences(t *testing.T) {
	if !SegmentsEquivalent("Config", "CONFIG") {
		t.Fatal("case-only differences must be treated as equivalent")
	}
}

func testNormalForms(t *testing.T) {
	composed := "\u00e9"
	decomposed := "e\u0301"
	if !SegmentsEquivalent(composed, decomposed) {
		t.Fatal("NFC and NFD forms of the same code point must be equivalent")
	}
}

func testDistinctSegments(t *testing.T) {
	if SegmentsEquivalent("config", "settings") {
		t.Fatal("distinct segments must not be equivalent")
	}
}

func testPathsCollideEqual(t *testing.T) {
	if !PathsEquivalent([]string{"config", "git"}, []string{"Config", "GIT"}) {
		t.Fatal("equal-length portably-equivalent paths must collide")
	}
}

func testPathsDifferentLength(t *testing.T) {
	if PathsEquivalent([]string{"config"}, []string{"config", "git"}) {
		t.Fatal("different-length paths must not collide")
	}
}

func testPathsDifferingSegment(t *testing.T) {
	if PathsEquivalent([]string{"config", "git"}, []string{"config", "shell"}) {
		t.Fatal("paths differing in any segment must not collide")
	}
}

func testParentStrictPrefix(t *testing.T) {
	if !IsParentEquivalent([]string{"config"}, []string{"config", "git", "ignore"}) {
		t.Fatal("shorter equivalent prefix must be a parent")
	}
}

func testEqualNotParent(t *testing.T) {
	if IsParentEquivalent([]string{"config", "git"}, []string{"config", "git"}) {
		t.Fatal("equal paths must not be a parent relationship")
	}
}

func testLongerParentRejected(t *testing.T) {
	if IsParentEquivalent([]string{"config", "git"}, []string{"config"}) {
		t.Fatal("a longer parent must not be treated as a prefix")
	}
}

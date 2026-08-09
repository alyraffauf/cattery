package pathsafe

import "testing"

func TestProtectedTree(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"descendant is protected", testDescendantProtected},
		{"ancestor direction is protected", testAncestorProtected},
		{"equal paths are protected", testEqualProtected},
		{"disjoint paths are not protected", testDisjointNotProtected},
		{"case-fold equivalent is protected", testCaseFoldProtected},
		{"nfc equivalent is protected", testNFCProtected},
		{"contains is strict ancestor", testContainsStrict},
		{"equal is native exact", testEqualNative},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testDescendantProtected(t *testing.T) {
	if !ProtectedTree("/home/user/repo/.config", "/home/user/repo") {
		t.Fatal("target descending into protected must be protected")
	}
}

func testAncestorProtected(t *testing.T) {
	if !ProtectedTree("/home/user/repo", "/home/user/repo/.config") {
		t.Fatal("reverse relation must also be protected")
	}
}

func testEqualProtected(t *testing.T) {
	if !ProtectedTree("/home/user/repo", "/home/user/repo") {
		t.Fatal("equal paths must be protected")
	}
}

func testDisjointNotProtected(t *testing.T) {
	if ProtectedTree("/home/user/other", "/home/user/repo") {
		t.Fatal("disjoint paths must not be protected")
	}
}

func testCaseFoldProtected(t *testing.T) {
	if !ProtectedTree("/home/user/REPO/file", "/home/user/repo") {
		t.Fatal("case-only segment difference must be protected via EqualFold")
	}
}

func testNFCProtected(t *testing.T) {
	composed := "/home/user/" + "\u00e9" + "/file"
	decomposed := "/home/user/" + "e\u0301"
	if !ProtectedTree(composed, decomposed) {
		t.Fatal("NFC/NFD-equivalent segments must be protected")
	}
}

func testContainsStrict(t *testing.T) {
	if !Contains("/home/user/repo", "/home/user/repo/sub") {
		t.Fatal("parent must contain strict child")
	}
	if Contains("/home/user/repo", "/home/user/repo") {
		t.Fatal("contains must be strict, not equal")
	}
}

func testEqualNative(t *testing.T) {
	if !Equal("/home/user/repo", "/home/user/repo") {
		t.Fatal("equal paths must compare native equal")
	}
	if Equal("/home/user/repo", "/home/user/REPO") {
		t.Fatal("native equal must be case-sensitive")
	}
}

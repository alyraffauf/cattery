package pathsafe

import (
	"strings"
	"testing"
)

func TestLexicalPath(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"accepts simple relative path", testAcceptsSimpleRelative},
		{"accepts nested relative path", testAcceptsNestedRelative},
		{"accepts unicode and spaces", testAcceptsUnicodeAndSpaces},
		{"rejects empty path", testRejectsEmptyPath},
		{"rejects absolute path", testRejectsAbsolutePath},
		{"rejects nul byte", testRejectsNulByte},
		{"rejects invalid utf-8", testRejectsInvalidUTF8},
		{"rejects empty segment", testRejectsEmptySegment},
		{"rejects dot segment", testRejectsDotSegment},
		{"rejects dot-dot segment", testRejectsDotDotSegment},
		{"rejects trailing separator", testRejectsTrailingSeparator},
		{"preserves original input in error", testPreservesOriginalInput},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testAcceptsSimpleRelative(t *testing.T) {
	segments, err := Segments(".bashrc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(segments) != 1 || segments[0] != ".bashrc" {
		t.Fatalf("segments = %v", segments)
	}
}

func testAcceptsNestedRelative(t *testing.T) {
	segments, err := Segments("config/git/ignore")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"config", "git", "ignore"}
	if len(segments) != len(want) {
		t.Fatalf("segments = %v, want %v", segments, want)
	}
	for index, value := range want {
		if segments[index] != value {
			t.Fatalf("segments = %v, want %v", segments, want)
		}
	}
}

func testAcceptsUnicodeAndSpaces(t *testing.T) {
	if _, err := Segments("Bakgrund/bild namn"); err != nil {
		t.Fatalf("unicode and spaces must be accepted: %v", err)
	}
}

func testRejectsEmptyPath(t *testing.T) {
	assertRejected(t, "", "empty path")
}

func testRejectsAbsolutePath(t *testing.T) {
	assertRejected(t, "/home/user/file", "absolute path")
}

func testRejectsNulByte(t *testing.T) {
	assertRejected(t, "a\x00b", "nul byte")
}

func testRejectsInvalidUTF8(t *testing.T) {
	assertRejected(t, "bad\xff\xfe", "invalid utf-8")
}

func testRejectsEmptySegment(t *testing.T) {
	assertRejected(t, "a//b", "empty segment")
}

func testRejectsDotSegment(t *testing.T) {
	assertRejected(t, "./config", "dot segment")
}

func testRejectsDotDotSegment(t *testing.T) {
	assertRejected(t, "a/../b", "dot-dot segment")
}

func testRejectsTrailingSeparator(t *testing.T) {
	assertRejected(t, "config/", "empty segment")
}

func testPreservesOriginalInput(t *testing.T) {
	original := "a/../b"
	_, err := Segments(original)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), original) {
		t.Fatalf("error %q omits original input %q", err.Error(), original)
	}
}

func assertRejected(t *testing.T, input, wantReason string) {
	t.Helper()
	segments, err := Segments(input)
	if err == nil {
		t.Fatalf("Segments(%q) = %v, want error containing %q", input, segments, wantReason)
	}
	pathError, ok := err.(*PathError)
	if !ok {
		t.Fatalf("error is %T, want *PathError", err)
	}
	if pathError.Input != input {
		t.Fatalf("Input = %q, want %q", pathError.Input, input)
	}
	if pathError.Reason != wantReason {
		t.Fatalf("Reason = %q, want %q", pathError.Reason, wantReason)
	}
}

func TestGroupName(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"accepts plain name", testGroupNameAcceptsPlain},
		{"accepts unicode and spaces", testGroupNameAcceptsUnicode},
		{"rejects empty", testGroupNameRejectsEmpty},
		{"rejects separator", testGroupNameRejectsSeparator},
		{"rejects dot value", testGroupNameRejectsDotValue},
		{"rejects dot-dot value", testGroupNameRejectsDotDotValue},
		{"rejects leading dot", testGroupNameRejectsLeadingDot},
		{"rejects leading underscore", testGroupNameRejectsLeadingUnderscore},
		{"rejects nul byte", testGroupNameRejectsNul},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testGroupNameAcceptsPlain(t *testing.T) {
	if err := GroupName("work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testGroupNameAcceptsUnicode(t *testing.T) {
	if err := GroupName("Mitt hem"); err != nil {
		t.Fatalf("spaces and unicode must be accepted: %v", err)
	}
}

func testGroupNameRejectsEmpty(t *testing.T) {
	if err := GroupName(""); err == nil {
		t.Fatal("empty group name must be rejected")
	}
}

func testGroupNameRejectsSeparator(t *testing.T) {
	if err := GroupName("a/b"); err == nil {
		t.Fatal("separator in group name must be rejected")
	}
}

func testGroupNameRejectsDotValue(t *testing.T) {
	if err := GroupName("."); err == nil {
		t.Fatal("dot group name must be rejected")
	}
}

func testGroupNameRejectsDotDotValue(t *testing.T) {
	if err := GroupName(".."); err == nil {
		t.Fatal("dot-dot group name must be rejected")
	}
}

func testGroupNameRejectsLeadingDot(t *testing.T) {
	if err := GroupName(".hidden"); err == nil {
		t.Fatal("leading dot must be rejected")
	}
}

func testGroupNameRejectsLeadingUnderscore(t *testing.T) {
	if err := GroupName("_control"); err == nil {
		t.Fatal("leading underscore must be rejected")
	}
}

func testGroupNameRejectsNul(t *testing.T) {
	if err := GroupName("a\x00b"); err == nil {
		t.Fatal("nul byte must be rejected")
	}
}

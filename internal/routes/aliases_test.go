package routes

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func TestAliasDeclaration(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"payload within one directory", testPayloadSameDirectory},
		{"payload climbs out of alias parent", testPayloadClimbs},
		{"payload from home root", testPayloadHomeRoot},
		{"payload with shared prefix", testPayloadSharedPrefix},
		{"payload points into sibling", testPayloadSibling},
		{"self payload is the bare name", testPayloadSelf},
		{"absolute canonical fails", testPayloadAbsoluteCanonical},
		{"absolute alias fails", testPayloadAbsoluteAlias},
		{"dot-dot fails", testPayloadDotDot},
		{"empty fails", testPayloadEmpty},
		{"alias descending into canonical fails", testPayloadDescending},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testPayloadSameDirectory(t *testing.T) {
	assertPayload(t, payloadCase{".config/app/config", ".config/app/alias", "config"})
}

func testPayloadClimbs(t *testing.T) {
	assertPayload(t, payloadCase{".config/ghostty/config",
		"Library/Application Support/com.ghostty/config", "../../../.config/ghostty/config"})
}

func testPayloadHomeRoot(t *testing.T) {
	assertPayload(t, payloadCase{"a/b/c", "x", "a/b/c"})
}

func testPayloadSharedPrefix(t *testing.T) {
	assertPayload(t, payloadCase{"a/b/c", "a/x/y", "../b/c"})
}

func testPayloadSibling(t *testing.T) {
	assertPayload(t, payloadCase{".config/a/config", ".config/b/alias", "../a/config"})
}

func testPayloadSelf(t *testing.T) {
	assertPayload(t, payloadCase{"a/b/c", "a/b/c", "c"})
}

func testPayloadAbsoluteCanonical(t *testing.T) {
	assertPayloadError(t, payloadCase{"/etc/passwd", "x/y", ""})
}

func testPayloadAbsoluteAlias(t *testing.T) {
	assertPayloadError(t, payloadCase{"x/y", "/etc/passwd", ""})
}

func testPayloadDotDot(t *testing.T) {
	assertPayloadError(t, payloadCase{"a/../b", "x/y", ""})
	assertPayloadError(t, payloadCase{"a/b", "x/../y", ""})
}

func testPayloadEmpty(t *testing.T) {
	assertPayloadError(t, payloadCase{"", "x/y", ""})
	assertPayloadError(t, payloadCase{"a/b", "", ""})
}

func testPayloadDescending(t *testing.T) {
	assertPayloadError(t, payloadCase{"a/b", "a/b/c", ""})
	assertPayloadError(t, payloadCase{"a", "a/b", ""})
}

type payloadCase struct {
	canonical string
	alias     string
	want      string
}

func assertPayload(t *testing.T, scenario payloadCase) {
	t.Helper()
	got, err := pathsafe.RelativeAliasPayload(scenario.canonical, scenario.alias)
	if err != nil {
		t.Fatalf("payload for %q -> %q: %v", scenario.alias, scenario.canonical, err)
	}
	if got != scenario.want {
		t.Fatalf("payload = %q, want %q", got, scenario.want)
	}
}

func assertPayloadError(t *testing.T, scenario payloadCase) {
	t.Helper()
	payload, err := pathsafe.RelativeAliasPayload(scenario.canonical, scenario.alias)
	if err == nil {
		t.Fatalf("payload for %q -> %q = %q, want error", scenario.alias, scenario.canonical, payload)
	}
}

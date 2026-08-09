package buildinfo

import (
	"runtime"
	"testing"
)

func TestBuildInformation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"development defaults", testDevelopmentDefaults},
		{"injected release values", testReleaseValues},
		{"malformed timestamp is safe", testMalformedTimestamp},
		{"empty timestamp is unknown", testEmptyTimestamp},
		{"runtime fields populated", testRuntimeFields},
		{"current reads package defaults", testCurrentReadsDefaults},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testDevelopmentDefaults(t *testing.T) {
	snapshot := FromValues("dev", "unknown", "unknown")
	if snapshot.Version != "dev" {
		t.Fatalf("version = %q", snapshot.Version)
	}
	if snapshot.Commit != "unknown" {
		t.Fatalf("commit = %q", snapshot.Commit)
	}
	if snapshot.Timestamp != "unknown" {
		t.Fatalf("timestamp = %q", snapshot.Timestamp)
	}
	if snapshot.HasTimestamp {
		t.Fatal("development timestamp must not parse")
	}
	if !snapshot.BuiltAt.IsZero() {
		t.Fatal("development built-at must be zero")
	}
}

func testReleaseValues(t *testing.T) {
	snapshot := FromValues("v1.2.3", "abcdef1234567890", "2026-08-09T12:00:00Z")
	if snapshot.Version != "v1.2.3" {
		t.Fatalf("version = %q", snapshot.Version)
	}
	if !snapshot.HasTimestamp {
		t.Fatal("RFC3339 timestamp must parse")
	}
	if year := snapshot.BuiltAt.Year(); year != 2026 {
		t.Fatalf("built-at year = %d", year)
	}
	if snapshot.BuiltAt.Location().String() != "UTC" {
		t.Fatalf("location = %q", snapshot.BuiltAt.Location())
	}
}

func testMalformedTimestamp(t *testing.T) {
	snapshot := FromValues("v1.0.0", "deadbeef", "not-a-date")
	if snapshot.HasTimestamp {
		t.Fatal("malformed timestamp must not parse as present")
	}
	if !snapshot.BuiltAt.IsZero() {
		t.Fatal("malformed timestamp must yield zero time")
	}
}

func testEmptyTimestamp(t *testing.T) {
	snapshot := FromValues("v1.0.0", "deadbeef", "")
	if snapshot.HasTimestamp {
		t.Fatal("empty timestamp must not parse")
	}
}

func testRuntimeFields(t *testing.T) {
	snapshot := FromValues("dev", "unknown", "unknown")
	if snapshot.GoVersion != runtime.Version() {
		t.Fatalf("go version = %q", snapshot.GoVersion)
	}
	if snapshot.OperatingSystem != runtime.GOOS {
		t.Fatalf("os = %q", snapshot.OperatingSystem)
	}
	if snapshot.Architecture != runtime.GOARCH {
		t.Fatalf("arch = %q", snapshot.Architecture)
	}
}

func testCurrentReadsDefaults(t *testing.T) {
	snapshot := Current()
	if snapshot.Version != Version {
		t.Fatalf("current version = %q", snapshot.Version)
	}
}

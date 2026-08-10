package version

import (
	"runtime"
	"testing"

	"github.com/alyraffauf/cattery/internal/buildinfo"
)

func TestVersionService(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"construction and invocation need no dependencies", testServiceNoDependencies},
		{"returns the development defaults", testServiceDevelopmentDefaults},
		{"returns the current buildinfo snapshot", testServiceMatchesSnapshot},
		{"returns all runtime fields", testServiceRuntimeFields},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testServiceNoDependencies(t *testing.T) {
	service := NewService()
	result := service.Version()
	if result.GoVersion == "" {
		t.Fatal("invocation returned an empty result")
	}
}

func testServiceDevelopmentDefaults(t *testing.T) {
	service := NewService()
	result := service.Version()
	if result.Version != "dev" {
		t.Fatalf("version = %q", result.Version)
	}
	if result.Commit != "unknown" {
		t.Fatalf("commit = %q", result.Commit)
	}
	if result.Timestamp != "unknown" {
		t.Fatalf("timestamp = %q", result.Timestamp)
	}
	if result.HasTimestamp || !result.BuiltAt.IsZero() {
		t.Fatalf("unexpected release timestamp %v", result.BuiltAt)
	}
}

func testServiceMatchesSnapshot(t *testing.T) {
	service := NewService()
	snapshot := buildinfo.Current()
	result := service.Version()
	if result.Version != snapshot.Version {
		t.Fatalf("version = %q, want %q", result.Version, snapshot.Version)
	}
	if result.Commit != snapshot.Commit {
		t.Fatalf("commit = %q, want %q", result.Commit, snapshot.Commit)
	}
	if result.Timestamp != snapshot.Timestamp {
		t.Fatalf("timestamp = %q, want %q", result.Timestamp, snapshot.Timestamp)
	}
	if result.HasTimestamp != snapshot.HasTimestamp || !result.BuiltAt.Equal(snapshot.BuiltAt) {
		t.Fatalf("built-at = %v, want %v", result.BuiltAt, snapshot.BuiltAt)
	}
}

func testServiceRuntimeFields(t *testing.T) {
	service := NewService()
	result := service.Version()
	if result.GoVersion != runtime.Version() {
		t.Fatalf("go version = %q", result.GoVersion)
	}
	if result.OperatingSystem != runtime.GOOS {
		t.Fatalf("os = %q", result.OperatingSystem)
	}
	if result.Architecture != runtime.GOARCH {
		t.Fatalf("arch = %q", result.Architecture)
	}
}

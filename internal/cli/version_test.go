package cli

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/buildinfo"
	"github.com/spf13/cobra"
)

func TestVersionCommand(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"development defaults", testVersionDevelopment},
		{"release values", testVersionRelease},
		{"single line with newline", testVersionSingleLine},
		{"repeatable invocation", testVersionRepeatable},
		{"writer error", testVersionWriterError},
		{"no backend access", testVersionNoBackend},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// versionFixture builds one version command over isolated output.
func versionFixture(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}})
	return newVersionCommand(runtime), stdout
}

func setBuildInfo(t *testing.T, version, commit, timestamp string) {
	t.Helper()
	previousVersion, previousCommit, previousTimestamp := buildinfo.Version, buildinfo.Commit, buildinfo.BuildTimestamp
	buildinfo.Version, buildinfo.Commit, buildinfo.BuildTimestamp = version, commit, timestamp
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit, buildinfo.BuildTimestamp = previousVersion, previousCommit, previousTimestamp
	})
}

func testVersionDevelopment(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown")
	command, stdout := versionFixture(t)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "cattery dev commit=unknown built=unknown go=" + runtime.Version() +
		" target=" + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testVersionRelease(t *testing.T) {
	setBuildInfo(t, "v1.0.0", "0123456789abcdef", "2026-08-09T00:00:00Z")
	command, stdout := versionFixture(t)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "cattery v1.0.0 commit=0123456789abcdef built=2026-08-09T00:00:00Z go=" + runtime.Version() +
		" target=" + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testVersionSingleLine(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown")
	command, stdout := versionFixture(t)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	output := stdout.String()
	if !strings.HasSuffix(output, "\n") || strings.Count(output, "\n") != 1 {
		t.Fatalf("stdout = %q, want exactly one newline-terminated line", output)
	}
}

func testVersionRepeatable(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown")
	command, stdout := versionFixture(t)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Fatal("output must be newline-terminated")
	}
}

func testVersionWriterError(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown")
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}})
	command := newVersionCommand(runtime)
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

func testVersionNoBackend(t *testing.T) {
	setBuildInfo(t, "dev", "unknown", "unknown")
	command, _ := versionFixture(t)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := command.Execute(); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

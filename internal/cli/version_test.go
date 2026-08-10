package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/version"
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
		{"one call", testVersionOneCall},
		{"writer error", testVersionWriterError},
		{"no backend access", testVersionNoBackend},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// versionServiceFake returns a fixed result.
type versionServiceFake struct {
	result version.Result
	calls  int
}

func (f *versionServiceFake) Version() version.Result {
	f.calls++
	return f.result
}

// versionFixture builds one version command over a recording service.
func versionFixture(t *testing.T, service *versionServiceFake) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}})
	return newVersionCommand(service, runtime), stdout
}

func testVersionDevelopment(t *testing.T) {
	service := &versionServiceFake{result: version.Result{
		Version: "dev", Commit: "unknown", Timestamp: "unknown",
		GoVersion: "go1.26.5", OperatingSystem: "linux", Architecture: "amd64",
	}}
	command, stdout := versionFixture(t, service)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "cattery dev commit=unknown built=unknown go=go1.26.5 target=linux/amd64\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testVersionRelease(t *testing.T) {
	service := &versionServiceFake{result: version.Result{
		Version: "v1.0.0", Commit: "0123456789abcdef", Timestamp: "2026-08-09T00:00:00Z",
		GoVersion: "go1.26.5", OperatingSystem: "darwin", Architecture: "arm64",
	}}
	command, stdout := versionFixture(t, service)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "cattery v1.0.0 commit=0123456789abcdef built=2026-08-09T00:00:00Z go=go1.26.5 target=darwin/arm64\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testVersionSingleLine(t *testing.T) {
	service := &versionServiceFake{result: version.Result{Version: "dev", Commit: "unknown", Timestamp: "unknown"}}
	command, stdout := versionFixture(t, service)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	output := stdout.String()
	if !strings.HasSuffix(output, "\n") || strings.Count(output, "\n") != 1 {
		t.Fatalf("stdout = %q, want exactly one newline-terminated line", output)
	}
}

func testVersionOneCall(t *testing.T) {
	service := &versionServiceFake{result: version.Result{Version: "dev", Commit: "unknown", Timestamp: "unknown"}}
	command, _ := versionFixture(t, service)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if service.calls != 1 {
		t.Fatalf("calls = %d, want one", service.calls)
	}
}

func testVersionWriterError(t *testing.T) {
	service := &versionServiceFake{result: version.Result{Version: "dev", Commit: "unknown", Timestamp: "unknown"}}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: failingWriter{}}})
	command := newVersionCommand(service, runtime)
	if err := command.Execute(); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

func testVersionNoBackend(t *testing.T) {
	service := &versionServiceFake{result: version.Result{Version: "dev", Commit: "unknown", Timestamp: "unknown"}}
	command, _ := versionFixture(t, service)
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := command.Execute(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if service.calls != 2 {
		t.Fatalf("calls = %d, want two independent invocations", service.calls)
	}
}

// unsupportedRepoVersion guards against accidental flag handling.
func unsupportedRepoVersion(t *testing.T, command *cobra.Command) {
	t.Helper()
	if command.Flags().Lookup("repo") != nil {
		t.Fatal("version must not declare repository flags")
	}
}

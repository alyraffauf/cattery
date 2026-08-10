package bootstrap

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/cli"
)

func TestBootstrapBuild(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"application builds", testBuildApplication},
		{"version touches no backend", testBuildVersion},
		{"help touches no backend", testBuildHelp},
		{"parse failure touches no backend", testBuildParseFailure},
		{"two builds are isolated", testBuildIsolation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// buildFixture builds one application over isolated streams.
func buildFixture(t *testing.T) (*cli.Application, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	application := Build(BuildInput{
		Streams:     cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr},
		WorkingDir:  "/work",
		Environment: []string{"HOME=/home", "PATH=/usr/bin"},
		IsTerminal:  func(fd int) bool { return true },
		StateHome:   strings.TrimSuffix(t.TempDir(), "") + "/state",
		Now:         fixedClock(),
	})
	return application, stdout, stderr
}

func testBuildApplication(t *testing.T) {
	application, _, _ := buildFixture(t)
	if application == nil {
		t.Fatal("the application must build")
	}
}

func testBuildVersion(t *testing.T) {
	application, stdout, _ := buildFixture(t)
	if err := application.Execute([]string{"version"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "cattery") {
		t.Fatalf("stdout = %q, want the version line", stdout.String())
	}
}

func testBuildHelp(t *testing.T) {
	application, stdout, _ := buildFixture(t)
	if err := application.Execute(nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "cattery") {
		t.Fatalf("stdout = %q, want the help text", stdout.String())
	}
}

func testBuildParseFailure(t *testing.T) {
	application, _, stderr := buildFixture(t)
	if err := application.Execute([]string{"--version"}); err == nil {
		t.Fatal("an unknown root flag must fail")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want no backend diagnostic on a parse failure", stderr.String())
	}
}

func testBuildIsolation(t *testing.T) {
	first, _, _ := buildFixture(t)
	second, _, _ := buildFixture(t)
	if first == second {
		t.Fatal("two builds must own distinct applications")
	}
}

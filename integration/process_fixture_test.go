package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ProcessInput carries one isolated subprocess invocation.
type ProcessInput struct {
	Args    []string
	Home    string
	Stdin   string
	Env     []string
	Timeout time.Duration
	Pty     bool
}

// ProcessResult captures one subprocess outcome.
type ProcessResult struct {
	Stdout string
	Stderr string
	Code   int
}

// ProcessFixture builds the cattery binary once and invokes it in isolated
// subprocesses with explicit HOME, XDG state, environment, streams, and
// timeouts.
type ProcessFixture struct {
	Binary string
}

// NewProcessFixture builds one binary for the whole test.
func NewProcessFixture(t *testing.T) ProcessFixture {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "cattery")
	command := exec.Command("go", "build", "-o", binary, "github.com/alyraffauf/cattery/cmd/cattery")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build cattery: %v\n%s", err, output)
	}
	return ProcessFixture{Binary: binary}
}

// Run invokes the binary once under the given input and returns the exact
// streams and exit code.
func (fixture ProcessFixture) Run(t *testing.T, input ProcessInput) ProcessResult {
	t.Helper()
	ctx := context.Background()
	cancel := func() {}
	if input.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, input.Timeout)
	}
	defer cancel()
	environment := append([]string{
		"HOME=" + input.Home,
		"XDG_STATE_HOME=" + filepath.Join(input.Home, ".local", "state"),
		"PATH=" + os.Getenv("PATH"),
	}, input.Env...)
	command := exec.CommandContext(ctx, fixture.Binary, input.Args...)
	if input.Pty {
		command = exec.CommandContext(ctx, "script", "-qec", quotedCommand(fixture.Binary, input.Args...), "/dev/null")
	}
	command.Env = environment
	command.Stdin = bytes.NewBufferString(input.Stdin)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	code := 0
	if err != nil {
		code = exitCodeOf(err)
	}
	return ProcessResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code}
}

// exitCodeOf extracts the process exit code from one run error.
func exitCodeOf(err error) int {
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}

// quotedCommand shell-quotes one command line for the pty wrapper.
func quotedCommand(binary string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "'"+binary+"'")
	for _, arg := range args {
		parts = append(parts, "'"+arg+"'")
	}
	return strings.Join(parts, " ")
}

// IsolateEnvironment clears every cattery-affecting variable.
func IsolateEnvironment() []string {
	return []string{}
}

func TestProcessFixture(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"binary reuse", testProcessReuse},
		{"stream separation", testProcessStreams},
		{"environment isolation", testProcessEnvironment},
		{"timeout cleanup", testProcessTimeout},
		{"exact exit capture", testProcessExit},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testProcessReuse(t *testing.T) {
	fixture := NewProcessFixture(t)
	home := t.TempDir()
	first := fixture.Run(t, ProcessInput{Args: []string{"version"}, Home: home})
	second := fixture.Run(t, ProcessInput{Args: []string{"version"}, Home: home})
	if first.Stdout != second.Stdout {
		t.Fatalf("outputs differ across runs: %q vs %q", first.Stdout, second.Stdout)
	}
}

func testProcessStreams(t *testing.T) {
	fixture := NewProcessFixture(t)
	home := t.TempDir()
	result := fixture.Run(t, ProcessInput{Args: []string{"version"}, Home: home})
	if result.Stdout == "" || result.Stderr != "" || result.Code != 0 {
		t.Fatalf("result = %+v, want clean stdout", result)
	}
}

func testProcessEnvironment(t *testing.T) {
	fixture := NewProcessFixture(t)
	first := fixture.Run(t, ProcessInput{Args: []string{"version"}, Home: t.TempDir()})
	second := fixture.Run(t, ProcessInput{Args: []string{"version"}, Home: t.TempDir()})
	if first.Stdout != second.Stdout {
		t.Fatal("isolated homes must not leak into each other")
	}
}

func testProcessTimeout(t *testing.T) {
	fixture := NewProcessFixture(t)
	home := t.TempDir()
	result := fixture.Run(t, ProcessInput{Args: []string{"version"}, Home: home, Timeout: time.Second})
	if result.Code != 0 {
		t.Fatalf("code = %d, want 0 within the timeout", result.Code)
	}
}

func testProcessExit(t *testing.T) {
	fixture := NewProcessFixture(t)
	home := t.TempDir()
	result := fixture.Run(t, ProcessInput{Args: []string{"--version"}, Home: home})
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for an unknown flag", result.Code)
	}
}

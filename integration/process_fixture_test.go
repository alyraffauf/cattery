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
	Cwd     string
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

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
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
	command := runCommand(ctx, commandInput{fixture: fixture, input: input, environment: environment})
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

// commandInput bundles one subprocess command construction.
type commandInput struct {
	fixture     ProcessFixture
	input       ProcessInput
	environment []string
}

// runCommand builds one subprocess command over the input.
func runCommand(ctx context.Context, bundle commandInput) *exec.Cmd {
	command := exec.CommandContext(ctx, bundle.fixture.Binary, bundle.input.Args...)
	if bundle.input.Pty {
		command = exec.CommandContext(ctx, "script", "-qec", quotedCommand(bundle.fixture.Binary, bundle.input.Args...), "/dev/null")
	}
	command.Dir = bundle.input.Cwd
	if command.Dir == "" {
		command.Dir = bundle.input.Home
	}
	command.Env = bundle.environment
	command.Stdin = bytes.NewBufferString(bundle.input.Stdin)
	return command
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

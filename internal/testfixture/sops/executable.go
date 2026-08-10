// Package sops ships a test-only fake of the external sops executable. It lets
// higher-level tests drive the secrets adapter and the subprocess runner with
// controlled stdout, stderr, exit codes, stdin echo, and a sleeping descendant
// for cancellation checks, without touching real credentials.
//
// Production code cannot import this package. Build compiles a tiny standalone
// program (fakeSource) into a fresh temp directory; each Command writes a JSON
// behavior file and wires the compiled binary to read it through the
// FAKE_SOPS_SPEC environment variable.
package sops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These environment variables connect Command to the compiled fake.
const (
	behaviorSpecEnvironment     = "FAKE_SOPS_SPEC"
	invocationRecordEnvironment = "FAKE_SOPS_RECORD"
)

// Executable is a handle to the compiled fake binary.
type Executable struct {
	Path string
}

// Behavior describes the controlled behavior one fake invocation emits.
type Behavior struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Sleep     time.Duration
	EchoStdin bool
}

// Build compiles the fake into a temp directory and registers its cleanup.
// The returned Executable points at the freshly built binary.
func Build(t *testing.T) *Executable {
	t.Helper()
	directory := t.TempDir()
	writeMain(t, directory)
	binary := filepath.Join(directory, "fake")
	if err := buildFake(directory, binary); err != nil {
		t.Fatalf("sops fixture build failed: %v", err)
	}
	return &Executable{Path: binary}
}

// Command returns an *exec.Cmd wired to run the fake with the given behavior.
// The behavior is serialized to a spec file; the fake reads it via the
// behaviorSpecEnvironment variable.
func (executable *Executable) Command(behavior Behavior) (*exec.Cmd, error) {
	directory := filepath.Dir(executable.Path)
	behaviorSpecPath, err := writeSpec(directory, behavior)
	if err != nil {
		return nil, err
	}
	record, err := uniquePath(directory, "record")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable.Path)
	cmd.Env = append(os.Environ(), behaviorSpecEnvironment+"="+behaviorSpecPath, invocationRecordEnvironment+"="+record)
	return cmd, nil
}

func writeMain(t *testing.T, directory string) {
	t.Helper()
	path := filepath.Join(directory, "main.go")
	if err := os.WriteFile(path, []byte(fakeSource), 0o600); err != nil {
		t.Fatalf("write fake source: %v", err)
	}
}

func buildFake(directory, binary string) error {
	if err := writeMod(directory); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = directory
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeMod(directory string) error {
	path := filepath.Join(directory, "go.mod")
	content := "module fake\n\ngo 1.25.0\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

func writeSpec(directory string, behavior Behavior) (string, error) {
	file, err := os.CreateTemp(directory, "spec-*.json")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(behavior); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func uniquePath(directory, prefix string) (string, error) {
	file, err := os.CreateTemp(directory, prefix+"-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

// fakeSource is the standalone program compiled into the fixture binary. It
// reads its behavior from the spec file named by specEnv: writes controlled
// stdout/stderr, echoes stdin when requested, exits with the configured code,
// or sleeps. When it sleeps it also spawns a child in the same process group so
// callers can verify group-wide cancellation. The child re-enters via childEnv
// and simply sleeps without reading a spec.
const fakeSource = `package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"
)

type behaviorSpec struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Sleep     time.Duration
	EchoStdin bool
}

type record struct {
	Argv     []string
	Cwd      string
	Stdin    []byte
	ChildPid int
}

func main() {
	if os.Getenv("FAKE_SOPS_CHILD") == "1" {
		time.Sleep(time.Hour)
		return
	}
	run()
}

func run() {
	behavior := loadBehavior()
	stdin, _ := io.ReadAll(os.Stdin)
	invocationRecord := record{Argv: os.Args, Cwd: currentWorkingDirectory(), Stdin: stdin}
	if behavior.Sleep > 0 {
		invocationRecord.ChildPid = spawnChild()
	}
	writeRecord(invocationRecord)
	os.Stderr.Write(behavior.Stderr)
	os.Stdout.Write(behavior.Stdout)
	if behavior.EchoStdin {
		os.Stdout.Write(stdin)
	}
	if behavior.Sleep > 0 {
		time.Sleep(behavior.Sleep)
	}
	os.Exit(behavior.ExitCode)
}

func loadBehavior() behaviorSpec {
	data, err := os.ReadFile(os.Getenv("FAKE_SOPS_SPEC"))
	if err != nil {
		os.Exit(2)
	}
	var behavior behaviorSpec
	if err := json.Unmarshal(data, &behavior); err != nil {
		os.Exit(2)
	}
	return behavior
}

func writeRecord(invocationRecord record) {
	path := os.Getenv("FAKE_SOPS_RECORD")
	if path == "" {
		return
	}
	data, err := json.Marshal(invocationRecord)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func spawnChild() int {
	executablePath, err := os.Executable()
	if err != nil {
		return 0
	}
	childProcess := exec.Command(executablePath)
	childProcess.Env = append(os.Environ(), "FAKE_SOPS_CHILD=1")
	if err := childProcess.Start(); err != nil {
		return 0
	}
	return childProcess.Process.Pid
}

func currentWorkingDirectory() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}
`

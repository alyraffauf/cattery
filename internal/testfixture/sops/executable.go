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

// specEnv and recordEnv name the contract between Command and the compiled
// fake: the behavior spec path and the metadata log path.
const (
	specEnv   = "FAKE_SOPS_SPEC"
	recordEnv = "FAKE_SOPS_RECORD"
	childEnv  = "FAKE_SOPS_CHILD"
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
// The behavior is serialized to a spec file; the fake reads it via specEnv.
func (executable *Executable) Command(behavior Behavior) (*exec.Cmd, error) {
	directory := filepath.Dir(executable.Path)
	spec, err := writeSpec(directory, behavior)
	if err != nil {
		return nil, err
	}
	record := uniquePath(directory, "record")
	cmd := exec.Command(executable.Path)
	cmd.Env = append(os.Environ(), specEnv+"="+spec, recordEnv+"="+record)
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

func uniquePath(directory, prefix string) string {
	file, err := os.CreateTemp(directory, prefix+"-*.json")
	if err != nil {
		return ""
	}
	path := file.Name()
	file.Close()
	return path
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

type spec struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Sleep     time.Duration
	EchoStdin bool
}

type record struct {
	Argv     []string
	Cwd      string
	Pid      int
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
	current := loadSpec()
	rec := record{Argv: os.Args, Cwd: cwd(), Pid: os.Getpid()}
	if current.Sleep > 0 {
		rec.ChildPid = spawnChild()
	}
	writeRecord(rec)
	os.Stderr.Write(current.Stderr)
	os.Stdout.Write(current.Stdout)
	if current.EchoStdin {
		io.Copy(os.Stdout, os.Stdin)
	}
	if current.Sleep > 0 {
		time.Sleep(current.Sleep)
	}
	os.Exit(current.ExitCode)
}

func loadSpec() spec {
	data, err := os.ReadFile(os.Getenv("FAKE_SOPS_SPEC"))
	if err != nil {
		os.Exit(2)
	}
	var current spec
	if err := json.Unmarshal(data, &current); err != nil {
		os.Exit(2)
	}
	return current
}

func writeRecord(rec record) {
	path := os.Getenv("FAKE_SOPS_RECORD")
	if path == "" {
		return
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func spawnChild() int {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "FAKE_SOPS_CHILD=1")
	if err := cmd.Start(); err != nil {
		return 0
	}
	return cmd.Process.Pid
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}
`

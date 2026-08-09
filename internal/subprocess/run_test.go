package subprocess

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestProcessRun(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"normal", testNormalRun},
		{"nonzero", testNonzeroRun},
		{"missing", testMissingExecutable},
		{"non-missing path error", testNonMissingPathError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testNonMissingPathError(t *testing.T) {
	cause := &os.PathError{Op: "start", Path: "/not-executable", Err: syscall.EACCES}
	launchErr := launchError(cause)
	if launchErr.NotFound {
		t.Fatal("NotFound = true for a non-ENOENT path error")
	}
	if !errors.Is(launchErr, cause) {
		t.Fatal("LaunchError did not preserve the original cause")
	}
}

func testNormalRun(t *testing.T) {
	var stdout bytes.Buffer
	request := Request{
		Command: []string{"go", "env", "GOVERSION"},
		Stdout:  &stdout,
	}
	result, err := Run(context.Background(), request)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", result.ExitCode)
	}
	if !strings.HasPrefix(stdout.String(), "go") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func testNonzeroRun(t *testing.T) {
	target := scriptTarget{name: "exit3.sh", body: "exit 3"}
	script := writeScript(t, target)
	request := Request{Command: []string{script}}
	result, err := Run(context.Background(), request)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if result.ExitCode != 3 {
		t.Fatalf("exit = %d, want 3", result.ExitCode)
	}
}

func testMissingExecutable(t *testing.T) {
	request := Request{
		Command: []string{"cattery-definitely-not-a-real-binary"},
	}
	_, err := Run(context.Background(), request)
	if err == nil {
		t.Fatal("err = nil, want LaunchError")
	}
	var launchErr *LaunchError
	if !errors.As(err, &launchErr) {
		t.Fatalf("err type = %T, want *LaunchError", err)
	}
	if !launchErr.NotFound {
		t.Fatal("NotFound = false, want true")
	}
}

type scriptTarget struct {
	directory string
	name      string
	body      string
}

func writeScript(t *testing.T, target scriptTarget) string {
	t.Helper()
	if target.directory == "" {
		target.directory = t.TempDir()
	}
	path := filepath.Join(target.directory, target.name)
	content := "#!/bin/sh\n" + target.body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

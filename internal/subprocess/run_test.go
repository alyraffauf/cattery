package subprocess

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
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

// scriptTarget bundles the inputs needed by writeScript so the helper stays
// under the three-parameter limit. directory is created fresh when empty.
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

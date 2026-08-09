package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSOPSClient(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"missing executable", testMissingExecutable},
		{"negative stdout limit", testNegativeStdoutLimit},
		{"nonzero exit", testNonzeroExit},
		{"large stderr", testLargeStderr},
		{"stdout over limit", testStdoutOverLimit},
		{"working directory", testWorkingDirectory},
		{"environment", testEnvironment},
		{"environment copy", testEnvironmentCopy},
		{"descendants", testDescendants},
		{"buffer zeroing", testBufferZeroing},
		{"drain capture", testDrainCapture},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func basicRequest(operation string, source string) Request {
	return Request{
		Operation:   operation,
		SourcePath:  source,
		Arguments:   []string{operation},
		StdoutLimit: 4096,
	}
}
func testMissingExecutable(t *testing.T) {
	repository := t.TempDir()
	client := NewClient(filepath.Join(repository, "no-such-sops"), repository, nil)
	output, err := client.Run(context.Background(), basicRequest("encrypt", "app/token"))
	expectKind(t, err, failure.Dependency)
	if output != nil || !strings.Contains(err.Error(), "sops") {
		t.Fatalf("output = %v, err = %q", output, err.Error())
	}
}

func testNegativeStdoutLimit(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, environment := newTestClient(t, clientTarget{executable: executable, repository: repository})
	request := basicRequest("encrypt", "app/token")
	request.StdoutLimit = -1
	output, err := client.Run(context.Background(), request)
	expectKind(t, err, failure.Operational)
	if output != nil {
		t.Fatalf("output = %v, want none", output)
	}
	if _, recorded := peekRecord(envValue(environment, "FAKE_SOPS_RECORD")); recorded {
		t.Fatal("fixture was launched")
	}
}

// testNonzeroExit also covers the plaintext-stderr redaction rule: neither
// captured stream may enter the returned error.
func testNonzeroExit(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	secret := "plaintext-must-not-leak"
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: []byte("partial-ciphertext"), Stderr: []byte(secret), ExitCode: 4}})
	request := basicRequest("encrypt", "app/token")
	request.Stdin = []byte("plaintext")
	output, err := client.Run(context.Background(), request)
	expectKind(t, err, failure.Operational)
	if output != nil {
		t.Fatalf("output = %d bytes, want none", len(output))
	}
	message := err.Error()
	if strings.Contains(message, "partial-ciphertext") || strings.Contains(message, secret) {
		t.Fatalf("captured bytes leaked into error: %q", message)
	}
	if !strings.Contains(message, "app/token exited with status 4") {
		t.Fatalf("diagnostic missing context: %q", message)
	}
}
func testStdoutOverLimit(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	payload := bytes.Repeat([]byte("x"), 2*1024*1024)
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: payload}})
	output, err := client.Run(context.Background(), basicRequest("encrypt", "app/token"))
	expectKind(t, err, failure.Operational)
	if output != nil {
		t.Fatalf("output = %d bytes, want none", len(output))
	}
	if !strings.Contains(err.Error(), "exceeded limit") {
		t.Fatalf("limit not named: %q", err.Error())
	}
}
func testWorkingDirectory(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, env := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: []byte("known-output")}})
	output, err := client.Run(context.Background(), basicRequest("encrypt", "app/token"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(output, []byte("known-output")) {
		t.Fatalf("output = %q, want known-output", output)
	}
	if rec := readRecord(t, env); rec.Cwd != repository {
		t.Fatalf("cwd = %q, want %q", rec.Cwd, repository)
	}
}
func testEnvironment(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository})
	_, err := client.Run(context.Background(), basicRequest("encrypt", "app/token"))
	if err != nil {
		t.Fatalf("fixture environment failed: %v", err)
	}
	stripped := NewClient(executable.Path, repository, []string{})
	_, err = stripped.Run(context.Background(), basicRequest("encrypt", "app/token"))
	expectKind(t, err, failure.Operational)
	if !strings.Contains(err.Error(), "status 2") {
		t.Fatalf("fake spec lookup not skipped: %q", err.Error())
	}
}
func testDescendants(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, env := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Sleep: 30 * time.Second}})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := client.Run(ctx, basicRequest("encrypt", "app/token")); finished <- err }()
	record := waitForRecord(t, env)
	cancel()
	err := <-finished
	expectKind(t, err, failure.Operational)
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation not named: %q", err.Error())
	}
	if processAlive(record.ChildPid) {
		t.Fatal("descendant survived cancellation")
	}
}

type clientTarget struct {
	executable *sops.Executable
	repository string
	behavior   sops.Behavior
}

func newTestClient(t *testing.T, target clientTarget) (*Client, []string) {
	t.Helper()
	cmd, err := target.executable.Command(target.behavior)
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(target.executable.Path, target.repository, cmd.Env), cmd.Env
}

type fixtureRecord struct {
	Argv     []string
	Cwd      string
	Stdin    []byte
	ChildPid int
}

func readRecord(t *testing.T, env []string) fixtureRecord {
	t.Helper()
	rec, ok := peekRecord(envValue(env, "FAKE_SOPS_RECORD"))
	if !ok {
		t.Fatal("fake record not readable")
	}
	return rec
}
func envValue(env []string, name string) string {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
func waitForRecord(t *testing.T, env []string) fixtureRecord {
	t.Helper()
	path := envValue(env, "FAKE_SOPS_RECORD")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := peekRecord(path); ok && rec.ChildPid != 0 {
			return rec
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fake record with child pid not written before timeout")
	return fixtureRecord{}
}
func peekRecord(path string) (fixtureRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fixtureRecord{}, false
	}
	var rec fixtureRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return fixtureRecord{}, false
	}
	return rec, true
}
func expectKind(t *testing.T, err error, want failure.Kind) {
	t.Helper()
	kind, ok := failure.HasKind(err)
	if !ok || kind != want {
		t.Fatalf("kind = %q, want %s (err %v)", kind, want, err)
	}
}
func processAlive(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

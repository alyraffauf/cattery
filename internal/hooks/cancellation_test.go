//go:build unix

// This file exercises the Section 10.4 cancellation contract of Execute:
// a canceled context stops execution, signals each started hook's process
// group, and kills descendants, and neither phase starts further hooks.
package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestHookCancellation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"cancel before start", testCancelBeforeStart},
		{"cancellation kills descendants", testCancelKillsDescendants},
		{"before sequence stops on cancellation", testCancelStopsBeforeSequence},
		{"after sequence stops on cancellation", testCancelStopsAfterSequence},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testCancelBeforeStart(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hook := writeTestHook(t, dir, hookSpec{name: "touch.sh", phase: deployment.HookBefore, body: "touch ran.txt"})
	input := executeInput(dir, deployment.HookBefore, "pending")
	err := Execute(ctx, input, []deployment.Hook{hook})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context cancellation", err)
	}
	if fileExists(filepath.Join(dir, "ran.txt")) {
		t.Fatal("canceled execution must not start hooks")
	}
}

func testCancelKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hook := writeTestHook(t, dir, hookSpec{
		name: "spawner.sh", phase: deployment.HookBefore,
		body: "trap 'exit 0' TERM\nsleep 30 &\necho $! > child.pid\nwait $!\n",
	})
	waitCh := executeInBackground(ctx, dir, []deployment.Hook{hook})
	childPID := waitForPIDFile(t, filepath.Join(dir, "child.pid"))
	sleepBriefly()
	cancel()
	if !awaitProcessDeath(childPID, 5*time.Second) {
		t.Fatalf("descendant %d still alive after cancellation", childPID)
	}
	if err := awaitExecuteResult(waitCh); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want cancellation or clean shutdown", err)
	}
}

func testCancelStopsBeforeSequence(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spawner := writeTestHook(t, dir, hookSpec{
		name: "spawner.sh", phase: deployment.HookBefore,
		body: "trap 'exit 0' TERM\nsleep 30 &\necho $! > child.pid\nwait $!\n",
	})
	marker := writeTestHook(t, dir, hookSpec{name: "marker.sh", phase: deployment.HookBefore, body: "touch ran.txt"})
	waitCh := executeInBackground(ctx, dir, []deployment.Hook{spawner, marker})
	waitForPIDFile(t, filepath.Join(dir, "child.pid"))
	cancel()
	err := awaitExecuteResult(waitCh)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context cancellation", err)
	}
	if fileExists(filepath.Join(dir, "ran.txt")) {
		t.Fatal("remaining before hooks must not run after cancellation")
	}
}

func testCancelStopsAfterSequence(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spawner := writeTestHook(t, dir, hookSpec{
		name: "spawner.sh", phase: deployment.HookAfter,
		body: "trap 'exit 0' TERM\nsleep 30 &\necho $! > child.pid\nwait $!\n",
	})
	marker := writeTestHook(t, dir, hookSpec{name: "marker.sh", phase: deployment.HookAfter, body: "touch ran.txt"})
	input := executeInput(dir, deployment.HookAfter, "success")
	waitCh := make(chan error, 1)
	go func() { waitCh <- Execute(ctx, input, []deployment.Hook{spawner, marker}) }()
	waitForPIDFile(t, filepath.Join(dir, "child.pid"))
	cancel()
	err := awaitExecuteResult(waitCh)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context cancellation", err)
	}
	if fileExists(filepath.Join(dir, "ran.txt")) {
		t.Fatal("remaining after hooks must not run after cancellation")
	}
}

// executeInBackground runs the sequence on the background goroutine.
func executeInBackground(ctx context.Context, dir string, ordered []deployment.Hook) chan error {
	waitCh := make(chan error, 1)
	input := executeInput(dir, deployment.HookBefore, "pending")
	go func() { waitCh <- Execute(ctx, input, ordered) }()
	return waitCh
}

// awaitExecuteResult waits for Execute to return within the deadline.
func awaitExecuteResult(waitCh <-chan error) error {
	select {
	case err := <-waitCh:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("Execute did not return after cancellation")
	}
}

func sleepBriefly() {
	time.Sleep(300 * time.Millisecond)
}

// waitForPIDFile waits for a child.pid file to appear and returns its PID.
func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pid, ok := readPIDFile(path); ok {
			return pid
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid file %s never appeared", path)
	return 0
}

func readPIDFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil {
		return 0, false
	}
	return pid, true
}

// awaitProcessDeath reports whether pid exited within the limit.
func awaitProcessDeath(pid int, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !processAlive(pid)
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

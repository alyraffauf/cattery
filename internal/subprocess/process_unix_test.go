//go:build unix

package subprocess

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessCancellation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"cancel-before", testCancelBeforeStart},
		{"cancel-after", testCancelAfterStart},
		{"descendants", testDescendantsDie},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testCancelBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := Request{Command: []string{"sleep", "30"}}
	result, err := Run(ctx, request)
	if err == nil {
		t.Fatal("err = nil, want cancellation")
	}
	if !result.SignalCanceled {
		t.Fatal("SignalCanceled = false, want true")
	}
}

func testCancelAfterStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := "trap 'exit 0' TERM\nsleep 30"
	script := writeScript(t, scriptTarget{name: "sleep.sh", body: body})
	waitCh := runInBackground(ctx, script)
	sleepBriefly()
	cancel()
	observed := awaitResult(t, waitCh)
	if !observed.result.SignalCanceled {
		t.Fatal("SignalCanceled = false, want true")
	}
}

func testDescendantsDie(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	body := "trap 'exit 0' TERM\nsleep 30 &\necho $! > child.pid\nwait $!\n"
	target := scriptTarget{directory: dir, name: "spawner.sh", body: body}
	script := writeScript(t, target)
	go func() {
		request := Request{Command: []string{script}, Directory: dir}
		_, _ = Run(ctx, request)
	}()
	childPID := waitForPIDFile(t, filepath.Join(dir, "child.pid"))
	sleepBriefly()
	cancel()
	if !awaitProcessDeath(childPID, 5*time.Second) {
		t.Fatalf("descendant %d still alive after cancel", childPID)
	}
}

func runInBackground(ctx context.Context, script string) chan outcome {
	waitCh := make(chan outcome, 1)
	go func() {
		result, err := Run(ctx, Request{Command: []string{script}})
		waitCh <- outcome{result: result, err: err}
	}()
	return waitCh
}

func awaitResult(t *testing.T, waitCh <-chan outcome) outcome {
	t.Helper()
	select {
	case observed := <-waitCh:
		return observed
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
		return outcome{}
	}
}

func sleepBriefly() {
	time.Sleep(300 * time.Millisecond)
}

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

package sops

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSOPSExecutableFixture(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"round trip echoes stdin", testRoundTripEchoesStdin},
		{"large output", testLargeOutput},
		{"descendant dies with group", testDescendantDiesWithGroup},
		{"cleanup removes the binary", testCleanupRemovesBinary},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRoundTripEchoesStdin(t *testing.T) {
	executable := Build(t)
	cmd, err := executable.Command(Behavior{EchoStdin: true})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("arbitrary bytes with \x00 nul and \xff byte\n")
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("fake run failed: %v", err)
	}
	if !bytes.Equal(output, payload) {
		t.Fatalf("echo mismatch: got %d bytes, want %d", len(output), len(payload))
	}
}

func testLargeOutput(t *testing.T) {
	executable := Build(t)
	payload := bytes.Repeat([]byte("cattery-secret-"), 8192)
	cmd, err := executable.Command(Behavior{Stdout: payload})
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("fake run failed: %v", err)
	}
	if !bytes.Equal(output, payload) {
		t.Fatalf("output mismatch: got %d bytes, want %d", len(output), len(payload))
	}
}

func testDescendantDiesWithGroup(t *testing.T) {
	executable := Build(t)
	cmd, err := executable.Command(Behavior{Sleep: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	child := waitForChild(t, cmd)
	killGroup(t, cmd.Process.Pid)
	_ = cmd.Wait()
	if !processGone(child) {
		t.Fatalf("descendant %d survived group kill", child)
	}
}

func testCleanupRemovesBinary(t *testing.T) {
	var path string
	t.Run("scope", func(inner *testing.T) {
		executable := Build(inner)
		path = executable.Path
		if _, err := os.Stat(path); err != nil {
			inner.Fatalf("binary missing after build: %v", err)
		}
	})
	if _, err := os.Stat(path); err == nil {
		t.Fatal("binary still exists after cleanup")
	}
}

func waitForChild(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	path := recordPath(cmd)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if pid := childPidOf(path); pid != 0 {
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("descendant pid not recorded before timeout")
	return 0
}

func recordPath(cmd *exec.Cmd) string {
	prefix := invocationRecordEnvironment + "="
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func childPidOf(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var invocationRecord struct{ ChildPid int }
	if err := json.Unmarshal(data, &invocationRecord); err != nil {
		return 0
	}
	return invocationRecord.ChildPid
}

func killGroup(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill process group failed: %v", err)
	}
}

func processGone(pid int) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

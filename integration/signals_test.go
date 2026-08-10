package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestExecutableSignals(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"interrupt is 130", testSignalsInterrupt},
		{"terminate is 143", testSignalsTerminate},
		{"descendants terminate", testSignalsDescendants},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// slowHook writes one hook that signals readiness and sleeps.
func slowHook(t *testing.T, env execEnv, phase string) {
	t.Helper()
	content := "#!/bin/sh\ntouch $CATTERY_HOME/hook-ready\ntrap '' TERM\necho waiting\nsleep 30\n"
	path := filepath.Join(env.repo, "_hooks", phase, phase+".sh")
	writeFile(t, path, []byte(content))
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// startApply launches one apply and waits for the hook readiness marker.
func startApply(t *testing.T, env execEnv) *exec.Cmd {
	t.Helper()
	command := exec.Command(env.fixture.Binary, "apply")
	command.Env = []string{
		"HOME=" + env.home,
		"XDG_STATE_HOME=" + filepath.Join(env.home, ".local", "state"),
		"PATH=" + os.Getenv("PATH"),
	}
	command.Dir = env.home
	if err := command.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	marker := filepath.Join(env.home, "hook-ready")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return command
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = command.Process.Kill()
	t.Fatal("hook never became ready")
	return nil
}

// finishSignal waits for the process and returns its exit code.
func finishSignal(t *testing.T, command *exec.Cmd) int {
	t.Helper()
	err := command.Wait()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	t.Fatalf("wait: %v", err)
	return -1
}

func testSignalsInterrupt(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	slowHook(t, env, "before")
	command := startApply(t, env)
	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if code := finishSignal(t, command); code != 130 {
		t.Fatalf("code = %d, want 130", code)
	}
}

func testSignalsTerminate(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	slowHook(t, env, "before")
	command := startApply(t, env)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := finishSignal(t, command); code != 143 {
		t.Fatalf("code = %d, want 143", code)
	}
}

func testSignalsDescendants(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	child := filepath.Join(env.repo, "_hooks", "before", "before.sh")
	content := "#!/bin/sh\n(sh -c 'sleep 30; touch $CATTERY_HOME/descendant-live') &\ntouch $CATTERY_HOME/hook-ready\nsleep 30\n"
	writeFile(t, child, []byte(content))
	if err := os.Chmod(child, 0o755); err != nil {
		t.Fatal(err)
	}
	command := startApply(t, env)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := finishSignal(t, command); code != 143 {
		t.Fatalf("code = %d, want 143", code)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(env.home, "descendant-live")); err == nil {
		t.Fatal("a descendant must be terminated with the hook group")
	}
}

//go:build unix

package subprocess

import (
	"errors"
	"os/exec"
	"syscall"
)

// processHandle wraps exec.Cmd for the runner. Its zero value is not usable;
// startProcess is the only constructor.
type processHandle struct {
	cmd *exec.Cmd
}

// startProcess launches the requested program in its own Unix process group
// (SysProcAttr.Setpgid) so SIGTERM/SIGKILL reach all descendants. An empty
// Command yields an inline error rather than a sentinel so the package keeps
// no package-level var.
func startProcess(request Request) (*processHandle, error) {
	if len(request.Command) == 0 {
		return nil, errors.New("subprocess: empty command")
	}
	cmd := exec.Command(request.Command[0], request.Command[1:]...)
	cmd.Dir = request.Directory
	cmd.Env = request.Environment
	cmd.Stdin = request.Stdin
	cmd.Stdout = request.Stdout
	cmd.Stderr = request.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &processHandle{cmd: cmd}, nil
}

func (h *processHandle) pid() int {
	if h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

func (h *processHandle) wait() error {
	return h.cmd.Wait()
}

func (h *processHandle) exitCode() int {
	if h.cmd.ProcessState == nil {
		return -1
	}
	return h.cmd.ProcessState.ExitCode()
}

// terminateGroup sends SIGTERM to the child's process group.
func terminateGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// killGroup sends SIGKILL to the child's process group after the grace period.
func killGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

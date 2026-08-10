// Package subprocess runs external programs with caller-supplied streams and
// process-group cancellation. It owns process creation, explicit streams,
// process-group shutdown, and exit status only; it neither captures nor
// redacts content. Callers wire stdin/stdout/stderr and own any buffering,
// limits, or redaction (internal/secrets and internal/hooks do this).
package subprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// gracePeriod is the SIGTERM-to-SIGKILL interval for canceled processes.
const gracePeriod = 5 * time.Second

// groupPollInterval paces the bounded wait for a canceled process group to
// disappear after SIGKILL.
const groupPollInterval = 5 * time.Millisecond

// groupShutdownTimeout bounds the post-SIGKILL wait so an unkillable child
// (e.g. stuck in uninterruptible disk sleep) cannot hang the caller forever.
// A timeout is reported as a partial cancellation rather than an infinite block.
const groupShutdownTimeout = 30 * time.Second

// Request describes one synchronous child invocation. Command[0] is the
// executable; Directory and Environment default to inherited when empty so
// the package stays neutral about caller policy.
type Request struct {
	Command     []string
	Directory   string
	Environment []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

// Result is the typed outcome of Run. A nonzero ExitCode is NOT an error:
// callers inspect ExitCode. SignalCanceled is true whenever Run returned
// because ctx was canceled, even if no signal was sent (cancel-before-start).
type Result struct {
	ExitCode       int
	SignalCanceled bool
	Duration       time.Duration
}

// LaunchError describes a failure to start the child. When NotFound is true
// the executable was missing from PATH or the filesystem; callers (secrets)
// map this to their own dependency category.
type LaunchError struct {
	NotFound bool
	Cause    error
}

func (e *LaunchError) Error() string {
	if e == nil {
		return "subprocess: launch failed"
	}
	if e.NotFound {
		return fmt.Sprintf("subprocess: executable not found: %v", e.Cause)
	}
	return fmt.Sprintf("subprocess: launch failed: %v", e.Cause)
}

// Unwrap exposes Cause to errors.Is and errors.As.
func (e *LaunchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Run executes request synchronously, applying the process-group
// cancellation policy: SIGTERM to the child group on ctx cancellation, a
// gracePeriod wait, then SIGKILL. Nonzero exit codes return (Result, nil);
// only launch failures and cancellation return an error.
func Run(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{SignalCanceled: true}, err
	}
	start := time.Now()
	handle, err := startProcess(request)
	if err != nil {
		return Result{Duration: time.Since(start)}, launchError(err)
	}
	observed := runAndWait(ctx, handle, start)
	return observed.result, observed.err
}

type outcome struct {
	result Result
	err    error
}

type groupShutdown struct {
	handle  *processHandle
	waitCh  chan error
	elapsed time.Duration
}

func runAndWait(ctx context.Context, handle *processHandle, start time.Time) outcome {
	waitCh := make(chan error, 1)
	go func() { waitCh <- handle.wait() }()
	select {
	case <-ctx.Done():
		shutdown := groupShutdown{handle: handle, waitCh: waitCh, elapsed: time.Since(start)}
		return awaitShutdown(ctx, shutdown)
	case err := <-waitCh:
		return outcome{result: finalize(handle, time.Since(start)), err: exitError(err)}
	}
}

func awaitShutdown(ctx context.Context, shutdown groupShutdown) outcome {
	pid := shutdown.handle.pid()
	// ESRCH here means the process already exited; terminateGroup's error is
	// only meaningful for unexpected failures, so it is intentionally ignored.
	_ = terminateGroup(pid)
	select {
	case <-shutdown.waitCh:
		waitForGroupExit(pid)
		return canceled(shutdown.elapsed, ctx.Err())
	case <-time.After(gracePeriod):
	}
	// As above, ESRCH is the expected case once the group is gone.
	_ = killGroup(pid)
	<-shutdown.waitCh
	if !waitForGroupExit(pid) {
		err := fmt.Errorf("subprocess: process group %d did not exit after SIGKILL within %s", pid, groupShutdownTimeout)
		return outcome{result: Result{SignalCanceled: true, Duration: shutdown.elapsed}, err: err}
	}
	return canceled(shutdown.elapsed, ctx.Err())
}

func waitForGroupExit(pid int) bool {
	deadline := time.After(groupShutdownTimeout)
	for {
		select {
		case <-deadline:
			return false
		default:
		}
		if errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(groupPollInterval)
	}
}

func canceled(elapsed time.Duration, err error) outcome {
	return outcome{
		result: Result{SignalCanceled: true, Duration: elapsed},
		err:    err,
	}
}

func finalize(handle *processHandle, elapsed time.Duration) Result {
	return Result{ExitCode: handle.exitCode(), Duration: elapsed}
}

// exitError suppresses *exec.ExitError; nonzero exit is reported via Result.
// Other wait-time errors (broken pipe, etc.) propagate to the caller.
func exitError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func launchError(err error) *LaunchError {
	missing := errors.Is(err, exec.ErrNotFound) || isPathENOENT(err)
	return &LaunchError{NotFound: missing, Cause: err}
}

func isPathENOENT(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) && errors.Is(pathErr.Err, syscall.ENOENT)
}

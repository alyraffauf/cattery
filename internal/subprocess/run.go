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
	"time"
)

// gracePeriod is the SIGTERM-to-SIGKILL interval mandated by Section 10.4.
const gracePeriod = 5 * time.Second

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

// Run executes request synchronously, applying the Section 10.4 process-group
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

// outcome bundles a Result with its error so helpers stay under three params.
type outcome struct {
	result Result
	err    error
}

// groupShutdown bundles the inputs needed by awaitShutdown so it stays under
// the three-parameter limit.
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
	_ = terminateGroup(shutdown.handle.pid())
	select {
	case <-shutdown.waitCh:
		return canceled(shutdown.elapsed, ctx.Err())
	case <-time.After(gracePeriod):
	}
	_ = killGroup(shutdown.handle.pid())
	<-shutdown.waitCh
	return canceled(shutdown.elapsed, ctx.Err())
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
	return errors.As(err, &pathErr)
}

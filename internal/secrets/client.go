// Package secrets owns the external SOPS adapter: exact executable lookup,
// repository working directory, bounded stream capture, sanitized launch and
// exit diagnostics, and clearing of every buffer that may carry secret bytes.
// Plaintext and ciphertext cross the adapter as caller-owned byte slices, and
// no captured byte ever enters an error.
package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/subprocess"
)

// maxStderr is the Section 4.3 stderr capture bound. Excess stderr is drained
// and discarded so a chatty process can never block its pipe.
const maxStderr = 64 * 1024

// Client launches the external sops executable from a fixed repository
// working directory with an optional environment policy. An empty
// environment inherits the process environment; a non-nil empty slice passes
// an explicitly empty environment.
type Client struct {
	executable  string
	directory   string
	environment []string
}

// NewClient builds a client pinned to one executable, one repository working
// directory, and one environment policy.
func NewClient(executable string, directory string, environment []string) *Client {
	return &Client{executable: executable, directory: directory, environment: slices.Clone(environment)}
}

// SetDirectory rebinds the client to the repository root that Section 4.3
// SOPS invocations must run in. Bootstrap cannot know the repository, so the
// binding happens once the selected command resolves it; the client is used
// by one single-use application for exactly one repository.
func (client *Client) SetDirectory(directory string) {
	client.directory = directory
}

// Request describes one SOPS invocation. Operation and SourcePath appear only
// in sanitized diagnostics; Arguments are the exact sops arguments after the
// executable name; Stdin is written once to the /dev/stdin argument; and
// StdoutLimit bounds the captured stdout in bytes.
type Request struct {
	Operation   string
	SourcePath  string
	Arguments   []string
	Stdin       []byte
	StdoutLimit int
}

// Run executes sops synchronously with the repository working directory and
// environment policy, capturing stdout up to StdoutLimit and stderr up to
// maxStderr. On success the captured stdout buffer is returned and becomes
// caller-owned; on every other path it is zeroed and discarded, and the error
// carries only the operation, safe source path, and exit status.
func (client *Client) Run(ctx context.Context, request Request) ([]byte, error) {
	if request.StdoutLimit < 0 {
		return nil, failure.New(failure.Operational, "sops output limit must not be negative", nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := newBounded(request.StdoutLimit, cancel)
	stderr := newDrain(maxStderr)
	diag := diagnosis{operation: request.Operation, source: request.SourcePath}
	result, runError := subprocess.Run(ctx, subprocess.Request{
		Command:     executableArgs(client.executable, request.Arguments),
		Directory:   client.directory,
		Environment: client.environment,
		Stdin:       bytes.NewReader(request.Stdin),
		Stdout:      stdout,
		Stderr:      stderr,
	})
	stderr.clear()
	if err := classifyRun(stdout, runOutcome{result: result, err: runError}, diag); err != nil {
		stdout.clear()
		return nil, err
	}
	return stdout.buf, nil
}

// diagnosis carries the two context strings that may appear in sanitized
// errors, keeping error mapping under the parameter limit.
type diagnosis struct {
	operation string
	source    string
}

// runOutcome bundles the raw subprocess result with its error so classifyRun
// stays under the parameter limit.
type runOutcome struct {
	result subprocess.Result
	err    error
}

// classifyRun maps the raw subprocess outcome to a categorized failure. The
// stdout overflow flag wins over cancellation because the internal cancel is
// what stops the process group in that case.
func classifyRun(stdout *boundedCapture, outcome runOutcome, diag diagnosis) error {
	if stdout.overflow {
		return failure.New(failure.Operational, limitMessage(diag), nil)
	}
	if outcome.err == nil {
		return exitFailure(outcome.result, diag)
	}
	if outcome.result.SignalCanceled {
		return failure.New(failure.Operational, diag.operation+" "+diag.source+" cancelled", outcome.err)
	}
	var launch *subprocess.LaunchError
	if !errors.As(outcome.err, &launch) {
		return failure.New(failure.Operational, diag.operation+" "+diag.source+" failed", outcome.err)
	}
	if launch.NotFound {
		return failure.New(failure.Dependency, "sops executable not found", outcome.err)
	}
	return failure.New(failure.Operational, diag.operation+" "+diag.source+" launch failed", outcome.err)
}

// exitFailure reports a nonzero exit with the Section 4.3 diagnostic shape.
func exitFailure(result subprocess.Result, diag diagnosis) error {
	if result.ExitCode == 0 {
		return nil
	}
	return failure.New(failure.Operational, exitMessage(diag, result.ExitCode), nil)
}

func limitMessage(diag diagnosis) string {
	return "sops " + diag.operation + " " + diag.source + " output exceeded limit"
}

func exitMessage(diag diagnosis, code int) string {
	return fmt.Sprintf("sops %s %s exited with status %d", diag.operation, diag.source, code)
}

// executableArgs copies the executable and arguments into a fresh slice so a
// caller-owned arguments slice is never mutated.
func executableArgs(executable string, arguments []string) []string {
	command := []string{executable}
	return append(command, arguments...)
}

// boundedCapture keeps stdout up to a byte limit and cancels the process
// group once the limit is exceeded; later writes are discarded.
type boundedCapture struct {
	buf      []byte
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func newBounded(limit int, cancel context.CancelFunc) *boundedCapture {
	return &boundedCapture{limit: limit, cancel: cancel}
}

func (capture *boundedCapture) Write(p []byte) (int, error) {
	if capture.overflow {
		return len(p), nil
	}
	room := capture.limit - len(capture.buf)
	if len(p) <= room {
		capture.buf = append(capture.buf, p...)
		return len(p), nil
	}
	if room > 0 {
		capture.buf = append(capture.buf, p[:room]...)
	}
	capture.overflow = true
	capture.cancel()
	return len(p), nil
}

func (capture *boundedCapture) clear() {
	zeroBytes(capture.buf)
	capture.buf = nil
}

// drainCapture keeps the first limit bytes of stderr and consumes the rest so
// overflowing stderr is drained rather than blocking.
type drainCapture struct {
	buf   []byte
	limit int
}

func newDrain(limit int) *drainCapture {
	return &drainCapture{limit: limit}
}

func (capture *drainCapture) Write(p []byte) (int, error) {
	room := capture.limit - len(capture.buf)
	if room > 0 && len(p) > 0 {
		if len(p) < room {
			room = len(p)
		}
		capture.buf = append(capture.buf, p[:room]...)
	}
	return len(p), nil
}

func (capture *drainCapture) clear() {
	zeroBytes(capture.buf)
	capture.buf = nil
}

// zeroBytes best-effort clears one owned buffer; Go does not guarantee
// erasure, so this is exposure reduction rather than guaranteed removal.
func zeroBytes(data []byte) {
	for index := range data {
		data[index] = 0
	}
}

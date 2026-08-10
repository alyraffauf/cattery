// Package cli implements the Cobra adapters of `cattery` (PLAN.md Section
// 11): every command mechanically maps raw values into one injected
// application service call and renders its typed result. Cobra and x/term
// stay confined to this package.
package cli

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Streams bundles the process streams one application renders to.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// RuntimeInput carries the injected process-boundary values of one
// application.
type RuntimeInput struct {
	Streams     Streams
	WorkingDir  string
	Environment []string
	IsTerminal  func(fd int) bool
	SetVerbose  func(bool)
}

// Runtime carries the injected process-boundary values of one application:
// the streams, working directory, environment, terminal predicate, and the
// per-application verbosity callback (PLAN.md Section 12.1). Instances
// never share mutable state.
type Runtime struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	workingDir  string
	environment []string
	isTerminal  func(fd int) bool
	setVerbose  func(bool)
}

// NewRuntime freezes one runtime, defaulting the streams and the terminal
// predicate and copying the environment.
func NewRuntime(input RuntimeInput) Runtime {
	stdin, stdout, stderr := input.Streams.Stdin, input.Streams.Stdout, input.Streams.Stderr
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	isTerminal := input.IsTerminal
	if isTerminal == nil {
		isTerminal = term.IsTerminal
	}
	return Runtime{
		stdin:       stdin,
		stdout:      stdout,
		stderr:      stderr,
		workingDir:  input.WorkingDir,
		environment: append([]string(nil), input.Environment...),
		isTerminal:  isTerminal,
		setVerbose:  input.SetVerbose,
	}
}

// Stdin returns the injected standard input.
func (r Runtime) Stdin() io.Reader { return r.stdin }

// Stdout returns the injected standard output.
func (r Runtime) Stdout() io.Writer { return r.stdout }

// Stderr returns the injected standard error.
func (r Runtime) Stderr() io.Writer { return r.stderr }

// WorkingDir returns the injected current directory.
func (r Runtime) WorkingDir() string { return r.workingDir }

// Environment returns a defensive copy of the injected environment.
func (r Runtime) Environment() []string {
	return append([]string(nil), r.environment...)
}

// IsTerminal reports whether the given file descriptor is a terminal.
func (r Runtime) IsTerminal(fd int) bool {
	if r.isTerminal == nil {
		return false
	}
	return r.isTerminal(fd)
}

// SetVerbose applies the per-application verbosity level, if wired.
func (r Runtime) SetVerbose(verbose bool) {
	if r.setVerbose != nil {
		r.setVerbose(verbose)
	}
}

// EnvValue returns the value and presence of one environment entry.
func (r Runtime) EnvValue(name string) (string, bool) {
	prefix := name + "="
	for _, entry := range r.environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

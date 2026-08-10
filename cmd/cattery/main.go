// Package main is the sole process boundary of cattery: it creates the
// signal-aware cancellation causes, requests one application from
// bootstrap, calls the CLI executor, and owns the only production
// os.Exit (PLAN.md Section 12.1).
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alyraffauf/cattery/internal/bootstrap"
	"github.com/alyraffauf/cattery/internal/cli"
	"github.com/alyraffauf/cattery/internal/failure"
)

func main() {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go forwardSignals(ctx, cancel, signals)
	environment := os.Environ()
	stateHome := stateHomeOf(environment)
	application := bootstrap.Build(bootstrap.BuildInput{
		Streams:     cli.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
		WorkingDir:  workingDir(),
		Environment: environment,
		StateHome:   stateHome,
		Now:         time.Now,
		Protected:   []string{stateHome},
	})
	os.Exit(exitCode(ctx, application, os.Args[1:]))
}

// exitCode runs the application and maps an interrupted context to the
// Section 11.8 signal status.
func exitCode(ctx context.Context, application *cli.Application, args []string) int {
	code := cli.Execute(ctx, application, args)
	if ctx.Err() == nil {
		return code
	}
	return signalCode(context.Cause(ctx))
}

// signalCode maps one cancellation cause to 130 or 143.
func signalCode(cause error) int {
	var interruption *failure.Interruption
	if errors.As(cause, &interruption) && interruption.Signal == failure.Terminate {
		return 143
	}
	return 130
}

// forwardSignals cancels the process context with the typed interruption.
func forwardSignals(ctx context.Context, cancel context.CancelCauseFunc, signals <-chan os.Signal) {
	select {
	case <-ctx.Done():
		return
	case signal := <-signals:
		if signal == syscall.SIGTERM {
			cancel(failure.NewInterruption(failure.Terminate))
			return
		}
		cancel(failure.NewInterruption(failure.Interrupt))
	}
}

// stateHomeOf derives the XDG state directory for cattery.
func stateHomeOf(environment []string) string {
	base := envValue(environment, "XDG_STATE_HOME")
	if base == "" {
		home := envValue(environment, "HOME")
		if home == "" {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "cattery")
}

// envValue returns the value of one environment entry.
func envValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// workingDir returns the initial working directory.
func workingDir() string {
	directory, err := os.Getwd()
	if err != nil {
		return ""
	}
	return directory
}

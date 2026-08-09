// This file executes ordered hook descriptors with the PLAN.md Section 10.4
// runtime: repository-root working directory, inherited streams, a CATTERY_*
// environment appended after the inherited one, and process-group
// cancellation through internal/subprocess. The caller supplies one phase's
// sequence built with the Section 12.2 comparators; before hooks stop at the
// first failure, after hooks attempt every hook and aggregate failures.
// Dry-run and no-hooks execute nothing. No secret-specific environment or
// captured-stream policy enters hooks.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/subprocess"
)

// ExecuteInput carries the Section 10.4 runtime values of one hook phase.
// Empty stream fields inherit the caller's process streams.
type ExecuteInput struct {
	RepositoryRoot string
	HomePath       string
	Platform       string
	Phase          deployment.HookPhase
	Result         string
	DryRun         bool
	NoHooks        bool
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
}

// Execute runs the ordered hooks of one phase synchronously, each in its own
// process group with inherited streams and the Section 10.4 environment.
// Before hooks stop at the first failure; after hooks run every hook and
// return the joined failures. Cancellation returns the context error without
// starting further hooks. Dry-run and no-hooks suppress execution entirely.
func Execute(ctx context.Context, input ExecuteInput, ordered []deployment.Hook) error {
	if input.DryRun || input.NoHooks {
		return nil
	}
	var failures []error
	for _, hook := range ordered {
		err := runHook(ctx, input, hook)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		if input.Phase == deployment.HookBefore {
			return err
		}
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// runHook executes one hook, wrapping launch and cancellation errors and
// translating a nonzero exit into a hook failure error.
func runHook(ctx context.Context, input ExecuteInput, hook deployment.Hook) error {
	environment := append(os.Environ(), hookEnvironment(input, hook)...)
	result, err := subprocess.Run(ctx, subprocess.Request{
		Command:     []string{hook.AbsolutePath},
		Directory:   input.RepositoryRoot,
		Environment: environment,
		Stdin:       inheritReader(input.Stdin),
		Stdout:      inheritStdout(input.Stdout),
		Stderr:      inheritStderr(input.Stderr),
	})
	if err != nil {
		return fmt.Errorf("hooks: hook %q: %w", hook.AbsolutePath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("hooks: hook %q exited with status %d", hook.AbsolutePath, result.ExitCode)
	}
	return nil
}

// hookEnvironment builds the Section 10.4 CATTERY_* variables for one hook.
// os/exec keeps the later duplicate of a variable, so appending after the
// inherited environment guarantees the canonical values win.
func hookEnvironment(input ExecuteInput, hook deployment.Hook) []string {
	return []string{
		"CATTERY_REPO=" + input.RepositoryRoot,
		"CATTERY_HOME=" + input.HomePath,
		"CATTERY_PLATFORM=" + input.Platform,
		"CATTERY_PHASE=" + string(input.Phase),
		"CATTERY_GROUP=" + hook.Scope.Group,
		"CATTERY_RESULT=" + input.Result,
	}
}

// inheritReader defaults a nil stdin to the caller process stream.
func inheritReader(reader io.Reader) io.Reader {
	if reader == nil {
		return os.Stdin
	}
	return reader
}

// inheritStdout defaults a nil stdout to the caller process stream.
func inheritStdout(writer io.Writer) io.Writer {
	if writer == nil {
		return os.Stdout
	}
	return writer
}

// inheritStderr defaults a nil stderr to the caller process stream.
func inheritStderr(writer io.Writer) io.Writer {
	if writer == nil {
		return os.Stderr
	}
	return writer
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alyraffauf/cattery/internal/failure"
)

const (
	ExitSuccess    = 0
	ExitFailure    = 1
	ExitDifference = 2
	ExitHook       = 3
	ExitDependency = 4
	ExitInterrupt  = 130
	ExitTerminate  = 143
)

// Execute runs one application over the given arguments, writes one
// diagnostic on failure, and maps every joined category and signal to the
// exit status. Status constants stay here and os.Exit remains in
// the process entrypoint.
func Execute(ctx context.Context, application *Application, args []string) int {
	err := application.Execute(ctx, args)
	if err == nil {
		return ExitSuccess
	}
	if !kindIs(err, failure.Difference) || strings.Contains(err.Error(), "interactive terminal") {
		_, _ = fmt.Fprintf(application.root.ErrOrStderr(), "%s\n", userFacingError(err))
	}
	return exitStatus(err)
}

func userFacingError(err error) string {
	if !kindIs(err, failure.Difference) {
		return err.Error()
	}
	if strings.Contains(err.Error(), "interactive terminal") {
		return "Cannot continue without a decision in a non-interactive session. Re-run interactively, review with `cattery diff`, or use `--force` only when replacement is intended."
	}
	return "Changes are pending. Review them with `cattery status` or `cattery diff`, then run `cattery apply`."
}

// exitStatus maps one error to its exit status: an interruption outranks
// every joined category, then hook, dependency, and difference failures
// precede ordinary operational or validation errors.
func exitStatus(err error) int {
	var interruption *failure.Interruption
	if errors.As(err, &interruption) {
		if interruption.Signal == failure.Terminate {
			return ExitTerminate
		}
		return ExitInterrupt
	}
	kind, ok := failure.HasKind(err)
	if !ok {
		return ExitFailure
	}
	switch kind {
	case failure.Hook:
		return ExitHook
	case failure.Dependency:
		return ExitDependency
	case failure.Difference:
		return ExitDifference
	}
	return ExitFailure
}

package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/alyraffauf/cattery/internal/failure"
)

// Execute runs one application over the given arguments, writes one
// diagnostic on failure, and maps every joined category and signal to the
// Section 11.8 exit status. Numeric statuses exist only here and
// os.Exit stays in the process entrypoint.
func Execute(ctx context.Context, application *Application, args []string) int {
	err := application.Execute(ctx, args)
	if err == nil {
		return 0
	}
	_, _ = fmt.Fprintf(application.root.ErrOrStderr(), "%s\n", err)
	return exitStatus(err)
}

// exitStatus maps one error to its exit status: an interruption outranks
// every joined category, then hook, dependency, and difference failures
// precede ordinary operational or validation errors.
func exitStatus(err error) int {
	var interruption *failure.Interruption
	if errors.As(err, &interruption) {
		if interruption.Signal == failure.Terminate {
			return 143
		}
		return 130
	}
	kind, ok := failure.HasKind(err)
	if !ok {
		return 1
	}
	switch kind {
	case failure.Hook:
		return 3
	case failure.Dependency:
		return 4
	case failure.Difference:
		return 2
	}
	return 1
}

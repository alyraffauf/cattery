package cli

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/inspect"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

// StatusService is the one-method role the status adapter calls.
type StatusService interface {
	Status(context.Context, inspect.Request) (inspect.StatusResult, error)
}

// newStatusCommand declares the status syntax and mechanically maps the
// raw repository fields and group arguments into one status call. No
// classification or state import appears here.
func newStatusCommand(service StatusService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "status [GROUP ...]",
		Short: "Compare the repository against the deployed state",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			explicit := *options
			explicit.RepositorySet = explicit.RepositorySet || command.Flags().Changed("repo")
			request := inspect.Request{
				Repository: inspectRepository(explicit, runtime),
				Groups:     append([]string(nil), args...),
			}
			result, err := service.Status(command.Context(), request)
			if err != nil && !kindIs(err, failure.Difference) {
				return err
			}
			if renderErr := renderStatus(runtime.Stdout(), result); renderErr != nil {
				return renderErr
			}
			return err
		},
	}
	return command
}

// kindIs reports whether err carries the given failure kind.
func kindIs(err error, want failure.Kind) bool {
	kind, ok := failure.HasKind(err)
	return ok && kind == want
}

// inspectRepository copies the raw repository values into the inspection
// request shape.
func inspectRepository(options Options, runtime Runtime) inspect.RepositoryInput {
	env, envSet := runtime.EnvValue("CATTERY_REPO")
	return inspect.RepositoryInput{
		RawExplicit: options.Repository,
		ExplicitSet: options.RepositorySet,
		RawEnv:      env,
		EnvSet:      envSet,
		WorkingDir:  runtime.WorkingDir(),
	}
}

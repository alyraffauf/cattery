package cli

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/validate"
	"github.com/spf13/cobra"
)

// ValidateService is the one-method role the validate adapter calls.
type ValidateService interface {
	Validate(context.Context, validate.Request) (validate.Result, error)
}

// newValidateCommand declares the validate syntax and mechanically maps
// the raw repository fields and group arguments into one validate call
// (PLAN.md Section 11.2). No group or repository semantics appear here.
func newValidateCommand(service ValidateService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "validate [GROUP ...]",
		Short: "Validate the repository and report scope counts",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			explicit := *options
			explicit.RepositorySet = explicit.RepositorySet || command.Flags().Changed("repo")
			request := validate.Request{
				Repository: validateRepository(explicit, runtime),
				Groups:     append([]string(nil), args...),
			}
			result, err := service.Validate(command.Context(), request)
			if err != nil {
				return err
			}
			return renderValidate(runtime.Stdout(), result)
		},
	}
	return command
}

// validateRepository copies the raw repository values into the validate
// request shape.
func validateRepository(options Options, runtime Runtime) validate.RepositoryInput {
	env, envSet := runtime.EnvValue("CATTERY_REPO")
	return validate.RepositoryInput{
		RawExplicit: options.Repository,
		ExplicitSet: options.RepositorySet,
		RawEnv:      env,
		EnvSet:      envSet,
		WorkingDir:  runtime.WorkingDir(),
	}
}

// bindSharedFlags declares the shared repository and verbose flags over the
// option values; the composition root moves them to persistent flags.
func bindSharedFlags(command *cobra.Command, options *Options) {
	command.Flags().StringVarP(&options.Repository, "repo", "r", "", "repository path")
	command.Flags().BoolVarP(&options.Verbose, "verbose", "v", false, "verbose diagnostics")
}

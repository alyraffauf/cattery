package cli

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

// ApplyService is the one-method role the apply adapter calls.
type ApplyService interface {
	Apply(context.Context, apply.Request) (apply.Result, error)
}

// newApplyCommand declares the apply syntax and mechanically maps the raw
// repository fields, group arguments, and dry-run/noninteractive/no-hooks/skip-secrets
// policy into one apply call. The service carries
// the prompt resolver; no decision policy or hook order appears here.
func newApplyCommand(service ApplyService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "apply [GROUP ...]",
		Short: "Deploy the repository to HOME",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			request := applyRequest(command, applyInput{runtime: runtime, options: *options, groups: args})
			result, err := service.Apply(command.Context(), request)
			if err != nil && !kindIs(err, failure.Difference) && len(result.Items) == 0 {
				return err
			}
			if renderErr := renderApply(runtime.Stdout(), result, request.DryRun); renderErr != nil {
				return renderErr
			}
			return err
		},
	}
	command.Flags().Bool("dry-run", false, "preview changes without modifying files")
	command.Flags().Bool("non-interactive", false, "stop if a decision is required")
	command.Flags().Bool("no-hooks", false, "do not run trusted hooks")
	command.Flags().Bool("skip-secrets", false, "do not apply encrypted secret targets")
	command.Flags().Bool("force", false, "replace conflicting local files with repository versions")
	return command
}

// applyInput bundles the runtime, options, and group arguments of one
// apply mapping.
type applyInput struct {
	runtime Runtime
	options Options
	groups  []string
}

// applyRequest maps the raw values and policy flags into one request.
func applyRequest(command *cobra.Command, input applyInput) apply.Request {
	options := input.options
	options.RepositorySet = options.RepositorySet || command.Flags().Changed("repo")
	dryRun, _ := command.Flags().GetBool("dry-run")
	nonInteractive, _ := command.Flags().GetBool("non-interactive")
	noHooks, _ := command.Flags().GetBool("no-hooks")
	skipSecrets, _ := command.Flags().GetBool("skip-secrets")
	force, _ := command.Flags().GetBool("force")
	return apply.Request{
		Repository:     applyRepository(options, input.runtime),
		Groups:         append([]string(nil), input.groups...),
		DryRun:         dryRun,
		NonInteractive: nonInteractive,
		NoHooks:        noHooks,
		SkipSecrets:    skipSecrets,
		Force:          force,
	}
}

// applyRepository copies the raw repository values into the apply request
// shape.
func applyRepository(options Options, runtime Runtime) apply.RepositoryInput {
	env, envSet := runtime.EnvValue("CATTERY_REPO")
	return apply.RepositoryInput{
		RawExplicit: options.Repository,
		ExplicitSet: options.RepositorySet,
		RawEnv:      env,
		EnvSet:      envSet,
		WorkingDir:  runtime.WorkingDir(),
	}
}

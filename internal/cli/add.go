package cli

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/add"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

// AddService is the one-method role the add adapter calls.
type AddService interface {
	Add(context.Context, add.Request) (add.Result, error)
}

// newAddCommand declares the add syntax and mechanically maps the raw
// targets, repository fields, and exact group/platform/secret presence
// bits into one add call (PLAN.md Section 11.6). No ownership inference or
// filesystem access appears here.
func newAddCommand(service AddService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "add TARGET...",
		Short: "Adopt target files or directories into the repository",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			request := addRequest(command, addRequestInput{runtime: runtime, options: *options, targets: args})
			result, err := service.Add(command.Context(), request)
			if err != nil && !kindIs(err, failure.Difference) && len(result.Items) == 0 {
				return err
			}
			if renderErr := renderAdd(runtime.Stdout(), result); renderErr != nil {
				return renderErr
			}
			return err
		},
	}
	command.Flags().String("group", "", "repository group")
	command.Flags().String("platform", "", "platform layer")
	command.Flags().Bool("secret", false, "adopt as a SOPS secret")
	command.Flags().Bool("dry-run", false, "show the plan without writing")
	return command
}

// addRequestInput bundles the runtime, options, and raw targets of one
// add mapping.
type addRequestInput struct {
	runtime Runtime
	options Options
	targets []string
}

// addRequest maps the raw values and exact presence bits into one request.
func addRequest(command *cobra.Command, input addRequestInput) add.Request {
	options := input.options
	options.RepositorySet = options.RepositorySet || command.Flags().Changed("repo")
	group, _ := command.Flags().GetString("group")
	platform, _ := command.Flags().GetString("platform")
	secretValue, _ := command.Flags().GetBool("secret")
	dryRunValue, _ := command.Flags().GetBool("dry-run")
	return add.Request{
		Repository:  addRepository(options, input.runtime),
		Targets:     append([]string(nil), input.targets...),
		Group:       group,
		GroupSet:    command.Flags().Changed("group"),
		Platform:    platform,
		PlatformSet: command.Flags().Changed("platform"),
		Secret:      secretValue,
		SecretSet:   command.Flags().Changed("secret"),
		DryRun:      dryRunValue,
	}
}

// addRepository copies the raw repository values into the add request
// shape.
func addRepository(options Options, runtime Runtime) add.RepositoryInput {
	env, envSet := runtime.EnvValue("CATTERY_REPO")
	return add.RepositoryInput{
		RawExplicit: options.Repository,
		ExplicitSet: options.RepositorySet,
		RawEnv:      env,
		EnvSet:      envSet,
		WorkingDir:  runtime.WorkingDir(),
	}
}

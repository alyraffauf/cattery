package cli

import (
	"context"
	"fmt"

	"github.com/alyraffauf/cattery/internal/application/forget"
	"github.com/spf13/cobra"
)

// ForgetService is the one-method role the forget adapter calls.
type ForgetService interface {
	Forget(context.Context, forget.Request) (forget.Result, error)
}

func newForgetCommand(service ForgetService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "forget DIRECTORY",
		Short: "Stop managing a directory without deleting its files",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			dryRun, _ := command.Flags().GetBool("dry-run")
			yes, _ := command.Flags().GetBool("yes")
			result, err := service.Forget(command.Context(), forget.Request{
				Repository: forgetRepository(*options, command, runtime), Directory: arguments[0], DryRun: dryRun, Yes: yes,
			})
			if renderErr := renderForget(runtime.Stdout(), result); renderErr != nil {
				return renderErr
			}
			return err
		},
	}
	command.Flags().Bool("dry-run", false, "show the sources that would be removed")
	command.Flags().Bool("yes", false, "remove repository sources without a prompt")
	return command
}

func forgetRepository(options Options, command *cobra.Command, runtime Runtime) forget.RepositoryInput {
	options.RepositorySet = options.RepositorySet || command.Flags().Changed("repo")
	environment, environmentSet := runtime.EnvValue("CATTERY_REPO")
	return forget.RepositoryInput{
		RawExplicit: options.Repository, ExplicitSet: options.RepositorySet,
		RawEnv: environment, EnvSet: environmentSet, WorkingDir: runtime.WorkingDir(),
	}
}

func renderForget(writer interface{ Write([]byte) (int, error) }, result forget.Result) error {
	for _, item := range result.Items {
		if _, err := fmt.Fprintf(writer, "$HOME/%s %s %s\n", displayPath(item.Target), item.Status, displayPath(item.Source)); err != nil {
			return err
		}
	}
	return nil
}

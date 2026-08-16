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
	command.Flags().Bool("dry-run", false, "preview repository sources that would stop being managed")
	command.Flags().Bool("yes", false, "stop managing repository sources without a prompt")
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
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(writer, "Nothing is managed in that directory.")
		return err
	}
	if _, err := fmt.Fprintf(writer, "Ready to stop managing — %d %s\n", len(result.Items), pluralize(len(result.Items), "file")); err != nil {
		return err
	}
	for _, item := range result.Items {
		if _, err := fmt.Fprintf(writer, "\n  Forget   ~/%s\n           Repository source: %s\n", displayPath(item.Target), displayPath(item.Source)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "\nFiles in your home directory are left untouched.")
	return err
}

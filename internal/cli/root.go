package cli

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

// Dependencies carries one explicitly named one-method service field per
// operational command.
type Dependencies struct {
	Initialize InitializeService
	Validate   ValidateService
	Status     StatusService
	Diff       DiffService
	Add        AddService
	Forget     ForgetService
	Secrets    SecretsService
	Apply      ApplyService
}

// Application is the opaque single-use Cobra root. The embedded command is
// unexported so bootstrap and main cannot inspect or mutate Cobra state.
type Application struct {
	root     *cobra.Command
	executed bool
}

// NewApplication builds one opaque application over the dependencies and
// runtime: operational commands, persistent repository and verbose
// flags, injected streams, no root Version or completion command, and no
// OnInitialize hook. Construction touches no backend.
func NewApplication(dependencies Dependencies, runtime Runtime) *Application {
	options := &Options{}
	root := &cobra.Command{
		Use:           "cattery",
		Short:         "Safely manage dotfiles from one repository",
		Long:          "Safely manage dotfiles from one repository.\n\nStart here:\n  cattery init PATH      Register a repository\n  cattery status         Review pending changes\n  cattery apply          Apply reviewed changes",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(command *cobra.Command, args []string) error {
			runtime.SetVerbose(options.Verbose)
			return nil
		},
	}
	root.PersistentFlags().StringVarP(&options.Repository, "repo", "r", "", "repository path")
	root.PersistentFlags().BoolVarP(&options.Verbose, "verbose", "v", false, "show diagnostic details")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(runtime.Stdin())
	root.SetOut(runtime.Stdout())
	root.SetErr(runtime.Stderr())
	root.SetHelpTemplate(`{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}
{{end}}
Usage:
  {{.UseLine}}

{{if .HasAvailableSubCommands}}Commands:
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
Options:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .HasAvailableInheritedFlags}}
Global options:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}
Run "{{.CommandPath}} [command] --help" for command details.
`)
	root.AddCommand(
		newInitCommand(dependencies.Initialize, runtime),
		newValidateCommand(dependencies.Validate, runtime, options),
		newVersionCommand(runtime),
		newStatusCommand(dependencies.Status, runtime, options),
		newDiffCommand(dependencies.Diff, runtime, options),
		newAddCommand(dependencies.Add, runtime, options),
		newForgetCommand(dependencies.Forget, runtime, options),
		newSecretsCommand(dependencies.Secrets, runtime, options),
		newApplyCommand(dependencies.Apply, runtime, options),
	)
	return &Application{root: root}
}

// Execute runs the application exactly once over the given arguments and
// process context; a second use is rejected.
func (a *Application) Execute(ctx context.Context, args []string) error {
	if a.executed {
		return failure.New(failure.Operational, "cli: application already executed", nil)
	}
	a.executed = true
	a.root.SetContext(ctx)
	a.root.SetArgs(args)
	return a.root.Execute()
}

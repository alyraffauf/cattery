package cli

import (
	"github.com/spf13/cobra"
)

// Dependencies carries one explicitly named one-method service field per
// operational command (PLAN.md Section 12.1).
type Dependencies struct {
	Initialize InitializeService
	Validate   ValidateService
	Version    VersionService
	Status     StatusService
	Diff       DiffService
	Add        AddService
	Apply      ApplyService
}

// Application is the opaque single-use Cobra root. The embedded command is
// unexported so bootstrap and main cannot inspect or mutate Cobra state.
type Application struct {
	root *cobra.Command
}

// NewApplication builds one opaque application over the dependencies and
// runtime: seven operational commands, persistent repository and verbose
// flags, injected streams, no root Version, no completion or suggestions,
// and no OnInitialize hook. Construction touches no backend.
func NewApplication(dependencies Dependencies, runtime Runtime) *Application {
	options := &Options{}
	root := &cobra.Command{
		Use:           "cattery",
		Short:         "Declarative dotfile deployment",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(command *cobra.Command, args []string) error {
			runtime.SetVerbose(options.Verbose)
			return nil
		},
	}
	root.PersistentFlags().StringVarP(&options.Repository, "repo", "r", "", "repository path")
	root.PersistentFlags().BoolVarP(&options.Verbose, "verbose", "v", false, "verbose diagnostics")
	root.CompletionOptions.DisableDefaultCmd = true
	root.DisableSuggestions = true
	root.SetIn(runtime.Stdin())
	root.SetOut(runtime.Stdout())
	root.SetErr(runtime.Stderr())
	root.AddCommand(
		newInitCommand(dependencies.Initialize, runtime),
		newValidateCommand(dependencies.Validate, runtime, options),
		newVersionCommand(dependencies.Version, runtime),
		newStatusCommand(dependencies.Status, runtime, options),
		newDiffCommand(dependencies.Diff, runtime, options),
		newAddCommand(dependencies.Add, runtime, options),
		newApplyCommand(dependencies.Apply, runtime, options),
	)
	return &Application{root: root}
}

// Execute runs the application once over the given arguments.
func (a *Application) Execute(args []string) error {
	a.root.SetArgs(args)
	return a.root.Execute()
}

package cli

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/inspect"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/spf13/cobra"
)

// DiffService is the one-method role the diff adapter calls.
type DiffService interface {
	Diff(context.Context, inspect.Request) (inspect.DiffResult, error)
}

// newDiffCommand declares the diff syntax and mechanically maps the raw
// repository fields and group arguments into one diff call. No diff calculation
// or formatter import appears here.
func newDiffCommand(service DiffService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "diff [GROUP ...]",
		Short: "Show secret-safe differences",
		Args:  cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, args []string) error {
			explicit := *options
			explicit.RepositorySet = explicit.RepositorySet || command.Flags().Changed("repo")
			request := inspect.Request{
				Repository: inspectRepository(explicit, runtime),
				Groups:     append([]string(nil), args...),
			}
			result, err := service.Diff(command.Context(), request)
			if err != nil && !kindIs(err, failure.Difference) {
				return err
			}
			if renderErr := renderDiff(runtime.Stdout(), result); renderErr != nil {
				return renderErr
			}
			return err
		},
	}
	return command
}

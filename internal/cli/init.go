package cli

import (
	"context"
	"fmt"

	"github.com/alyraffauf/cattery/internal/application/initialize"
	"github.com/spf13/cobra"
)

// InitializeService is the one-method role the init adapter calls.
type InitializeService interface {
	Initialize(context.Context, initialize.Request) (initialize.Result, error)
}

// newInitCommand declares the init syntax and mechanically maps one raw
// path or the injected working directory into a single initialize call
// (PLAN.md Section 11.1). No path resolution, registration, or backend
// import appears here.
func newInitCommand(service InitializeService, runtime Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "init [PATH]",
		Short: "Initialize a cattery repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path := runtime.WorkingDir()
			if len(args) == 1 {
				path = args[0]
			}
			result, err := service.Initialize(command.Context(), initialize.Request{Path: path})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(runtime.Stdout(), "initialized %s\n", result.Repository.RootPath)
			return err
		},
	}
	return command
}

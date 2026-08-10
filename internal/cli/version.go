package cli

import (
	"fmt"

	"github.com/alyraffauf/cattery/internal/application/version"
	"github.com/spf13/cobra"
)

// VersionService is the one-method role the version adapter calls.
type VersionService interface {
	Version() version.Result
}

// newVersionCommand declares only the version subcommand and renders the
// typed build fields in the exact Section 11.7 single-line format, without
// touching the Cobra root Version field.
func newVersionCommand(service VersionService, runtime Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			result := service.Version()
			_, err := fmt.Fprintf(runtime.Stdout(),
				"cattery %s commit=%s built=%s go=%s target=%s/%s\n",
				result.Version, result.Commit, result.Timestamp,
				result.GoVersion, result.OperatingSystem, result.Architecture)
			return err
		},
	}
	return command
}

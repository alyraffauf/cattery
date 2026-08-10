package cli

import (
	"fmt"

	"github.com/alyraffauf/cattery/internal/buildinfo"
	"github.com/spf13/cobra"
)

// newVersionCommand declares only the version subcommand and renders the
// typed build fields in the exact single-line format, without
// touching the Cobra root Version field.
func newVersionCommand(runtime Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			result := buildinfo.Current()
			_, err := fmt.Fprintf(runtime.Stdout(),
				"cattery %s commit=%s built=%s go=%s target=%s/%s\n",
				result.Version, result.Commit, result.Timestamp,
				result.GoVersion, result.OperatingSystem, result.Architecture)
			return err
		},
	}
	return command
}

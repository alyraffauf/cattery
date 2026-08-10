package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/alyraffauf/cattery/internal/application/inspect"
)

// renderStatus writes one line per pending record and the summary line of
// one status result (PLAN.md Sections 11.3 and 11.9).
func renderStatus(writer io.Writer, result inspect.StatusResult) error {
	for _, record := range result.Records() {
		if _, err := fmt.Fprintf(writer, "$HOME/%s %s %s\n",
			displayPath(record.TargetPath()), record.Kind(), record.Action()); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(writer, "summary files=%d aliases=%d retired=%d converged=%t\n",
		result.Files(), result.Aliases(), result.Retired(), result.Converged())
	return err
}

// displayPath escapes control characters and ambiguous whitespace with the
// stable Go-style quoted representation so a filename can never inject
// terminal lines.
func displayPath(path string) string {
	if needsQuoting(path) {
		return strconv.Quote(path)
	}
	return path
}

// needsQuoting reports whether a path carries control characters or
// ambiguous whitespace.
func needsQuoting(path string) bool {
	for _, character := range path {
		if character < 0x20 || character == 0x7f || character == ' ' {
			return true
		}
	}
	return false
}

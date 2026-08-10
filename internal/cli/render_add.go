package cli

import (
	"fmt"
	"io"

	"github.com/alyraffauf/cattery/internal/application/add"
)

// renderAdd writes one line per item record and the summary line of one
// add result (PLAN.md Section 11.6).
func renderAdd(writer io.Writer, result add.Result) error {
	for _, item := range result.Items {
		if _, err := fmt.Fprintf(writer, "$HOME/%s %s %s\n",
			displayPath(item.Target), item.Status, displayPath(item.Source)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(writer, "summary planned=%d completed=%d partial=%d\n",
		result.Summary.Planned, result.Summary.Completed, result.Summary.Partial)
	return err
}

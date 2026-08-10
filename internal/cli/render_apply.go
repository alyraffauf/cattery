package cli

import (
	"fmt"
	"io"

	"github.com/alyraffauf/cattery/internal/application/apply"
)

// renderApply writes one line per item record and the summary line of one
// apply result (PLAN.md Section 11.5).
func renderApply(writer io.Writer, result apply.Result) error {
	for _, item := range result.Items {
		if _, err := fmt.Fprintf(writer, "$HOME/%s %s %s\n",
			displayPath(item.TargetPath), item.Status, item.Kind); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(writer, "summary planned=%d completed=%d partial=%d\n",
		result.Summary.Planned, result.Summary.Completed, result.Summary.Partial)
	return err
}

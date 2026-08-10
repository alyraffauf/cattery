package cli

import (
	"fmt"
	"io"

	"github.com/alyraffauf/cattery/internal/application/validate"
)

// renderValidate writes the two deterministic platform count lines of one
// validate result (PLAN.md Section 11.2).
func renderValidate(writer io.Writer, result validate.Result) error {
	for _, record := range result.Platforms {
		if _, err := fmt.Fprintf(writer, "%s files=%d secrets=%d aliases=%d groups=%d\n",
			record.Platform, record.Files, record.Secrets, record.Aliases, record.Groups); err != nil {
			return err
		}
	}
	return nil
}

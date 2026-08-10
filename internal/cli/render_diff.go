package cli

import (
	"fmt"
	"io"

	"github.com/alyraffauf/cattery/internal/application/inspect"
)

// renderDiff writes one line per tagged safe record plus the summary line
// of one diff result (PLAN.md Section 11.4). Secret records render the
// marker only, with zero content, size, or hash fields.
func renderDiff(writer io.Writer, result inspect.DiffResult) error {
	for _, record := range result.Records() {
		if err := renderDiffRecord(writer, record); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(writer, "summary files=%d aliases=%d retired=%d converged=%t\n",
		result.Files(), result.Aliases(), result.Retired(), result.Converged())
	return err
}

// renderDiffRecord writes one safe record line and its payload.
func renderDiffRecord(writer io.Writer, record inspect.DiffRecord) error {
	path := "$HOME/" + displayPath(record.TargetPath())
	switch inspect.DiffTagName(record) {
	case "text":
		return renderTextDiff(writer, path, record)
	case "binary":
		_, err := fmt.Fprintf(writer, "%s %s binary size=%d/%d\n",
			path, record.Kind(), record.SourceSize(), record.TargetSize())
		return err
	case "secret":
		_, err := fmt.Fprintf(writer, "%s %s secret\n", path, record.Kind())
		return err
	}
	_, err := fmt.Fprintf(writer, "%s %s %s\n", path, record.Kind(), record.Action())
	return err
}

// renderTextDiff writes the record line, the label line, and the diff
// lines of one printable text difference.
func renderTextDiff(writer io.Writer, path string, record inspect.DiffRecord) error {
	if _, err := fmt.Fprintf(writer, "%s %s %s\n", path, record.Kind(), record.Action()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "%s\n", record.SourceLabel()); err != nil {
		return err
	}
	_, err := fmt.Fprintf(writer, "%s", record.Lines())
	return err
}

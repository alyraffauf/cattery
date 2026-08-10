package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/alyraffauf/cattery/internal/application/add"
	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/application/inspect"
	"github.com/alyraffauf/cattery/internal/application/validate"
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

// displayPath escapes control characters and ambiguous whitespace with the
// stable Go-style quoted representation so a filename can never inject
// terminal lines.
func displayPath(path string) string {
	if needsQuoting(path) {
		return strconv.Quote(path)
	}
	return path
}

func needsQuoting(path string) bool {
	for _, character := range path {
		if character < 0x20 || character == 0x7f || character == ' ' {
			return true
		}
	}
	return false
}

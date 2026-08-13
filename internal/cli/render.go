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

func renderAdd(writer io.Writer, result add.Result) error {
	if result.Summary.Planned > 0 {
		if err := renderHeading(writer, "Ready to add", result.Summary.Planned, "change"); err != nil {
			return err
		}
	} else {
		if err := renderHeading(writer, "Added", result.Summary.Completed, "change"); err != nil {
			return err
		}
	}
	for _, item := range result.Items {
		if err := renderAction(writer, addAction(item.Status), item.Target, "from "+displayPath(item.Source)); err != nil {
			return err
		}
	}
	if result.Summary.Planned > 0 {
		return renderFooter(writer, "No files were changed.")
	}
	return renderOutcomeFooter(writer, result.Summary.Completed, result.Summary.Partial, "added")
}

func renderApply(writer io.Writer, result apply.Result, dryRun bool) error {
	if len(result.Items) == 0 {
		return renderFooter(writer, "Nothing to apply.")
	}
	if dryRun {
		if err := renderHeading(writer, "Ready to apply", result.Summary.Planned, "change"); err != nil {
			return err
		}
	} else if result.Summary.Partial > 0 {
		if err := renderHeading(writer, "Applied with unresolved items", result.Summary.Completed, "change"); err != nil {
			return err
		}
	} else {
		if err := renderHeading(writer, "Applied", result.Summary.Completed, "change"); err != nil {
			return err
		}
	}
	for _, item := range result.Items {
		if err := renderAction(writer, applyAction(item.Kind, item.Status), item.TargetPath, applyContext(item.Status, dryRun)); err != nil {
			return err
		}
	}
	if dryRun {
		return renderFooter(writer, "No files were changed.\nNext: run `cattery apply` to make these changes.")
	}
	return renderOutcomeFooter(writer, result.Summary.Completed, result.Summary.Partial, "applied")
}

func renderValidate(writer io.Writer, result validate.Result) error {
	if _, err := fmt.Fprintln(writer, "Repository is valid."); err != nil {
		return err
	}
	for _, record := range result.Platforms {
		if _, err := fmt.Fprintf(writer, "\n  %s\n    Files: %d  Secrets: %d  Links: %d  Groups: %d\n", record.Platform, record.Files, record.Secrets, record.Aliases, record.Groups); err != nil {
			return err
		}
	}
	return nil
}

func renderStatus(writer io.Writer, result inspect.StatusResult) error {
	if result.Converged() {
		return renderFooter(writer, "Everything is up to date.")
	}
	if err := renderHeading(writer, "Changes needed", len(result.Records()), "change"); err != nil {
		return err
	}
	for _, record := range result.Records() {
		if err := renderAction(writer, statusAction(record.Action(), record.Kind()), record.TargetPath(), statusContext(record.Kind())); err != nil {
			return err
		}
	}
	return renderFooter(writer, "No files were changed.\nNext: run `cattery apply` to make these changes.")
}

func renderDiff(writer io.Writer, result inspect.DiffResult) error {
	if result.Converged() {
		return renderFooter(writer, "Everything is up to date.")
	}
	if err := renderHeading(writer, "Changes to review", len(result.Records()), "change"); err != nil {
		return err
	}
	for _, record := range result.Records() {
		if err := renderDiffRecord(writer, record); err != nil {
			return err
		}
	}
	return renderFooter(writer, "Next: run `cattery apply` to make these changes.")
}

func renderDiffRecord(writer io.Writer, record inspect.DiffRecord) error {
	if err := renderAction(writer, statusAction(record.Action(), record.Kind()), record.TargetPath(), diffContext(record)); err != nil {
		return err
	}
	if inspect.DiffTagName(record) != "text" {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "\n%s", record.Lines()); err != nil {
		return err
	}
	return nil
}

func renderHeading(writer io.Writer, title string, count int, noun string) error {
	_, err := fmt.Fprintf(writer, "%s — %d %s\n", title, count, pluralize(count, noun))
	return err
}

func renderAction(writer io.Writer, action, target, context string) error {
	if _, err := fmt.Fprintf(writer, "\n  %-8s ~/%s\n", action, displayPath(target)); err != nil {
		return err
	}
	if context == "" {
		return nil
	}
	_, err := fmt.Fprintf(writer, "           %s\n", context)
	return err
}

func renderFooter(writer io.Writer, message string) error {
	_, err := fmt.Fprintf(writer, "\n%s\n", message)
	return err
}

func renderOutcomeFooter(writer io.Writer, completed, partial int, verb string) error {
	if partial == 0 {
		return renderFooter(writer, fmt.Sprintf("%d %s %s.", completed, pluralize(completed, "change"), verb))
	}
	return renderFooter(writer, fmt.Sprintf("%d %s %s; %d %s need attention.", completed, pluralize(completed, "change"), verb, partial, pluralize(partial, "item")))
}

func statusAction(action string, kind inspect.StatusKind) string {
	if kind == inspect.StatusKindRetired {
		return "Forget"
	}
	switch action {
	case "create-target", "create-alias":
		return "Create"
	case "write-source-to-target", "correct-mode", "establish-baseline":
		return "Update"
	case "needs-decision", "replace-alias":
		return "Review"
	case "retire-state", "retire-alias-state":
		return "Forget"
	case "verify-alias":
		return "Link"
	}
	if kind == inspect.StatusKindAlias {
		return "Link"
	}
	return "Update"
}

func addAction(status add.ItemStatus) string {
	if status == add.StatusPlanned {
		return "Add"
	}
	if status == add.StatusPartial {
		return "Skipped"
	}
	return "Added"
}

func applyAction(kind apply.ActionKind, status apply.ItemStatus) string {
	if status == apply.StatusPartial {
		return "Skipped"
	}
	switch kind {
	case apply.ActionKindWriteSource:
		return "Update"
	case apply.ActionKindReplaceFile:
		return "Replace"
	case apply.ActionKindRealizeAlias, apply.ActionKindTransitionToAlias:
		return "Link"
	case apply.ActionKindRetireFile, apply.ActionKindRetireAlias:
		return "Forget"
	case apply.ActionKindTransitionToFile:
		return "Replace"
	}
	return "Update"
}

func applyContext(status apply.ItemStatus, dryRun bool) string {
	if status == apply.StatusPartial {
		return "This item was not changed."
	}
	if dryRun {
		return "This change has not been applied."
	}
	return ""
}

func statusContext(kind inspect.StatusKind) string {
	if kind == inspect.StatusKindRetired {
		return "Its repository source is gone; the file in your home directory is left untouched."
	}
	return ""
}

func diffContext(record inspect.DiffRecord) string {
	switch inspect.DiffTagName(record) {
	case "binary":
		return fmt.Sprintf("Binary content differs (%d bytes in repository, %d bytes locally).", record.SourceSize(), record.TargetSize())
	case "secret":
		return "Encrypted secret content differs; its plaintext is not shown."
	}
	return statusContext(record.Kind())
}

func pluralize(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
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

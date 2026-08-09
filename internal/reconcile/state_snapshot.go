package reconcile

import (
	"fmt"
	"io/fs"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/state"
)

// StateRows bundles the persisted rows of one canonical repository pair so a
// snapshot can be converted from them without touching the filesystem or the
// store again.
type StateRows struct {
	RepositoryRoot string
	HomePath       string
	Files          []state.FileBaseline
	Aliases        []state.AliasBaseline
}

// NewStateSnapshot converts the persisted file and alias rows of one
// canonical repository pair into immutable evaluation records. Active and
// retired rows are preserved verbatim so state-only and deleted scopes stay
// visible; any path active in both tables is cross-table corruption and
// fails the snapshot. No filesystem access or state mutation occurs.
func NewStateSnapshot(rows StateRows) (StateSnapshot, error) {
	if rows.RepositoryRoot == "" || rows.HomePath == "" {
		return StateSnapshot{}, fmt.Errorf("reconcile: state snapshot requires canonical repository and home paths")
	}
	if err := rejectDualActive(rows.Files, rows.Aliases); err != nil {
		return StateSnapshot{}, err
	}
	fileRecords := make([]FileState, len(rows.Files))
	for index, row := range rows.Files {
		record, err := convertFileRow(row)
		if err != nil {
			return StateSnapshot{}, err
		}
		fileRecords[index] = record
	}
	aliasRecords := make([]AliasState, len(rows.Aliases))
	for index, row := range rows.Aliases {
		record, err := convertAliasRow(row)
		if err != nil {
			return StateSnapshot{}, err
		}
		aliasRecords[index] = record
	}
	return StateSnapshot{
		RepositoryRoot: rows.RepositoryRoot,
		HomePath:       rows.HomePath,
		Files:          fileRecords,
		Aliases:        aliasRecords,
	}, nil
}

// rejectDualActive rejects a path that is active in both the file and alias
// tables of one repository pair, per PLAN.md Section 8.4.
func rejectDualActive(files []state.FileBaseline, aliases []state.AliasBaseline) error {
	active := make(map[string]bool, len(files))
	for _, row := range files {
		if row.Status == state.StatusActive {
			active[row.TargetPath] = true
		}
	}
	for _, row := range aliases {
		if row.Status == state.StatusActive && active[row.AliasPath] {
			return fmt.Errorf("reconcile: path %q is active in both representations", row.AliasPath)
		}
	}
	return nil
}

// convertFileRow projects one file baseline into its immutable evaluation
// record, cloning the retirement timestamp and validating every field.
func convertFileRow(row state.FileBaseline) (FileState, error) {
	if err := validateFileRow(row); err != nil {
		return FileState{}, err
	}
	return FileState{
		TargetPath:      row.TargetPath,
		GroupName:       row.GroupName,
		SourcePath:      row.SourcePath,
		SourceKind:      row.SourceKind,
		Layer:           row.Layer,
		BaselineContent: row.BaselineContentHash,
		BaselineSource:  row.BaselineSourceHash,
		ExecutableBits:  fs.FileMode(row.ExecutableBits),
		Active:          row.Status == state.StatusActive,
		RetiredAt:       state.CloneTimestamp(row.RetiredAt),
	}, nil
}

// validateFileRow rejects a row that could not have been stored faithfully.
func validateFileRow(row state.FileBaseline) error {
	if err := validateFilePaths(row); err != nil {
		return err
	}
	if row.BaselineContentHash == (deployment.Digest{}) || row.BaselineSourceHash == (deployment.Digest{}) {
		return fmt.Errorf("reconcile: file row %q has an unset digest", row.TargetPath)
	}
	if row.ExecutableBits > 0o777 {
		return fmt.Errorf("reconcile: file row %q has invalid executable bits %o", row.TargetPath, row.ExecutableBits)
	}
	if !row.Status.Valid() {
		return fmt.Errorf("reconcile: file row %q has invalid status %q", row.TargetPath, row.Status)
	}
	return nil
}

// validateFilePaths rejects malformed path and enum fields of a file row.
func validateFilePaths(row state.FileBaseline) error {
	if !state.IsSlashRelative(row.TargetPath) {
		return fmt.Errorf("reconcile: file row target %q is not a slash-relative path", row.TargetPath)
	}
	if !state.IsSlashRelative(row.SourcePath) {
		return fmt.Errorf("reconcile: file row source %q is not a slash-relative path", row.SourcePath)
	}
	if row.GroupName != "" && !state.IsSlashRelative(row.GroupName) {
		return fmt.Errorf("reconcile: file row group %q is not a slash-relative path", row.GroupName)
	}
	if !row.SourceKind.Valid() {
		return fmt.Errorf("reconcile: file row has invalid source kind %q", row.SourceKind)
	}
	if !row.Layer.Valid() {
		return fmt.Errorf("reconcile: file row has invalid layer %q", row.Layer)
	}
	return nil
}

// convertAliasRow projects one alias baseline into its immutable evaluation
// record, cloning the retirement timestamp and validating every field.
func convertAliasRow(row state.AliasBaseline) (AliasState, error) {
	if err := validateAliasRow(row); err != nil {
		return AliasState{}, err
	}
	return AliasState{
		AliasPath:           row.AliasPath,
		CanonicalTargetPath: row.CanonicalTargetPath,
		GroupName:           row.GroupName,
		Layer:               row.Layer,
		Active:              row.Status == state.StatusActive,
		RetiredAt:           state.CloneTimestamp(row.RetiredAt),
	}, nil
}

// validateAliasRow rejects a row that could not have been stored faithfully.
func validateAliasRow(row state.AliasBaseline) error {
	if !state.IsSlashRelative(row.AliasPath) {
		return fmt.Errorf("reconcile: alias row path %q is not a slash-relative path", row.AliasPath)
	}
	if !state.IsSlashRelative(row.CanonicalTargetPath) {
		return fmt.Errorf("reconcile: alias row target %q is not a slash-relative path", row.CanonicalTargetPath)
	}
	if row.GroupName != "" && !state.IsSlashRelative(row.GroupName) {
		return fmt.Errorf("reconcile: alias row group %q is not a slash-relative path", row.GroupName)
	}
	if !row.Layer.Valid() {
		return fmt.Errorf("reconcile: alias row has invalid layer %q", row.Layer)
	}
	if !row.Status.Valid() {
		return fmt.Errorf("reconcile: alias row %q has invalid status %q", row.AliasPath, row.Status)
	}
	return nil
}

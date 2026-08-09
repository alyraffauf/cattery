package reconcile

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
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
		repositoryRoot: rows.RepositoryRoot,
		homePath:       rows.HomePath,
		files:          fileRecords,
		aliases:        aliasRecords,
	}, nil
}

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

func convertFileRow(row state.FileBaseline) (FileState, error) {
	if err := validateFileRow(row); err != nil {
		return FileState{}, err
	}
	return FileState{
		targetPath:      row.TargetPath,
		groupName:       row.GroupName,
		sourcePath:      row.SourcePath,
		sourceKind:      row.SourceKind,
		layer:           row.Layer,
		baselineContent: row.BaselineContentHash,
		baselineSource:  row.BaselineSourceHash,
		executableBits:  fs.FileMode(row.ExecutableBits),
		active:          row.Status == state.StatusActive,
		retiredAt:       state.CloneTimestamp(row.RetiredAt),
	}, nil
}

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
	if err := validateRetirement(row.Status, row.RetiredAt); err != nil {
		return fmt.Errorf("reconcile: file row %q %w", row.TargetPath, err)
	}
	return nil
}

func validateFilePaths(row state.FileBaseline) error {
	if !state.IsSlashRelative(row.TargetPath) {
		return fmt.Errorf("reconcile: file row target %q is not a slash-relative path", row.TargetPath)
	}
	if !state.IsSlashRelative(row.SourcePath) {
		return fmt.Errorf("reconcile: file row source %q is not a slash-relative path", row.SourcePath)
	}
	if row.GroupName != "" {
		if err := pathsafe.GroupName(row.GroupName); err != nil {
			return fmt.Errorf("reconcile: file row group: %w", err)
		}
	}
	if !row.SourceKind.Valid() {
		return fmt.Errorf("reconcile: file row has invalid source kind %q", row.SourceKind)
	}
	if !row.Layer.Valid() {
		return fmt.Errorf("reconcile: file row has invalid layer %q", row.Layer)
	}
	return nil
}

func convertAliasRow(row state.AliasBaseline) (AliasState, error) {
	if err := validateAliasRow(row); err != nil {
		return AliasState{}, err
	}
	return AliasState{
		aliasPath:           row.AliasPath,
		canonicalTargetPath: row.CanonicalTargetPath,
		groupName:           row.GroupName,
		layer:               row.Layer,
		active:              row.Status == state.StatusActive,
		retiredAt:           state.CloneTimestamp(row.RetiredAt),
	}, nil
}

func validateAliasRow(row state.AliasBaseline) error {
	if !state.IsSlashRelative(row.AliasPath) {
		return fmt.Errorf("reconcile: alias row path %q is not a slash-relative path", row.AliasPath)
	}
	if !state.IsSlashRelative(row.CanonicalTargetPath) {
		return fmt.Errorf("reconcile: alias row target %q is not a slash-relative path", row.CanonicalTargetPath)
	}
	if row.GroupName != "" {
		if err := pathsafe.GroupName(row.GroupName); err != nil {
			return fmt.Errorf("reconcile: alias row group: %w", err)
		}
	}
	if !row.Layer.Valid() {
		return fmt.Errorf("reconcile: alias row has invalid layer %q", row.Layer)
	}
	if !row.Status.Valid() {
		return fmt.Errorf("reconcile: alias row %q has invalid status %q", row.AliasPath, row.Status)
	}
	if err := validateRetirement(row.Status, row.RetiredAt); err != nil {
		return fmt.Errorf("reconcile: alias row %q %w", row.AliasPath, err)
	}
	return nil
}

func validateRetirement(status state.SourceStatus, retiredAt *time.Time) error {
	if status == state.StatusActive && retiredAt != nil {
		return fmt.Errorf("has retirement time while active")
	}
	if status == state.StatusRetired && (retiredAt == nil || retiredAt.IsZero()) {
		return fmt.Errorf("is retired without a valid retirement time")
	}
	return nil
}

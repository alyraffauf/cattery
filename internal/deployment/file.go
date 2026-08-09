package deployment

import (
	"fmt"
	"io/fs"
)

// FileKind distinguishes ordinary deployable files from SOPS-encrypted
// secrets. Only the kind changes; target and repository paths are unaffected.
type FileKind string

const (
	FileOrdinary FileKind = "ordinary"
	FileSecret   FileKind = "secret"
)

// ParseFileKind converts a raw string into a FileKind, rejecting unknown
// values.
func ParseFileKind(value string) (FileKind, error) {
	kind := FileKind(value)
	if !kind.Valid() {
		return "", fmt.Errorf("deployment: unknown file kind %q", value)
	}
	return kind, nil
}

// Valid reports whether kind is one of the supported constants.
func (k FileKind) Valid() bool {
	switch k {
	case FileOrdinary, FileSecret:
		return true
	}
	return false
}

// ManagedFile describes one deployable source file compiled from a repository.
// SourceRepositoryPath is repository-relative; TargetRelativePath is
// HOME-relative; SourceExecutableBits records the executable bits Cattery
// must reproduce on the target.
type ManagedFile struct {
	Scope                Scope
	Layer                Layer
	Kind                 FileKind
	SourceAbsolutePath   string
	SourceRepositoryPath string
	TargetRelativePath   string
	SourceExecutableBits fs.FileMode
}

// NewManagedFile validates candidate field-by-field and returns it on success.
func NewManagedFile(candidate ManagedFile) (ManagedFile, error) {
	if err := validateFile(candidate); err != nil {
		return ManagedFile{}, err
	}
	return candidate, nil
}

func validateFile(file ManagedFile) error {
	if file.TargetRelativePath == "" {
		return fmt.Errorf("deployment: managed file has empty target relative path")
	}
	if !file.Layer.Valid() {
		return fmt.Errorf("deployment: file %q has invalid layer", file.TargetRelativePath)
	}
	if !file.Kind.Valid() {
		return fmt.Errorf("deployment: file %q has invalid kind", file.TargetRelativePath)
	}
	if file.SourceAbsolutePath == "" {
		return fmt.Errorf("deployment: file %q missing source absolute path", file.TargetRelativePath)
	}
	if file.SourceRepositoryPath == "" {
		return fmt.Errorf("deployment: file %q missing repository path", file.TargetRelativePath)
	}
	return nil
}

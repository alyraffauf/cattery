package filesystem

import (
	"fmt"
	"io/fs"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// SourceFacts freezes the read-only facts of a deployment source: identity,
// exact content token, and the executable bits that must reach the target.
type SourceFacts struct {
	entry      TargetFacts
	executable fs.FileMode
}

// FreezeSource captures the facts of an existing ordinary source file and
// rejects symlink and special sources.
func FreezeSource(path string) (SourceFacts, error) {
	entry, err := CaptureTarget(path)
	if err != nil {
		return SourceFacts{}, err
	}
	if entry.Kind() != KindFile {
		return SourceFacts{}, fmt.Errorf("filesystem: source %s is not a regular file", path)
	}
	return SourceFacts{entry: entry, executable: entry.Mode() & 0o111}, nil
}

// Token returns the frozen source content token.
func (s SourceFacts) Token() ContentToken { return s.entry.Token() }

// Executable returns the source executable bits for the target.
func (s SourceFacts) Executable() fs.FileMode { return s.executable }

// Revalidate re-checks that the source still carries the same identity,
// content token, and executable bits.
func (s SourceFacts) Revalidate() error {
	if err := s.entry.Revalidate(); err != nil {
		return err
	}
	identity, err := pathsafe.FilesystemIdentity(s.entry.Identity().Path())
	if err != nil {
		return err
	}
	if identity.Mode().Perm()&0o111 != s.executable {
		return fmt.Errorf("filesystem: source executable bits changed at %s", s.entry.Identity().Path())
	}
	return nil
}

package filesystem

import (
	"fmt"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// Precondition freezes the validated destination context of one planned
// mutation: destination, parent identity, and entry facts. Freezing and
// revalidation never mutate the filesystem.
type Precondition struct {
	destination Destination
	parent      pathsafe.Identity
	entry       TargetFacts
}

// Freeze validates the destination, walks every existing parent component,
// and captures the parent and entry facts read-only. A blocking ancestor or
// an invalid destination fails before any mutation.
func Freeze(destination Destination) (Precondition, error) {
	if _, err := pathsafe.Segments(destination.Relative); err != nil {
		return Precondition{}, err
	}
	if err := walkParentsValid(destination.Root, destination.Relative); err != nil {
		return Precondition{}, err
	}
	parent, err := parentIdentity(destination.Root, destination.Relative)
	if err != nil {
		return Precondition{}, err
	}
	entry, err := CaptureTarget(targetPath(destination))
	if err != nil {
		return Precondition{}, err
	}
	return Precondition{destination: destination, parent: parent, entry: entry}, nil
}

// Destination returns the frozen destination.
func (p Precondition) Destination() Destination { return p.destination }

// Parent returns the frozen parent identity; zero when absent at freeze.
func (p Precondition) Parent() pathsafe.Identity { return p.parent }

// Target returns the frozen destination facts.
func (p Precondition) Target() TargetFacts { return p.entry }

// Revalidate re-checks the parent and destination against the frozen facts.
// A parent absent at freeze must now be a real directory; a present parent
// must be the same real directory; the destination must match every field.
func (p Precondition) Revalidate() error {
	parent, err := parentIdentity(p.destination.Root, p.destination.Relative)
	if err != nil {
		return err
	}
	if p.parent.Path() == "" {
		if parent.Path() == "" {
			return fmt.Errorf("filesystem: parent still absent at %s", filepath.Dir(targetPath(p.destination)))
		}
	} else if !pathsafe.SameIdentity(p.parent, parent) {
		return fmt.Errorf("filesystem: parent changed at %s", p.parent.Path())
	}
	return p.entry.Revalidate()
}

// revalidateBeforeCreatingParent accepts a parent that was absent at freeze
// and remains absent. An existing frozen parent must retain its identity, and
// the destination entry must remain unchanged in both cases.
func (p Precondition) revalidateBeforeCreatingParent() error {
	if err := walkParentsValid(p.destination.Root, p.destination.Relative); err != nil {
		return err
	}
	parent, err := parentIdentity(p.destination.Root, p.destination.Relative)
	if err != nil {
		return err
	}
	if p.parent.Path() != "" && !pathsafe.SameIdentity(p.parent, parent) {
		return fmt.Errorf("filesystem: parent changed at %s", p.parent.Path())
	}
	return p.entry.Revalidate()
}

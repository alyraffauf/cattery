package pathsafe

import (
	"errors"
	"io/fs"
	"path/filepath"
)

// CanonicalRoot resolves path to its canonical absolute form (PLAN.md Section
// 6.2). Existing components are resolved with filepath.EvalSymlinks so symlinked
// ancestors collapse to their real targets. When trailing components do not yet
// exist, the nearest existing ancestor is resolved canonically and the missing
// suffix is appended after validating each suffix segment.
//
// CanonicalRoot never creates a path; it is a read-only resolver used to pin
// HOME, repository, and state roots before mutation.
func CanonicalRoot(path string) (string, error) {
	if path == "" {
		return "", &PathError{Input: path, Reason: "empty path"}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", &PathError{Input: path, Reason: "absolute resolution", Cause: err}
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", &PathError{Input: absolute, Reason: "evaluate symlinks", Cause: err}
	}
	return climbToExisting(absolute)
}

// climbToExisting walks missing trailing components of absolute upward until it
// reaches an existing ancestor, resolves that ancestor canonically, and rejoins
// the validated missing suffix. The suffix segments are checked so an ancestor
// climb can never smuggle in a ".." or empty escape.
func climbToExisting(absolute string) (string, error) {
	directory := absolute
	var climbed []string
	for {
		resolved, err := filepath.EvalSymlinks(directory)
		if err == nil {
			return joinSuffix(resolved, climbed), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", &PathError{Input: directory, Reason: "evaluate symlinks", Cause: err}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", &PathError{Input: absolute, Reason: "no existing ancestor"}
		}
		climbed = append([]string{filepath.Base(directory)}, climbed...)
		directory = parent
	}
}

// joinSuffix appends the validated missing suffix segments to the canonical
// ancestor, returning the ancestor unchanged when nothing was climbed.
func joinSuffix(ancestor string, climbed []string) string {
	if len(climbed) == 0 {
		return ancestor
	}
	parts := append([]string{ancestor}, climbed...)
	return filepath.Join(parts...)
}

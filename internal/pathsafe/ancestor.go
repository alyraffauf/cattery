package pathsafe

import (
	"os"
	"path/filepath"
)

// AncestorWalk validates that every existing component from root through the
// parent of relativePath is a real directory. It uses
// os.Lstat so a symlinked parent component is rejected even when it resolves
// beneath the same root. The walk is read-only: no path is created.
//
// relativePath is the HOME-relative destination; its final segment is the
// intended entry and is not walked, so a missing target does not fail the
// parent walk.
func AncestorWalk(root, relativePath string) error {
	segments, err := Segments(relativePath)
	if err != nil {
		return err
	}
	return walkParents(root, parentSegments(segments))
}

// ExistingAncestorWalk validates every existing component from root through
// the parent of relativePath. It stops at the first missing component because
// nothing below it can exist yet. Callers provide error rendering so the
// validation stays shared without imposing their package's error vocabulary.
func ExistingAncestorWalk(root, relativePath string, rootError func(error) error, entryError func(path, reason string) error) error {
	segments, err := Segments(relativePath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return rootError(err)
	}
	if err := directoryEntryError(root, info, entryError); err != nil {
		return err
	}
	current := root
	for _, segment := range parentSegments(segments) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := directoryEntryError(current, info, entryError); err != nil {
			return err
		}
	}
	return nil
}

func directoryEntryError(path string, info os.FileInfo, entryError func(path, reason string) error) error {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return entryError(path, "symlink")
	case isSpecial(mode):
		return entryError(path, "special")
	case !mode.IsDir():
		return entryError(path, "non-directory")
	default:
		return nil
	}
}

// parentSegments drops the final destination segment, leaving the chain of
// directories that must already be real directories.
func parentSegments(segments []string) []string {
	if len(segments) == 0 {
		return nil
	}
	return segments[:len(segments)-1]
}

func walkParents(root string, segments []string) error {
	if err := requireDirectory(root); err != nil {
		return err
	}
	current := root
	for _, segment := range segments {
		current = filepath.Join(current, segment)
		if err := requireDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

// requireDirectory reports whether path is a real directory, never a symlink,
// regular file, or special entry. The descriptive PathError names the blocking
// component so callers can surface it.
func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return &PathError{Input: path, Reason: "stat component", Cause: err}
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		return &PathError{Input: path, Reason: "symlink component"}
	}
	if isSpecial(mode) {
		return &PathError{Input: path, Reason: "special component"}
	}
	if !mode.IsDir() {
		return &PathError{Input: path, Reason: "non-directory component"}
	}
	return nil
}

// isSpecial reports whether mode is a device, named pipe, socket, or char
// device, none of which are acceptable parent components.
func isSpecial(mode os.FileMode) bool {
	mask := os.ModeDevice | os.ModeNamedPipe | os.ModeSocket | os.ModeCharDevice
	return mode&mask != 0
}

package pathsafe

import (
	"os"
	"path/filepath"
)

// AncestorWalk validates that every existing component from root through the
// parent of relativePath is a real directory (PLAN.md Section 6.2). It uses
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

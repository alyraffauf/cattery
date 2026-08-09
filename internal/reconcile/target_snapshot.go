package reconcile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// CaptureTarget freezes the immutable facts of one destination without
// mutating it: entry kind, object and parent identities, exact-bytes token,
// semantic digest, permission mode, and symlink payload. A missing path
// freezes as KindAbsent; a final symlink is never followed. Existing parent
// components must be real directories, while missing parents are tolerated
// because a replacement may create them.
func CaptureTarget(destination Destination) (TargetSnapshot, error) {
	if _, err := pathsafe.Segments(destination.Relative); err != nil {
		return TargetSnapshot{}, err
	}
	if err := walkParentComponents(destination.Root, destination.Relative); err != nil {
		return TargetSnapshot{}, err
	}
	parent, err := parentIdentity(destination.Root, destination.Relative)
	if err != nil {
		return TargetSnapshot{}, err
	}
	return captureEntry(destination, parent)
}

// walkParentComponents checks every existing component from root through the
// parent of relative; each must be a real directory. The walk stops at the
// first missing component because nothing deeper can exist yet.
func walkParentComponents(root, relative string) error {
	segments, err := pathsafe.Segments(relative)
	if err != nil {
		return err
	}
	if err := requireDirectory(root); err != nil {
		return err
	}
	current := root
	for _, segment := range segments[:len(segments)-1] {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := requireDirectoryEntry(current, info); err != nil {
			return err
		}
	}
	return nil
}

// parentIdentity freezes the identity of the destination parent directory;
// a missing parent yields the zero identity.
func parentIdentity(root, relative string) (pathsafe.Identity, error) {
	parent := filepath.Join(root, filepath.Dir(filepath.FromSlash(relative)))
	identity, err := pathsafe.FilesystemIdentity(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return pathsafe.Identity{}, nil
		}
		return pathsafe.Identity{}, err
	}
	return identity, nil
}

// captureEntry freezes the destination entry facts: kind, object identity,
// permission mode, content token and digest for regular files, and the exact
// payload for symlinks.
func captureEntry(destination Destination, parent pathsafe.Identity) (TargetSnapshot, error) {
	path := filepath.Join(destination.Root, filepath.FromSlash(destination.Relative))
	identity, err := pathsafe.FilesystemIdentity(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return TargetSnapshot{destination: destination, parent: parent, kind: KindAbsent}, nil
		}
		return TargetSnapshot{}, err
	}
	snapshot := TargetSnapshot{
		destination: destination, parent: parent, kind: KindOfIdentity(identity),
		identity: identity, mode: identity.Mode().Perm(),
	}
	switch snapshot.kind {
	case KindFile:
		return captureFileFacts(path, snapshot)
	case KindSymlink:
		payload, err := os.Readlink(path)
		if err != nil {
			return TargetSnapshot{}, fmt.Errorf("reconcile: read link %s: %w", path, err)
		}
		snapshot.payload = payload
	}
	return snapshot, nil
}

// captureFileFacts hashes the exact bytes of a regular target into the
// content token and semantic digest without retaining them.
func captureFileFacts(path string, snapshot TargetSnapshot) (TargetSnapshot, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("reconcile: read target %s: %w", path, err)
	}
	snapshot.token = TokenOfContent(content)
	snapshot.digest = deployment.Ordinary(content)
	return snapshot, nil
}

// requireDirectory rejects a root that is missing or not a real directory.
func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reconcile: stat root %s: %w", path, err)
	}
	return requireDirectoryEntry(path, info)
}

// requireDirectoryEntry rejects symlink, special, and non-directory
// components so a destination can never be redirected through its parents.
func requireDirectoryEntry(path string, info os.FileInfo) error {
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		return fmt.Errorf("reconcile: symlink component %s", path)
	}
	if mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeCharDevice) != 0 {
		return fmt.Errorf("reconcile: special component %s", path)
	}
	if !mode.IsDir() {
		return fmt.Errorf("reconcile: non-directory component %s", path)
	}
	return nil
}

// KindOfIdentity classifies an existing identity without touching the path.
func KindOfIdentity(identity pathsafe.Identity) EntryKind {
	mode := identity.Mode()
	switch {
	case mode.IsDir():
		return KindDirectory
	case mode&os.ModeSymlink != 0:
		return KindSymlink
	case mode.IsRegular():
		return KindFile
	}
	return KindSpecial
}

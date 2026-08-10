package reconcile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
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
	return pathsafe.ExistingAncestorWalk(root, relative,
		func(err error) error {
			return fmt.Errorf("reconcile: stat root %s: %w", root, err)
		},
		func(path, reason string) error {
			return fmt.Errorf("reconcile: %s component %s", reason, path)
		})
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
		destination: destination, parent: parent, kind: filesystem.KindOfIdentity(identity),
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
	content, err := readTargetContent(path, snapshot)
	if err != nil {
		return TargetSnapshot{}, err
	}
	live, err := pathsafe.FilesystemIdentity(path)
	if err != nil {
		return TargetSnapshot{}, fmt.Errorf("reconcile: stat target after read %s: %w", path, err)
	}
	if !pathsafe.SameIdentity(snapshot.identity, live) || live.Mode().Perm() != snapshot.mode {
		return TargetSnapshot{}, fmt.Errorf("reconcile: target changed while reading %s", path)
	}
	snapshot.token = filesystem.TokenOfContent(content)
	snapshot.digest = deployment.Ordinary(content)
	return snapshot, nil
}

func readTargetContent(path string, snapshot TargetSnapshot) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reconcile: read target %s: %w", path, err)
	}
	defer file.Close()
	return readValidatedContent(file, snapshot, path)
}

func readValidatedContent(file *os.File, snapshot TargetSnapshot, path string) ([]byte, error) {
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("reconcile: stat opened target %s: %w", path, err)
	}
	if err := validateOpenedFile(snapshot, opened); err != nil {
		return nil, err
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("reconcile: read target %s: %w", path, err)
	}
	readAgain, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("reconcile: stat target after read %s: %w", path, err)
	}
	if err := validateOpenedFile(snapshot, readAgain); err != nil {
		return nil, err
	}
	return content, nil
}

func validateOpenedFile(snapshot TargetSnapshot, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("reconcile: target changed to non-regular file %s", snapshot.identity.Path())
	}
	if !snapshot.identity.SameFileInfo(info) || info.Mode().Perm() != snapshot.mode {
		return fmt.Errorf("reconcile: target identity or mode changed while reading %s", snapshot.identity.Path())
	}
	return nil
}

// KindOfIdentity classifies an existing identity without touching the path.
func KindOfIdentity(identity pathsafe.Identity) EntryKind {
	return filesystem.KindOfIdentity(identity)
}

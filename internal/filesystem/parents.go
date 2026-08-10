package filesystem

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func targetPath(destination Destination) string {
	return filepath.Join(destination.Root, filepath.FromSlash(destination.Relative))
}

func walkParentsValid(root, relative string) error {
	return pathsafe.ExistingAncestorWalk(root, relative,
		func(err error) error {
			return fmt.Errorf("filesystem: stat root %s: %w", root, err)
		},
		func(path, reason string) error {
			return fmt.Errorf("filesystem: %s parent component %s", reason, path)
		})
}

// ensureParents creates only missing parent components.  Mkdir is deliberately
// followed by a complete walk: an EEXIST result may mean that another process
// installed a symlink instead of the directory we need.
func ensureParents(root, relative string) error {
	segments, err := pathsafe.Segments(relative)
	if err != nil {
		return err
	}
	if err := requireDir(root); err != nil {
		return err
	}
	current := root
	for _, segment := range segments[:len(segments)-1] {
		current = filepath.Join(current, segment)
		if err := ensureParent(root, relative, current); err != nil {
			return err
		}
	}
	return walkParentsValid(root, relative)
}

func ensureParent(root, relative, current string) error {
	info, err := os.Lstat(current)
	if errors.Is(err, fs.ErrNotExist) {
		if err := createParent(current); err != nil {
			return err
		}
		return walkParentsValid(root, relative)
	}
	if err != nil {
		return err
	}
	return requireDirEntry(current, info)
}

func createParent(path string) error {
	if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("filesystem: create parent %s: %w", path, err)
	}
	return nil
}

func requireDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("filesystem: stat %s: %w", path, err)
	}
	return requireDirEntry(path, info)
}

func requireDirEntry(path string, info os.FileInfo) error {
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		return fmt.Errorf("filesystem: symlink parent component %s", path)
	}
	if mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeCharDevice) != 0 {
		return fmt.Errorf("filesystem: special parent component %s", path)
	}
	if !mode.IsDir() {
		return fmt.Errorf("filesystem: non-directory parent component %s", path)
	}
	return nil
}

func parentIdentity(root, relative string) (pathsafe.Identity, error) {
	parent := filepath.Join(root, filepath.Dir(filepath.FromSlash(relative)))
	identity, err := pathsafe.FilesystemIdentity(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return pathsafe.Identity{}, nil
		}
		return pathsafe.Identity{}, err
	}
	if !identity.Mode().IsDir() {
		return pathsafe.Identity{}, fmt.Errorf("filesystem: parent %s is not a directory", parent)
	}
	return identity, nil
}

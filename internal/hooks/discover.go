package hooks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// Discover validates the _hooks tree of one scope beneath root and returns
// the immutable hook descriptors sorted by phase and bytewise name. A scope
// without a _hooks tree yields no hooks, not an error.
func Discover(root string, scope deployment.Scope) ([]deployment.Hook, error) {
	hooksRoot := filepath.Join(root, scope.Group, "_hooks")
	info, err := os.Lstat(hooksRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("hooks: %q is not a directory", hooksRoot)
	}
	var discovered []deployment.Hook
	for _, phase := range []deployment.HookPhase{deployment.HookBefore, deployment.HookAfter} {
		found, err := discoverPhase(hooksRoot, scope, phase)
		if err != nil {
			return nil, err
		}
		discovered = append(discovered, found...)
	}
	deployment.SortHooks(discovered)
	return discovered, nil
}

func discoverPhase(hooksRoot string, scope deployment.Scope, phase deployment.HookPhase) ([]deployment.Hook, error) {
	phasePath := filepath.Join(hooksRoot, string(phase))
	info, err := os.Lstat(phasePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("hooks: phase path %q is not a directory", phasePath)
	}
	entries, err := os.ReadDir(phasePath)
	if err != nil {
		return nil, err
	}
	var discovered []deployment.Hook
	for _, entry := range entries {
		found, err := discoverEntry(scope, phasePath, entry)
		if err != nil {
			return nil, err
		}
		discovered = append(discovered, found)
	}
	return discovered, nil
}

func discoverEntry(scope deployment.Scope, phasePath string, entry os.DirEntry) (deployment.Hook, error) {
	phase := deployment.HookPhase(filepath.Base(phasePath))
	full := filepath.Join(phasePath, entry.Name())
	info, err := entry.Info()
	if err != nil {
		return deployment.Hook{}, err
	}
	if !info.Mode().IsRegular() {
		return deployment.Hook{}, fmt.Errorf("hooks: %q is not a regular file", full)
	}
	if info.Mode().Perm()&deployment.ExecutableBitMask == 0 {
		return deployment.Hook{}, fmt.Errorf("hooks: %q is not executable", full)
	}
	return deployment.NewHook(deployment.Hook{
		Scope: scope, Phase: phase, Name: entry.Name(),
		AbsolutePath:   full,
		RepositoryPath: filepath.Join(scope.Group, "_hooks", string(phase), entry.Name()),
	})
}

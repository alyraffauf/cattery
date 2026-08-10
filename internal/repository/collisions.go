package repository

import (
	"fmt"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// CollisionScope carries the pure lexical inputs the engine validates
// against: the canonical HOME root and the absolute protected trees that
// targets must not enter.
type CollisionScope struct {
	HomeRoot  string
	Protected []string
}

// CheckCollisions rejects equivalent or overlapping destinations among the
// compiled files and aliases of one platform plan, plus destinations that
// equal or descend into a protected tree. Alias records must already be
// filtered to the plan's platform; the only legal file/alias overlap is an
// alias pointing at its own canonical target.
func CheckCollisions(files []deployment.ManagedFile, aliases []deployment.Alias, scope CollisionScope) error {
	if err := fileCollisions(files); err != nil {
		return err
	}
	if err := aliasFileCollisions(aliases, files); err != nil {
		return err
	}
	if err := aliasCollisions(aliases); err != nil {
		return err
	}
	return protectedTreeCollisions(files, aliases, scope)
}

func fileCollisions(files []deployment.ManagedFile) error {
	targets := fileTargets(files)
	for first := 0; first < len(files); first++ {
		second, err := conflictIndex(targets, first)
		if err != nil {
			return err
		}
		if second >= 0 {
			return filePairError(files[first], files[second])
		}
	}
	return nil
}

// conflictIndex returns the first later destination colliding with
// destinations[first], or -1.
func conflictIndex(destinations []string, first int) (int, error) {
	for second := first + 1; second < len(destinations); second++ {
		collides, err := destinationsCollide(destinations[first], destinations[second])
		if err != nil {
			return -1, err
		}
		if collides {
			return second, nil
		}
	}
	return -1, nil
}

func fileTargets(files []deployment.ManagedFile) []string {
	targets := make([]string, len(files))
	for index := range files {
		targets[index] = files[index].TargetRelativePath
	}
	return targets
}

func aliasDestinations(aliases []deployment.Alias) []string {
	destinations := make([]string, len(aliases))
	for index := range aliases {
		destinations[index] = aliases[index].AliasRelativePath
	}
	return destinations
}

func destinationsCollide(first, second string) (bool, error) {
	firstSegments, err := pathsafe.Segments(first)
	if err != nil {
		return false, err
	}
	secondSegments, err := pathsafe.Segments(second)
	if err != nil {
		return false, err
	}
	return pathsafe.PortableOverlap(firstSegments, secondSegments), nil
}

// filePairError identifies both source owners and the colliding target.
func filePairError(first, second deployment.ManagedFile) error {
	return fmt.Errorf("repository: file %q collides with file %q at target %q",
		first.SourceRepositoryPath, second.SourceRepositoryPath, second.TargetRelativePath)
}

// aliasFileCollisions rejects alias destinations that collide with a managed
// file target. An alias pointing at its own canonical target is the only
// legal destination/file overlap.
func aliasFileCollisions(aliases []deployment.Alias, files []deployment.ManagedFile) error {
	for aliasIndex := range aliases {
		fileIndex, err := aliasFileConflictIndex(aliases[aliasIndex], files)
		if err != nil {
			return err
		}
		if fileIndex >= 0 {
			return aliasFilePairError(aliases[aliasIndex], files[fileIndex])
		}
	}
	return nil
}

// aliasFileConflictIndex returns the first file colliding with the alias, or
// -1.
func aliasFileConflictIndex(alias deployment.Alias, files []deployment.ManagedFile) (int, error) {
	for index := range files {
		collides, err := aliasFileCollide(alias, files[index])
		if err != nil {
			return -1, err
		}
		if collides {
			return index, nil
		}
	}
	return -1, nil
}

func aliasFileCollide(alias deployment.Alias, file deployment.ManagedFile) (bool, error) {
	destination, err := pathsafe.Segments(alias.AliasRelativePath)
	if err != nil {
		return false, err
	}
	target, err := pathsafe.Segments(file.TargetRelativePath)
	if err != nil {
		return false, err
	}
	canonical, err := pathsafe.Segments(alias.CanonicalTargetRelativePath)
	if err != nil {
		return false, err
	}
	if pathsafe.PathsEquivalent(destination, canonical) && pathsafe.PathsEquivalent(canonical, target) {
		return false, nil
	}
	return pathsafe.PortableOverlap(destination, target), nil
}

// aliasFilePairError identifies the alias scope, the file owner, and the
// colliding destination.
func aliasFilePairError(alias deployment.Alias, file deployment.ManagedFile) error {
	return fmt.Errorf("repository: alias %q in scope %q collides with file %q at destination %q",
		alias.AliasRelativePath, scopeLabel(alias.Scope), file.SourceRepositoryPath, alias.AliasRelativePath)
}

// aliasCollisions rejects equivalent or ancestor-related alias destinations.
func aliasCollisions(aliases []deployment.Alias) error {
	destinations := aliasDestinations(aliases)
	for first := 0; first < len(aliases); first++ {
		second, err := conflictIndex(destinations, first)
		if err != nil {
			return err
		}
		if second >= 0 {
			return aliasPairError(aliases[first], aliases[second])
		}
	}
	return nil
}

// aliasPairError identifies both declaring scopes and the colliding
// destination.
func aliasPairError(first, second deployment.Alias) error {
	return fmt.Errorf("repository: alias %q in scope %q collides with alias %q in scope %q at destination %q",
		first.AliasRelativePath, scopeLabel(first.Scope),
		second.AliasRelativePath, scopeLabel(second.Scope), second.AliasRelativePath)
}

// protectedTreeCollisions rejects file targets and alias destinations that
// equal or descend into a protected tree beneath HOME.
func protectedTreeCollisions(files []deployment.ManagedFile, aliases []deployment.Alias, scope CollisionScope) error {
	if scope.HomeRoot == "" {
		return nil
	}
	targets := fileTargets(files)
	destinations := aliasDestinations(aliases)
	for _, tree := range scope.Protected {
		if index := protectedIndex(targets, scope.HomeRoot, tree); index >= 0 {
			return protectedFileError(files[index], tree)
		}
		if index := protectedIndex(destinations, scope.HomeRoot, tree); index >= 0 {
			return protectedAliasError(aliases[index], tree)
		}
	}
	return nil
}

// protectedIndex returns the first path that equals or descends into the
// tree, or -1.
func protectedIndex(paths []string, homeRoot, tree string) int {
	for index := range paths {
		if protectedTarget(paths[index], homeRoot, tree) {
			return index
		}
	}
	return -1
}

// protectedTarget reports whether the HOME-relative path equals or descends
// into the protected tree, under native and portable equivalence.
func protectedTarget(relative, homeRoot, tree string) bool {
	return pathsafe.ProtectedTree(filepath.Join(homeRoot, relative), tree)
}

func protectedFileError(file deployment.ManagedFile, tree string) error {
	return fmt.Errorf("repository: file %q target %q descends into protected tree %q",
		file.SourceRepositoryPath, file.TargetRelativePath, tree)
}

func protectedAliasError(alias deployment.Alias, tree string) error {
	return fmt.Errorf("repository: alias %q in scope %q descends into protected tree %q",
		alias.AliasRelativePath, scopeLabel(alias.Scope), tree)
}

// scopeLabel renders a scope for diagnostics.
func scopeLabel(scope deployment.Scope) string {
	if scope.IsRoot() {
		return "root"
	}
	return scope.Group
}

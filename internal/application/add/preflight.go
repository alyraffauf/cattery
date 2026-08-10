package add

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// preflightContext bundles the read-only inputs of batch validation: the
// canonical repository pair (Home roots target validation) and the compiled
// plan (owned sources are checked for collisions).
type preflightContext struct {
	identity RepositoryIdentity
	plan     deployment.Plan
}

// resolveTargets canonicalizes each raw argument against the working
// directory, requires it to be a strict descendant of home, expands directory
// arguments into descendants, and rejects duplicate canonical paths. It is the
// canonicalization half of preflight: inference needs canonical absolute
// targets, so resolution runs first.
func resolveTargets(workingDir, home string, raw []string) ([]string, error) {
	targets := make([]string, 0, len(raw))
	seenArguments := make(map[string]bool, len(raw))
	seenTargets := make(map[string]bool, len(raw))
	for _, argument := range raw {
		resolved, err := resolveOneTarget(workingDir, home, argument)
		if err != nil {
			return nil, err
		}
		if seenArguments[resolved] {
			return nil, failure.New(failure.InvalidInput, "add: duplicate target "+argument, nil)
		}
		seenArguments[resolved] = true

		expanded, err := expandTarget(resolved)
		if err != nil {
			return nil, err
		}
		for _, target := range expanded {
			if seenTargets[target] {
				return nil, failure.New(failure.InvalidInput, "add: duplicate target "+target, nil)
			}
			seenTargets[target] = true
			targets = append(targets, target)
		}
	}
	return targets, nil
}

// expandTarget returns target itself unless it is a directory, in which case
// it returns every non-directory descendant. Directories are not repository
// entries, so empty directories are intentionally omitted.
func expandTarget(target string) ([]string, error) {
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() {
		return []string{target}, nil
	}
	return expandDirectory(target)
}

// expandDirectory recursively collects descendants in lexical order. It uses
// Lstat so a symlinked directory remains a leaf for preflight to reject rather
// than redirecting the traversal outside the requested tree.
func expandDirectory(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, failure.New(failure.InvalidInput, "add: read directory "+directory, err)
	}

	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, failure.New(failure.InvalidInput, "add: stat target "+path, err)
		}
		if !info.IsDir() {
			targets = append(targets, path)
			continue
		}
		children, err := expandDirectory(path)
		if err != nil {
			return nil, err
		}
		targets = append(targets, children...)
	}
	return targets, nil
}

// resolveOneTarget resolves one argument to its canonical absolute form and
// proves it lives strictly beneath home.
func resolveOneTarget(workingDir, home, argument string) (string, error) {
	canonical, err := pathsafe.CanonicalRoot(absoluteTarget(workingDir, argument))
	if err != nil {
		return "", failure.New(failure.InvalidInput, "add: resolve target "+argument, err)
	}
	if !pathsafe.Contains(home, canonical) {
		return "", failure.New(failure.InvalidInput, "add: target "+argument+" is not beneath $HOME", nil)
	}
	return canonical, nil
}

// absoluteTarget returns the absolute form of argument, joining a relative
// argument to the command's initial working directory.
func absoluteTarget(workingDir, argument string) string {
	if filepath.IsAbs(argument) {
		return argument
	}
	return filepath.Join(workingDir, argument)
}

// Preflight validates the inferred batch without writing: each target must be
// a regular file, no two arguments may name the same object, and no derived
// source may collide with an existing owner or another batch item. It returns
// a defensive copy of items with ExecutableBits filled from each live target.
func Preflight(context preflightContext, items []ItemPlanInput) ([]ItemPlanInput, error) {
	snapshots, err := captureSnapshots(context.identity.Home, items)
	if err != nil {
		return nil, err
	}
	if err := rejectSameObject(snapshots); err != nil {
		return nil, err
	}
	if err := rejectSourceCollisions(context.plan, items); err != nil {
		return nil, err
	}
	return applyExecutableBits(items, snapshots), nil
}

// captureSnapshots reads each target as a precondition and requires it to be
// a regular file, rejecting directories, symlinks, and special entries.
func captureSnapshots(home string, items []ItemPlanInput) ([]reconcile.TargetSnapshot, error) {
	snapshots := make([]reconcile.TargetSnapshot, 0, len(items))
	for _, item := range items {
		snapshot, err := reconcile.CaptureTarget(reconcile.Destination{Root: home, Relative: item.TargetRelativePath})
		if err != nil {
			return nil, failure.New(failure.InvalidInput, "add: capture target "+item.TargetRelativePath, err)
		}
		if snapshot.Kind() != reconcile.KindFile {
			return nil, failure.New(failure.InvalidInput, "add: target "+item.TargetRelativePath+" is not a regular file", nil)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// rejectSameObject rejects two arguments that resolve to one filesystem object.
func rejectSameObject(snapshots []reconcile.TargetSnapshot) error {
	for index, current := range snapshots {
		if err := collidesWithEarlier(snapshots[:index], current); err != nil {
			return err
		}
	}
	return nil
}

// collidesWithEarlier reports whether current names the same object as any
// earlier snapshot.
func collidesWithEarlier(previous []reconcile.TargetSnapshot, current reconcile.TargetSnapshot) error {
	for _, candidate := range previous {
		if pathsafe.SameIdentity(candidate.Identity(), current.Identity()) {
			return failure.New(failure.InvalidInput, "add: two arguments name the same file", nil)
		}
	}
	return nil
}

// rejectSourceCollisions rejects sources owned by the plan or shared in batch.
func rejectSourceCollisions(plan deployment.Plan, items []ItemPlanInput) error {
	if err := rejectPlanCollisions(plan, items); err != nil {
		return err
	}
	return rejectBatchCollisions(items)
}

// rejectPlanCollisions rejects a derived source already owned by a different
// target in the compiled plan.
func rejectPlanCollisions(plan deployment.Plan, items []ItemPlanInput) error {
	for _, item := range items {
		if err := itemPlanCollides(plan, item); err != nil {
			return err
		}
	}
	return nil
}

// itemPlanCollides reports whether item's source belongs to another target.
func itemPlanCollides(plan deployment.Plan, item ItemPlanInput) error {
	for _, file := range plan.Files() {
		if file.SourceRepositoryPath == item.SourceRepositoryPath &&
			file.TargetRelativePath == item.TargetRelativePath {
			continue
		}
		if sourcePathsOverlap(file.SourceRepositoryPath, item.SourceRepositoryPath) {
			return failure.New(failure.InvalidInput,
				"add: source "+item.SourceRepositoryPath+" is already owned by "+file.TargetRelativePath, nil)
		}
	}
	for _, group := range plan.Groups() {
		if group == item.SourceRepositoryPath {
			return failure.New(failure.InvalidInput,
				"add: source "+item.SourceRepositoryPath+" is an existing group directory", nil)
		}
	}
	return nil
}

// sourcePathsOverlap reports exact or parent/child overlap for validated
// slash-relative repository paths.
func sourcePathsOverlap(first, second string) bool {
	return first == second || strings.HasPrefix(first, second+"/") || strings.HasPrefix(second, first+"/")
}

// rejectBatchCollisions rejects two batch items that derive one source.
func rejectBatchCollisions(items []ItemPlanInput) error {
	for index, item := range items {
		if err := sourceUsedEarlier(items[:index], item); err != nil {
			return err
		}
	}
	return nil
}

// sourceUsedEarlier reports whether item's source matches an earlier item's.
func sourceUsedEarlier(previous []ItemPlanInput, current ItemPlanInput) error {
	for _, candidate := range previous {
		if sourcePathsOverlap(candidate.SourceRepositoryPath, current.SourceRepositoryPath) {
			return failure.New(failure.InvalidInput,
				"add: two targets derive the same source "+current.SourceRepositoryPath, nil)
		}
	}
	return nil
}

// applyExecutableBits copies items and stamps each with the live exec bits.
func applyExecutableBits(items []ItemPlanInput, snapshots []reconcile.TargetSnapshot) []ItemPlanInput {
	validated := make([]ItemPlanInput, len(items))
	for index := range items {
		validated[index] = items[index]
		validated[index].ExecutableBits = snapshots[index].Mode() & deployment.ExecutableBitMask
	}
	return validated
}

package repository

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/hooks"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/routes"
)

// CompileInput carries the platform, repository root, HOME root, protected trees, and selected groups.
type CompileInput struct {
	Platform       deployment.Layer
	RepositoryRoot string
	HomeRoot       string
	Protected      []string
	Selected       []string
}

type compiledRepository struct {
	groups  []string
	files   []deployment.ManagedFile
	aliases []deployment.Alias
	hooks   []deployment.Hook
}

// Compile validates the entire repository for one platform and returns the
// deterministic plan: unselected scopes are still validated so invalid ones
// cannot hide, then the plan is filtered to the selection (empty = all).
func Compile(input CompileInput) (deployment.Plan, error) {
	records, err := compileRepository(input)
	if err != nil {
		return deployment.Plan{}, err
	}
	if err := CheckCollisions(records.files, records.aliases,
		CollisionScope{HomeRoot: input.HomeRoot, Protected: input.Protected}); err != nil {
		return deployment.Plan{}, err
	}
	return finalize(input, records)
}

func compileRepository(input CompileInput) (compiledRepository, error) {
	base, err := scanAndSelect(input)
	if err != nil {
		return compiledRepository{}, err
	}
	files, err := ResolvePlatform(input.RepositoryRoot, base, input.Platform)
	if err != nil {
		return compiledRepository{}, err
	}
	aliases, err := activateRoutes(input, compiledRepository{groups: base.Groups, files: files})
	if err != nil {
		return compiledRepository{}, err
	}
	hookRecords, err := discoverHooks(input, base.Groups)
	if err != nil {
		return compiledRepository{}, err
	}
	if err := validateDestinations(files, aliases); err != nil {
		return compiledRepository{}, err
	}
	return compiledRepository{groups: base.Groups, files: files, aliases: aliases, hooks: hookRecords}, nil
}

func scanAndSelect(input CompileInput) (ScanResult, error) {
	base, err := Scan(input.RepositoryRoot)
	if err != nil {
		return ScanResult{}, err
	}
	if err := validateSelection(base.Groups, input.Selected); err != nil {
		return ScanResult{}, err
	}
	return base, nil
}

func finalize(input CompileInput, compiled compiledRepository) (deployment.Plan, error) {
	selected := input.Selected
	groups := compiled.groups
	if len(input.Selected) > 0 {
		groups = append([]string(nil), selected...)
		sort.Strings(groups)
	}
	keptFiles := selectRecords(compiled.files, selected,
		func(file deployment.ManagedFile) bool { return scopeKept(file.Scope.Group, selected) })
	keptAliases := selectRecords(compiled.aliases, selected,
		func(alias deployment.Alias) bool { return scopeKept(alias.Scope.Group, selected) })
	keptHooks := selectRecords(compiled.hooks, selected,
		func(hook deployment.Hook) bool { return hookKept(hook.Scope, selected) })
	deployment.SortFiles(keptFiles)
	deployment.SortAliases(keptAliases)
	deployment.SortHooks(keptHooks)
	return deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: input.RepositoryRoot,
		Platform:       string(input.Platform),
		Groups:         groups,
		Files:          keptFiles,
		Aliases:        keptAliases,
		Hooks:          keptHooks,
	})
}

func selectRecords[T any](records []T, selected []string, kept func(T) bool) []T {
	filtered := make([]T, 0, len(records))
	for _, record := range records {
		if kept(record) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func scopeKept(group string, selected []string) bool {
	return len(selected) == 0 || slices.Contains(selected, group)
}

// hookKept applies the documented asymmetry between repository-scope hooks
// and root files under explicit group selection. Repository-scope (root)
// hooks — identified by scope.Group == "" — always run even when a subset of
// groups is selected, whereas root files are dropped, because Section 10.1
// mandates that repository hooks run for every apply.
func hookKept(scope deployment.Scope, selected []string) bool {
	if scope.Group == "" {
		return true
	}
	return scopeKept(scope.Group, selected)
}

func validateSelection(groups, selected []string) error {
	for _, name := range selected {
		if !slices.Contains(groups, name) {
			return fmt.Errorf("repository: selected group %q is not a repository group", name)
		}
	}
	return nil
}

func activateRoutes(input CompileInput, compiled compiledRepository) ([]deployment.Alias, error) {
	var activated []deployment.Alias
	for _, scope := range scopesOf(compiled.groups) {
		scopeAliases, err := activateScope(input, scope, compiled.files)
		if err != nil {
			return nil, err
		}
		activated = append(activated, scopeAliases...)
	}
	return activated, nil
}

func activateScope(input CompileInput, scope deployment.Scope, files []deployment.ManagedFile) ([]deployment.Alias, error) {
	config, err := loadRoutes(input.RepositoryRoot, scope)
	if err != nil {
		return nil, err
	}
	if len(config.Declarations) == 0 {
		return nil, nil
	}
	canonical := make([]string, 0, len(files))
	for _, file := range files {
		if file.Scope == scope {
			canonical = append(canonical, file.TargetRelativePath)
		}
	}
	sort.Strings(canonical)
	activated, err := routes.Activate(config, input.Platform, canonical)
	if err != nil {
		return nil, err
	}
	for index := range activated {
		activated[index].Scope = scope
	}
	return activated, nil
}

func loadRoutes(root string, scope deployment.Scope) (routes.Config, error) {
	path := filepath.Join(root, scope.Group, "_routes.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return routes.Config{}, nil
		}
		return routes.Config{}, err
	}
	return routes.Decode(data)
}

func discoverHooks(input CompileInput, groups []string) ([]deployment.Hook, error) {
	var hookRecords []deployment.Hook
	for _, scope := range scopesOf(groups) {
		found, err := hooks.Discover(input.RepositoryRoot, scope)
		if err != nil {
			return nil, err
		}
		hookRecords = append(hookRecords, found...)
	}
	return hookRecords, nil
}

func validateDestinations(files []deployment.ManagedFile, aliases []deployment.Alias) error {
	for _, file := range files {
		if err := validateTarget(file.TargetRelativePath); err != nil {
			return err
		}
	}
	for _, alias := range aliases {
		if err := validateTarget(alias.AliasRelativePath); err != nil {
			return err
		}
		if err := validateTarget(alias.CanonicalTargetRelativePath); err != nil {
			return err
		}
	}
	return nil
}

func validateTarget(path string) error {
	if _, err := pathsafe.Segments(path); err != nil {
		return fmt.Errorf("repository: %w", err)
	}
	return nil
}

func scopesOf(groups []string) []deployment.Scope {
	scopes := make([]deployment.Scope, 0, len(groups)+1)
	scopes = append(scopes, deployment.NewScope(""))
	for _, group := range groups {
		scopes = append(scopes, deployment.NewScope(group))
	}
	return scopes
}

// Package forget implements `cattery forget`: source-only removal of a
// managed HOME subtree. It never removes the user's target files.
package forget

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alyraffauf/cattery/internal/application/evaluation"
	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

type Dependencies struct {
	RepositorySource applicationrepository.RepositorySource
	Compiler         applicationrepository.Compiler
	State            evaluation.StateReader
	Retirements      RetirementStore
	Remover          SourceRemover
}

type RetirementStore interface {
	RetireFileBaseline(root, home, target string) (state.FileBaseline, error)
}

type SourceRemover interface {
	RemoveResult(context.Context, filesystem.Precondition) (filesystem.RemoveResult, error)
}

type Request struct {
	Repository RepositoryInput
	Directory  string
	DryRun     bool
	Yes        bool
}

// RepositoryInput carries raw repository-selection fields from the CLI.
type RepositoryInput = applicationrepository.RepositoryInput

type Item struct {
	Target string
	Source string
	Status string
}

type Result struct {
	Items []Item
}

type Service struct{ deps Dependencies }

func NewService(dependencies Dependencies) *Service { return &Service{deps: dependencies} }

// Forget removes matching sources and retires their active state rows. The
// target subtree is used only as an address; it is never read or mutated.
func (service *Service) Forget(ctx context.Context, request Request) (Result, error) {
	identity, err := service.resolve(request.Repository)
	if err != nil {
		return Result{}, err
	}
	directory, err := resolveDirectory(request.Repository.WorkingDir, identity.Home, request.Directory)
	if err != nil {
		return Result{}, err
	}
	if err := service.rejectAliases(identity, directory); err != nil {
		return Result{}, err
	}
	items, err := service.plan(identity, directory)
	if err != nil {
		return Result{}, err
	}
	result := Result{Items: records(items, "planned")}
	if request.DryRun {
		return result, nil
	}
	if !request.Yes {
		return result, failure.New(failure.InvalidInput, "forget: pass --yes to remove repository sources", nil)
	}
	return service.execute(ctx, identity, items)
}

func (service *Service) resolve(input applicationrepository.RepositoryInput) (applicationrepository.RepositoryIdentity, error) {
	identity, err := service.deps.RepositorySource.Resolve(selection.RepositoryRequest{
		RawExplicit: input.RawExplicit, ExplicitSet: input.ExplicitSet, RawEnv: input.RawEnv,
		EnvSet: input.EnvSet, WorkingDir: input.WorkingDir,
	})
	if err != nil {
		return applicationrepository.RepositoryIdentity{}, failure.New(failure.InvalidInput, "forget: resolve repository", err)
	}
	return identity, nil
}

func resolveDirectory(workingDirectory, home, argument string) (string, error) {
	if argument == "" {
		return "", failure.New(failure.InvalidInput, "forget: empty directory", nil)
	}
	path := argument
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingDirectory, path)
	}
	canonical, err := pathsafe.CanonicalRoot(path)
	if err != nil {
		return "", failure.New(failure.InvalidInput, "forget: resolve directory "+argument, err)
	}
	if canonical == home || !pathsafe.Contains(home, canonical) {
		return "", failure.New(failure.InvalidInput, "forget: directory must be strictly beneath $HOME", nil)
	}
	relative, err := filepath.Rel(home, canonical)
	if err != nil {
		return "", failure.New(failure.InvalidInput, "forget: relative directory", err)
	}
	return filepath.ToSlash(relative), nil
}

func (service *Service) rejectAliases(identity applicationrepository.RepositoryIdentity, directory string) error {
	for _, platform := range []deployment.Layer{deployment.LayerLinux, deployment.LayerDarwin} {
		plan, err := service.deps.Compiler.Compile(repository.CompileInput{Platform: platform, RepositoryRoot: identity.Root, HomeRoot: identity.Home})
		if err != nil {
			return failure.New(failure.InvalidInput, "forget: compile plan", err)
		}
		for _, alias := range plan.Aliases() {
			if within(directory, alias.AliasRelativePath) || within(directory, alias.CanonicalTargetRelativePath) {
				return failure.New(failure.InvalidInput, "forget: remove aliases before forgetting "+directory, nil)
			}
		}
	}
	return nil
}

type plannedItem struct {
	target string
	source repository.Source
}

func (service *Service) plan(identity applicationrepository.RepositoryIdentity, directory string) ([]plannedItem, error) {
	sources, err := repository.Sources(identity.Root)
	if err != nil {
		return nil, failure.New(failure.InvalidInput, "forget: scan repository", err)
	}
	items := make([]plannedItem, 0)
	for _, source := range sources {
		if within(directory, source.TargetRelativePath) {
			items = append(items, plannedItem{target: source.TargetRelativePath, source: source})
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].source.Candidate.SourceRepoPath < items[right].source.Candidate.SourceRepoPath
	})
	return items, nil
}

func within(directory, path string) bool {
	return path == directory || strings.HasPrefix(path, directory+"/")
}

func records(items []plannedItem, status string) []Item {
	records := make([]Item, 0, len(items))
	for _, item := range items {
		records = append(records, Item{Target: item.target, Source: item.source.Candidate.SourceRepoPath, Status: status})
	}
	return records
}

func (service *Service) execute(ctx context.Context, identity applicationrepository.RepositoryIdentity, items []plannedItem) (Result, error) {
	result := Result{Items: make([]Item, 0, len(items))}
	active, err := service.deps.State.FileBaselines(identity.Root, identity.Home)
	if err != nil {
		return result, fmt.Errorf("forget: read baselines: %w", err)
	}
	activeTargets := activeTargetSet(active)
	retiredTargets := make(map[string]bool, len(activeTargets))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		precondition, err := filesystem.Freeze(filesystem.Destination{Root: identity.Root, Relative: item.source.Candidate.SourceRepoPath})
		if err != nil {
			return result, fmt.Errorf("forget: freeze source %s: %w", item.source.Candidate.SourceRepoPath, err)
		}
		removed, err := service.deps.Remover.RemoveResult(ctx, precondition)
		if err != nil || !removed.Removed || !removed.DirectorySynced {
			return result, fmt.Errorf("forget: remove source %s: %w", item.source.Candidate.SourceRepoPath, err)
		}
		if activeTargets[item.target] && !retiredTargets[item.target] {
			if _, err := service.deps.Retirements.RetireFileBaseline(identity.Root, identity.Home, item.target); err != nil {
				return result, fmt.Errorf("forget: retire %s: %w", item.target, err)
			}
			retiredTargets[item.target] = true
		}
		result.Items = append(result.Items, Item{Target: item.target, Source: item.source.Candidate.SourceRepoPath, Status: "forgotten"})
	}
	return result, nil
}

func activeTargetSet(baselines []state.FileBaseline) map[string]bool {
	targets := make(map[string]bool, len(baselines))
	for _, baseline := range baselines {
		if baseline.Status == state.StatusActive {
			targets[baseline.TargetPath] = true
		}
	}
	return targets
}

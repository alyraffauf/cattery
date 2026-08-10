package secretlifecycle

import (
	"fmt"
	"slices"
	"strings"

	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/repository"
)

type selectedSource struct {
	source repository.Source
}

func (service *Service) resolveAndSelect(request Request) (applicationrepository.RepositoryIdentity, []selectedSource, error) {
	identity, err := service.deps.RepositorySource.Resolve(repositoryRequest(request.Repository))
	if err != nil {
		return identity, nil, failure.New(failure.InvalidInput, "secrets: resolve repository", err)
	}
	sources, err := repository.Sources(identity.Root)
	if err != nil {
		return identity, nil, failure.FromPathError("secrets: scan repository", err)
	}
	scan, err := repository.Scan(identity.Root)
	if err != nil {
		return identity, nil, failure.FromPathError("secrets: scan repository groups", err)
	}
	selected, err := selectSources(sources, scan.Groups, request.Groups, request.Sources)
	if err != nil {
		return identity, nil, failure.New(failure.InvalidInput, "secrets: select sources", err)
	}
	return identity, selected, nil
}

func selectSources(sources []repository.Source, groupsFound, groups, paths []string) ([]selectedSource, error) {
	for _, group := range groups {
		if !slices.Contains(groupsFound, group) {
			return nil, fmt.Errorf("unknown group %q", group)
		}
	}
	requestedPaths, err := validateSourcePaths(paths)
	if err != nil {
		return nil, err
	}

	selected := make([]selectedSource, 0)
	seenPaths := make(map[string]bool)
	for _, source := range sources {
		path := source.Candidate.SourceRepoPath
		matches := len(groups) == 0 && len(paths) == 0
		matches = matches || slices.Contains(groups, source.Candidate.Scope.Group) || requestedPaths[path]
		if !matches {
			continue
		}
		if source.Candidate.Kind != deployment.FileSecret {
			if requestedPaths[path] {
				return nil, fmt.Errorf("source %q is not a secret", path)
			}
			continue
		}
		selected = append(selected, selectedSource{source: source})
		seenPaths[path] = true
	}
	for path := range requestedPaths {
		if !seenPaths[path] {
			return nil, fmt.Errorf("unknown source %q", path)
		}
	}
	return selected, nil
}

func validateSourcePaths(paths []string) (map[string]bool, error) {
	validated := make(map[string]bool, len(paths))
	for _, path := range paths {
		if _, err := pathsafe.Segments(path); err != nil {
			return nil, fmt.Errorf("invalid source %q: %w", path, err)
		}
		if strings.ContainsRune(path, '\\') {
			return nil, fmt.Errorf("invalid source %q: backslash separator", path)
		}
		if validated[path] {
			return nil, fmt.Errorf("duplicate source %q", path)
		}
		validated[path] = true
	}
	return validated, nil
}

func itemOf(selected selectedSource, status string) Item {
	candidate := selected.source.Candidate
	return Item{
		Source: candidate.SourceRepoPath, Target: selected.source.TargetRelativePath,
		Group: candidate.Scope.Group, Layer: candidate.Layer, Kind: candidate.Kind, Status: status,
	}
}

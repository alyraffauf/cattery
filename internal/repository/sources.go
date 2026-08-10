package repository

import (
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// Source is one repository file and the HOME-relative target it manages.
// It includes base and both platform-layer files, even when an override makes
// a source inactive on the current platform.
type Source struct {
	Candidate          Candidate
	TargetRelativePath string
}

// Sources returns every deployable repository source in deterministic order.
func Sources(root string) ([]Source, error) {
	base, err := Scan(root)
	if err != nil {
		return nil, err
	}
	ignores, err := loadIgnoreMatcher(root)
	if err != nil {
		return nil, err
	}
	sources, err := baseSources(base.Files)
	if err != nil {
		return nil, err
	}
	for _, layer := range []deployment.Layer{deployment.LayerLinux, deployment.LayerDarwin} {
		layerSources, err := platformSources(root, base.Groups, layer, ignores)
		if err != nil {
			return nil, err
		}
		sources = append(sources, layerSources...)
	}
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].Candidate.SourceRepoPath < sources[right].Candidate.SourceRepoPath
	})
	return sources, nil
}

func baseSources(candidates []Candidate) ([]Source, error) {
	sources := make([]Source, 0, len(candidates))
	for _, candidate := range candidates {
		target, err := baseTarget(candidate)
		if err != nil {
			return nil, err
		}
		sources = append(sources, Source{Candidate: candidate, TargetRelativePath: target})
	}
	return sources, nil
}

func platformSources(root string, groups []string, layer deployment.Layer, ignores ignoreMatcher) ([]Source, error) {
	scopes := append([]deployment.Scope{deployment.NewScope("")}, scopesFor(groups)...)
	var sources []Source
	for _, scope := range scopes {
		view, err := scanLayerTree(root, scope, layer, ignores)
		if err != nil {
			return nil, err
		}
		for target, candidate := range view.files {
			sources = append(sources, Source{Candidate: candidate, TargetRelativePath: target})
		}
	}
	return sources, nil
}

func scopesFor(groups []string) []deployment.Scope {
	scopes := make([]deployment.Scope, 0, len(groups))
	for _, group := range groups {
		scopes = append(scopes, deployment.NewScope(group))
	}
	return scopes
}

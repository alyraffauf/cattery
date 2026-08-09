// Package selection owns repository and group resolution for the
// repository-using commands (PLAN.md Section 8.2): explicit --repo path and
// presence, raw CATTERY_REPO and presence, then the default repository of
// the canonical home. Resolution never registers a repository and never
// imports CLI concepts.
package selection

import (
	"fmt"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/state"
)

// RepositoryRequest carries the raw repository inputs a CLI adapter copies
// mechanically: the explicit --repo value and its presence, the raw
// CATTERY_REPO value and its presence, and the initial working directory for
// relative resolution. Presence is significant: an empty value with presence
// blocks fallback.
type RepositoryRequest struct {
	RawExplicit string
	ExplicitSet bool
	RawEnv      string
	EnvSet      bool
	WorkingDir  string
}

// Defaults is the narrow read-only port over the state default lookup.
type Defaults interface {
	DefaultRepository(home string) (state.Repository, error)
}

// RepositoryResolver applies the Section 8.2 precedence and returns the
// canonical repository identity. Explicit and environment paths are resolved
// against the initial working directory; only the default lookup may touch
// state, and it never registers a row.
type RepositoryResolver struct {
	home     string
	defaults Defaults
}

// NewRepositoryResolver constructs a resolver bound to the canonical home
// and the read-only default lookup. Construction performs no filesystem or
// state access.
func NewRepositoryResolver(home string, defaults Defaults) *RepositoryResolver {
	return &RepositoryResolver{home: home, defaults: defaults}
}

// Resolve applies explicit path, then environment, then the canonical-home
// default, failing with instructions when nothing selects a repository.
func (resolver *RepositoryResolver) Resolve(request RepositoryRequest) (state.Repository, error) {
	if request.ExplicitSet {
		if request.RawExplicit == "" {
			return state.Repository{}, fmt.Errorf("selection: explicit repository path is empty")
		}
		return resolver.canonical(request.RawExplicit, request.WorkingDir)
	}
	if request.EnvSet {
		if request.RawEnv == "" {
			return state.Repository{}, fmt.Errorf("selection: CATTERY_REPO is empty")
		}
		return resolver.canonical(request.RawEnv, request.WorkingDir)
	}
	return resolver.defaults.DefaultRepository(resolver.home)
}

// canonical resolves raw against the working directory and returns the
// canonical pair under the resolver's home.
func (resolver *RepositoryResolver) canonical(raw, workingDir string) (state.Repository, error) {
	path := raw
	if !filepath.IsAbs(raw) {
		path = filepath.Join(workingDir, raw)
	}
	root, err := pathsafe.CanonicalRoot(path)
	if err != nil {
		return state.Repository{}, err
	}
	return state.Repository{RootPath: root, HomePath: resolver.home}, nil
}

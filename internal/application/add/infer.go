package add

import (
	"fmt"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// Inference is the frozen ownership of one add target: the scope, layer,
// storage kind, and the repository source destination.
type Inference struct {
	scope                deployment.Scope
	layer                deployment.Layer
	kind                 deployment.FileKind
	targetAbsolutePath   string
	sourceAbsolutePath   string
	sourceRepositoryPath string
}

// Scope returns the frozen scope.
func (i Inference) Scope() deployment.Scope { return i.scope }

// Layer returns the frozen layer.
func (i Inference) Layer() deployment.Layer { return i.layer }

// Kind returns the frozen storage kind.
func (i Inference) Kind() deployment.FileKind { return i.kind }

// TargetAbsolutePath returns the canonical absolute target path.
func (i Inference) TargetAbsolutePath() string { return i.targetAbsolutePath }

// SourceAbsolutePath returns the canonical absolute source destination.
func (i Inference) SourceAbsolutePath() string { return i.sourceAbsolutePath }

// SourceRepositoryPath returns the repository-relative source destination.
func (i Inference) SourceRepositoryPath() string { return i.sourceRepositoryPath }

// InferInput bundles the raw request, one target argument, the compiled
// ownership of the current platform, and the canonical home.
type InferInput struct {
	Request Request
	Target  string
	Plan    deployment.Plan
	Home    string
}

// InferOwnership derives the frozen ownership of one raw target from the
// explicit presence bits and the existing compiled ownership (PLAN.md
// Section 11.6). The derivation is read-only and never inspects target
// bytes or invokes SOPS.
func InferOwnership(input InferInput) (Inference, error) {
	if file, owned := ownedFile(input.Plan, input.Target); owned {
		return managedInference(input, file)
	}
	if _, owned := ownedAlias(input.Plan, input.Target); owned {
		return Inference{}, fmt.Errorf("add: target %q is an alias; add adopts regular files only", input.Target)
	}
	return freshInference(input)
}

// managedInference reuses the compiled ownership of a managed target and
// rejects every explicit option that contradicts it.
func managedInference(input InferInput, file deployment.ManagedFile) (Inference, error) {
	if input.Request.SecretSet && (input.Request.Secret != (file.Kind == deployment.FileSecret)) {
		return Inference{}, fmt.Errorf("add: target %q is already managed as %s", input.Target, file.Kind)
	}
	if input.Request.GroupSet && input.Request.Group != file.Scope.Group {
		return Inference{}, fmt.Errorf("add: target %q is already managed in group %q", input.Target, file.Scope.Group)
	}
	if input.Request.PlatformSet && string(file.Layer) != input.Request.Platform && file.Layer != deployment.LayerBase {
		return Inference{}, fmt.Errorf("add: target %q is already managed on layer %s", input.Target, file.Layer)
	}
	return Inference{
		scope:                file.Scope,
		layer:                file.Layer,
		kind:                 file.Kind,
		targetAbsolutePath:   filepath.Join(input.Home, filepath.FromSlash(input.Target)),
		sourceAbsolutePath:   file.SourceAbsolutePath,
		sourceRepositoryPath: file.SourceRepositoryPath,
	}, nil
}

// freshInference derives the ownership of an unmanaged target from the
// explicit presence bits: root or the named group, base or the current
// platform, and ordinary or secret storage.
func freshInference(input InferInput) (Inference, error) {
	group := ""
	if input.Request.GroupSet {
		group = input.Request.Group
	}
	layer := deployment.LayerBase
	if input.Request.PlatformSet {
		platform, err := deployment.ParseLayer(input.Request.Platform)
		if err != nil {
			return Inference{}, fmt.Errorf("add: unknown platform %q", input.Request.Platform)
		}
		current, err := deployment.ParseLayer(input.Plan.Platform())
		if err != nil || platform != current {
			return Inference{}, fmt.Errorf("add: platform %q is not the current platform %q", input.Request.Platform, input.Plan.Platform())
		}
		layer = platform
	}
	kind := deployment.FileOrdinary
	if input.Request.SecretSet && input.Request.Secret {
		kind = deployment.FileSecret
	}
	source := input.Target
	if group != "" {
		source = group + "/" + input.Target
	}
	return Inference{
		scope:                deployment.NewScope(group),
		layer:                layer,
		kind:                 kind,
		targetAbsolutePath:   filepath.Join(input.Home, filepath.FromSlash(input.Target)),
		sourceAbsolutePath:   filepath.Join(input.Plan.RepositoryRoot(), filepath.FromSlash(source)),
		sourceRepositoryPath: source,
	}, nil
}

// ownedFile finds one compiled file whose target matches the raw target.
func ownedFile(plan deployment.Plan, target string) (deployment.ManagedFile, bool) {
	for _, file := range plan.Files() {
		if file.TargetRelativePath == target {
			return file, true
		}
	}
	return deployment.ManagedFile{}, false
}

// ownedAlias reports whether the raw target is a compiled alias path.
func ownedAlias(plan deployment.Plan, target string) (deployment.Alias, bool) {
	for _, alias := range plan.Aliases() {
		if alias.AliasRelativePath == target {
			return alias, true
		}
	}
	return deployment.Alias{}, false
}

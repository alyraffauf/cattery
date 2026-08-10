// Package validate implements `cattery validate` (PLAN.md Section 11.2): it
// compiles and validates the full repository for Linux and Darwin, checks the
// JSON storage shape of every secret, and reports deterministic counts of the
// selected scopes. The package is Cobra-free: no CLI type appears here, and
// the CLI talks to the service through the frozen Request and Result shapes
// below. No target, SOPS, hook, prompt, or renderer is reachable.
package validate

import (
	"errors"
	"os"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
)

func compileFailure(message string, cause error) error {
	var pathError *os.PathError
	if errors.As(cause, &pathError) {
		return failure.New(failure.Operational, message, cause)
	}
	return failure.New(failure.InvalidInput, message, cause)
}

// Dependencies bundles the injectable seams of the validation service.
// RepositorySource resolves the canonical repository pair for a raw request;
// Compiler compiles and validates platform plans; ProtectedTrees lists the
// trees compiled plans must never target, such as the state directory.
type Dependencies struct {
	RepositorySource RepositorySource
	Compiler         Compiler
	ProtectedTrees   []string
}

// RepositorySource resolves the canonical repository pair for a selection
// request. The composition root satisfies it with a selection resolver bound
// to the canonical home and the state default lookup.
type RepositorySource interface {
	Resolve(selection.RepositoryRequest) (RepositoryIdentity, error)
}

// RepositoryIdentity is the canonical repository pair one validation compiles
// from. It is the validate-owned projection of the lower selection result so
// no backend type leaks through the application seam.
type RepositoryIdentity struct {
	Root string
	Home string
}

// Compiler validates and compiles one platform plan from a repository.
type Compiler interface {
	Compile(repository.CompileInput) (deployment.Plan, error)
}

// RepositoryInput carries the raw repository fields the CLI adapter copies
// mechanically: the explicit --repo value and its presence, the raw
// CATTERY_REPO value and its presence, and the initial working directory for
// relative resolution. Presence is significant: an empty value with presence
// blocks fallback.
type RepositoryInput struct {
	RawExplicit string
	ExplicitSet bool
	RawEnv      string
	EnvSet      bool
	WorkingDir  string
}

// Request is the frozen input of one validation: the raw repository fields
// and the raw ordered group arguments.
type Request struct {
	Repository RepositoryInput
	Groups     []string
}

// PlatformCount summarizes the selected scopes of one compiled platform plan:
// total files, secret files, aliases, and group names.
type PlatformCount struct {
	Platform string
	Files    int
	Secrets  int
	Aliases  int
	Groups   int
}

// Result is the frozen outcome of one validation: the sorted Linux and
// Darwin platform counts.
type Result struct {
	Platforms []PlatformCount
}

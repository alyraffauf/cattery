// Package inspect implements the `cattery status` and `cattery diff`
// evaluation pipeline (PLAN.md Sections 11.3 and 11.4): one immutable
// selection, compile, snapshot, and classification evaluation with on-demand
// secret semantics. The package is Cobra-free: no CLI type appears here, and
// the CLI talks to the service through the frozen Request and Result shapes
// below. No status/diff rendering, hook, prompt, registration, or mutation
// occurs.
package inspect

import (
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

// Dependencies bundles the injectable seams of the inspection service.
// RepositorySource resolves the canonical repository pair for a raw request;
// Compiler compiles and validates one platform plan; State reads the
// persisted rows and the per-installation secret hash key; Secrets performs
// on-demand SOPS decryption; ProtectedTrees lists the trees compiled plans
// must never target; Platform names the layer evaluated ("linux" or
// "darwin").
type Dependencies struct {
	RepositorySource RepositorySource
	Compiler         Compiler
	State            StateReader
	Secrets          *secrets.Client
	ProtectedTrees   []string
	Platform         string
}

// RepositorySource resolves the canonical repository pair for a selection
// request. The composition root satisfies it with a selection resolver bound
// to the canonical home and the state default lookup.
type RepositorySource interface {
	Resolve(selection.RepositoryRequest) (RepositoryIdentity, error)
}

// RepositoryIdentity is the canonical repository pair one inspection
// evaluates from. It is the inspect-owned projection of the lower selection
// result so no backend type leaks through the application seam.
type RepositoryIdentity struct {
	Root string
	Home string
}

// Compiler validates and compiles one platform plan from a repository.
type Compiler interface {
	Compile(repository.CompileInput) (deployment.Plan, error)
}

// StateReader is the narrow read-only port over the persisted rows and the
// per-installation secret hash key of one repository pair. It never
// registers, retires, or mutates rows.
type StateReader interface {
	FileBaselines(root, home string) ([]state.FileBaseline, error)
	AliasBaselines(root, home string) ([]state.AliasBaseline, error)
	RecoverHashKey() ([32]byte, error)
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

// Request is the frozen input of one inspection: the raw repository fields
// and the raw ordered group arguments.
type Request struct {
	Repository RepositoryInput
	Groups     []string
}

// Result is the frozen, opaque outcome of one evaluation: the immutable
// snapshot joined with the on-demand semantic fingerprints and the three
// classifications of every record. Consumers hand it back to the status and
// diff translations without inspecting it.
type Result struct {
	home    string
	records []evaluatedRecord
}

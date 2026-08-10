// Package evaluation owns the shared immutable application evaluation
// pipeline: repository resolution, state selection, plan compilation,
// snapshot assembly, semantic fingerprints, and classifications.
package evaluation

import (
	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/state"
)

// Dependencies are the read-only seams required by one evaluation.
type Dependencies struct {
	RepositorySource RepositorySource
	Compiler         Compiler
	State            StateReader
	Secrets          *secrets.Client
	ProtectedTrees   []string
	Platform         string
	CommandLabel     string

	// IncludeUnmanagedTargetDigest preserves apply's classification of a
	// state-only regular target without a producing plan entry.
	IncludeUnmanagedTargetDigest bool
}

type RepositorySource = applicationrepository.RepositorySource
type RepositoryIdentity = applicationrepository.RepositoryIdentity
type Compiler = applicationrepository.Compiler

// StateReader reads persisted rows and the installation hash key.
type StateReader interface {
	FileBaselines(root, home string) ([]state.FileBaseline, error)
	AliasBaselines(root, home string) ([]state.AliasBaseline, error)
	RecoverHashKey() ([32]byte, error)
}

// RepositoryInput carries raw repository selection fields.
type RepositoryInput = applicationrepository.RepositoryInput

// Request is the shared input of one evaluation.
type Request struct {
	Repository RepositoryInput
	Groups     []string
}

// Record joins one immutable reconciliation evaluation with all classifications
// and its semantic fingerprints.
type Record struct {
	Evaluation reconcile.Evaluation
	File       reconcile.FileClassification
	Alias      reconcile.AliasClassification
	Retirement reconcile.RetirementClassification
	Semantics  reconcile.FileSemantics
}

// Result is the shared outcome consumed by the inspect and apply adapters.
type Result struct {
	RepositoryRoot string
	HomePath       string
	Platform       string
	Hooks          []deployment.Hook
	Records        []Record
}

// All returns a defensive copy of the evaluated records.
func (result Result) All() []Record {
	return append([]Record(nil), result.Records...)
}

// HooksCopy returns a defensive copy of the trusted hooks.
func (result Result) HooksCopy() []deployment.Hook {
	return append([]deployment.Hook(nil), result.Hooks...)
}

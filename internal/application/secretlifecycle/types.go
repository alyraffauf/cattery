// Package secretlifecycle implements safe inventory, verification, and
// recipient/configuration rotation for managed encrypted sources.
package secretlifecycle

import (
	"context"

	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/selection"
)

type RepositoryInput = applicationrepository.RepositoryInput

// Request carries the union selectors and re-encryption authorization flags.
type Request struct {
	Repository RepositoryInput
	Groups     []string
	Sources    []string
	DryRun     bool
	Yes        bool
}

// Item is safe metadata for one encrypted repository source.
type Item struct {
	Source string
	Target string
	Group  string
	Layer  deployment.Layer
	Kind   deployment.FileKind
	Status string
}

// Result records every selected item, including independent failures.
type Result struct {
	Items []Item
}

// SecretClient is the bounded, repository-pinned SOPS role.
type SecretClient interface {
	SetDirectory(string)
	Decrypt(context.Context, []byte, string) ([]byte, error)
	Encrypt(context.Context, []byte, string) ([]byte, error)
}

// AtomicWriter publishes exact bytes over a frozen source.
type AtomicWriter interface {
	ReplaceResult(context.Context, filesystem.Precondition, filesystem.ReplacementSpec) (filesystem.ReplaceResult, error)
}

// SourceBaselineRefresher updates only an active baseline that names a source.
type SourceBaselineRefresher interface {
	RefreshSecretSourceHash(root, home, source string, hash deployment.Digest) (bool, error)
}

type Dependencies struct {
	RepositorySource applicationrepository.RepositorySource
	Secrets          SecretClient
	Writer           AtomicWriter
	Baselines        SourceBaselineRefresher
}

type Service struct{ deps Dependencies }

func NewService(dependencies Dependencies) *Service { return &Service{deps: dependencies} }

func repositoryRequest(input RepositoryInput) selection.RepositoryRequest {
	return selection.RepositoryRequest{
		RawExplicit: input.RawExplicit, ExplicitSet: input.ExplicitSet,
		RawEnv: input.RawEnv, EnvSet: input.EnvSet, WorkingDir: input.WorkingDir,
	}
}

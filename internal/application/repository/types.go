// Package repository contains the application-layer contracts shared by
// repository-oriented services.
package repository

import (
	"github.com/alyraffauf/cattery/internal/deployment"
	backend "github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
)

// RepositorySource resolves the canonical repository pair for a selection request.
type RepositorySource interface {
	Resolve(selection.RepositoryRequest) (RepositoryIdentity, error)
}

// RepositoryIdentity is the canonical repository and home pair.
type RepositoryIdentity struct {
	Root string
	Home string
}

// Compiler validates and compiles one platform plan.
type Compiler interface {
	Compile(backend.CompileInput) (deployment.Plan, error)
}

// RepositoryInput carries raw repository selection fields.
type RepositoryInput struct {
	RawExplicit string
	ExplicitSet bool
	RawEnv      string
	EnvSet      bool
	WorkingDir  string
}

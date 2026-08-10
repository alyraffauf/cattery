// Package initialize implements `cattery init`: it
// creates a missing repository directory, rejects repository/home/state
// overlaps, and registers the canonical pair as the sole default of its home.
// The package is Cobra-free: no CLI type appears here, and the CLI talks to
// the service through the frozen Request and Result shapes below.
package initialize

import "github.com/alyraffauf/cattery/internal/state"

// Dependencies bundles the injectable seams of the initialization service.
// Home must be an existing canonical home root; tests inject it instead of
// reading the developer's environment. Store must be acquired before
// Initialize runs so the service can read the protected state directory.
type Dependencies struct {
	Home  string
	Store *state.Store
}

// Request is the frozen input of one initialization. An empty Path selects
// the current working directory, matching the `cattery init` default.
type Request struct {
	Path string
}

// RegisteredRepository is the application-owned projection of a registered
// repository row. Persistence identifiers and timestamps stay inside state.
type RegisteredRepository struct {
	RootPath  string
	HomePath  string
	IsDefault bool
}

// Result is the frozen outcome of one initialization, carrying the registered
// repository projection so callers can render or chain it without another lookup.
type Result struct {
	Repository RegisteredRepository
}

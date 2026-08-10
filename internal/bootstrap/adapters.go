// Package bootstrap owns the composition root: lazy concrete adapters,
// per-application services, and the opaque CLI application (PLAN.md
// Section 12.1). Nothing here imports Cobra and nothing opens or probes
// backend resources at construction time.
package bootstrap

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/hooks"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

// Adapters bundles the concrete infrastructure adapters of one application.
// Construction never opens the store, launches sops, or touches the
// filesystem; every effect starts inside a service call.
type Adapters struct {
	Store    *state.Store
	Replacer *filesystem.Replacer
	SOPS     *secrets.Client
}

// NewAdapters constructs the lazy infrastructure adapters over the given
// state home, the injected clock, and the sops executable name.
func NewAdapters(stateHome string, now func() time.Time) Adapters {
	return Adapters{
		Store:    state.NewStore(state.Dependencies{StateHome: stateHome, Now: now}),
		Replacer: filesystem.NewReplacer(),
		SOPS:     secrets.NewClient(sopsExecutable, "", nil),
	}
}

// sopsExecutable is the bare sops command resolved from PATH at launch
// time, so constructing the client never probes the system.
const sopsExecutable = "sops"

// compilerAdapter runs the frozen repository compiler.
type compilerAdapter struct{}

// Compile compiles one platform plan from a repository.
func (compilerAdapter) Compile(input repository.CompileInput) (deployment.Plan, error) {
	return repository.Compile(input)
}

// stateReaderAdapter exposes the read-only state port over one store.
type stateReaderAdapter struct {
	store *state.Store
}

// FileBaselines returns the persisted file rows of one repository pair.
func (adapter stateReaderAdapter) FileBaselines(root, home string) ([]state.FileBaseline, error) {
	return adapter.store.FileBaselines(root, home)
}

// AliasBaselines returns the persisted alias rows of one repository pair.
func (adapter stateReaderAdapter) AliasBaselines(root, home string) ([]state.AliasBaseline, error) {
	return adapter.store.AliasBaselines(root, home)
}

// RecoverHashKey loads the per-installation secret hash key.
func (adapter stateReaderAdapter) RecoverHashKey() ([32]byte, error) {
	return adapter.store.RecoverHashKey()
}

// DefaultRepository returns the default repository of one home.
func (adapter stateReaderAdapter) DefaultRepository(home string) (state.Repository, error) {
	return adapter.store.DefaultRepository(home)
}

// baselineAdapter exposes the baseline port over one store.
type baselineAdapter struct {
	store *state.Store
}

// UpsertFileBaseline establishes or replaces one file row.
func (adapter baselineAdapter) UpsertFileBaseline(root, home string, baseline state.FileBaseline) (state.FileBaseline, error) {
	return adapter.store.UpsertFileBaseline(root, home, baseline)
}

// UpsertAliasBaseline establishes or replaces one alias row.
func (adapter baselineAdapter) UpsertAliasBaseline(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error) {
	return adapter.store.UpsertAliasBaseline(root, home, baseline)
}

// transitionAdapter exposes the representation-transition port.
type transitionAdapter struct {
	store *state.Store
}

// TransitionToAlias switches one file row to an alias row.
func (adapter transitionAdapter) TransitionToAlias(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error) {
	return adapter.store.TransitionToAlias(root, home, baseline)
}

// TransitionToFile switches one alias row to a file row.
func (adapter transitionAdapter) TransitionToFile(root, home string, baseline state.FileBaseline) (state.FileBaseline, error) {
	return adapter.store.TransitionToFile(root, home, baseline)
}

// retirementAdapter exposes the retirement port over one store.
type retirementAdapter struct {
	store *state.Store
}

// RetireFileBaseline retires one file row.
func (adapter retirementAdapter) RetireFileBaseline(root, home, target string) (state.FileBaseline, error) {
	return adapter.store.RetireFileBaseline(root, home, target)
}

// RetireAliasBaseline retires one alias row.
func (adapter retirementAdapter) RetireAliasBaseline(root, home, aliasPath string) (state.AliasBaseline, error) {
	return adapter.store.RetireAliasBaseline(root, home, aliasPath)
}

// hookAdapter runs the trusted hooks through the frozen executor.
type hookAdapter struct{}

// Execute runs one ordered hook phase.
func (hookAdapter) Execute(ctx context.Context, input hooks.ExecuteInput, ordered []deployment.Hook) error {
	return hooks.Execute(ctx, input, ordered)
}

// probeAdapter verifies the sops dependency before any write begins.
type probeAdapter struct {
	once   sync.Once
	client *secrets.Client
}

// Probe locates the sops executable in PATH and reports a dependency
// failure when it is missing.
func (probe *probeAdapter) Probe(ctx context.Context) error {
	probe.once.Do(func() {
		path, err := exec.LookPath(sopsExecutable)
		if err != nil {
			probe.client = nil
			return
		}
		probe.client = secrets.NewClient(path, "", nil)
	})
	if probe.client == nil {
		return failure.New(failure.Dependency, "bootstrap: sops executable missing", nil)
	}
	return nil
}

// repositorySource adapts one selection resolver into the per-application
// repository identity of the application.
type repositorySource[T any] struct {
	resolver *selection.RepositoryResolver
	convert  func(state.Repository) T
}

// Resolve maps one raw request into the canonical repository identity.
func (source repositorySource[T]) Resolve(request selection.RepositoryRequest) (T, error) {
	repository, err := source.resolver.Resolve(request)
	var zero T
	if err != nil {
		return zero, err
	}
	return source.convert(repository), nil
}

// LoggerResources bundles the per-application logging state: one level
// variable and one logger bound to it.
type LoggerResources struct {
	Level  *slog.LevelVar
	Logger *slog.Logger
}

// NewLoggerResources builds a logger writing to stderr through the shared
// level variable, without mutating any default logger.
func NewLoggerResources(stderr interface{ Write([]byte) (int, error) }) LoggerResources {
	level := new(slog.LevelVar)
	handler := slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level})
	return LoggerResources{Level: level, Logger: slog.New(handler)}
}

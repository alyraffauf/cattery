package add

import (
	"context"
	"runtime"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
)

// WriteDependencies carries the secret-specific ports required by add's
// execution phases. They remain separate from Task 75's frozen Dependencies
// so the contract owner can finish without a shared-file change.
type WriteDependencies struct {
	Secrets *secrets.Client
	HashKey Recoverer
}

// Recoverer loads the per-installation secret hash key for keyed baselines.
type Recoverer interface {
	RecoverHashKey() ([32]byte, error)
}

// Service performs one add batch against the injectable ports. Construction
// is side-effect-free: every repository, filesystem, secret, and state effect
// happens inside Add. The runtime platform layer is read once at
// construction via runtime.GOOS so an explicit --platform must equal it.
type Service struct {
	deps     Dependencies
	write    WriteDependencies
	platform deployment.Layer
}

// NewService binds the dependencies and resolves the runtime platform layer
// (linux or darwin on a supported host; the zero layer otherwise, which Add
// rejects).
func NewService(deps Dependencies) *Service {
	return newService(deps, WriteDependencies{})
}

// NewServiceWithWrites binds the additional secret execution ports while
// preserving the frozen Task 75 construction seam for ordinary callers.
func NewServiceWithWrites(deps Dependencies, writes WriteDependencies) *Service {
	return newService(deps, writes)
}

func newService(deps Dependencies, writes WriteDependencies) *Service {
	return &Service{deps: deps, write: writes, platform: runtimeLayer()}
}

// runtimeLayer resolves the host platform layer, returning the zero layer
// when the host is neither linux nor darwin.
func runtimeLayer() deployment.Layer {
	layer, err := deployment.ParseLayer(runtime.GOOS)
	if err != nil {
		return ""
	}
	return layer
}

// Add runs the full target-to-repository pipeline for one batch (PLAN.md
// Section 11.6): resolve the repository, compile the current platform, infer
// ownership, preflight the batch, and either report a dry-run plan or write
// sources and baselines sequentially.
func (service *Service) Add(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if service.platform == "" {
		return Result{}, failure.New(failure.InvalidInput, "add: runtime platform must be linux or darwin", nil)
	}
	identity, err := service.resolve(request.Repository)
	if err != nil {
		return Result{}, err
	}
	plan, err := service.compile(identity)
	if err != nil {
		return Result{}, err
	}
	return service.complete(ctx, resolvedBatch{identity: identity, plan: plan}, request)
}

// resolvedBatch bundles the resolved identity and compiled plan of one add.
type resolvedBatch struct {
	identity RepositoryIdentity
	plan     deployment.Plan
}

// resolve maps the raw repository fields to the canonical repository pair.
func (service *Service) resolve(input RepositoryInput) (RepositoryIdentity, error) {
	identity, err := service.deps.RepositorySource.Resolve(repositoryRequest(input))
	if err != nil {
		return RepositoryIdentity{}, failure.New(failure.InvalidInput, "add: resolve repository", err)
	}
	return identity, nil
}

// repositoryRequest copies the raw repository fields into the selection shape.
func repositoryRequest(input RepositoryInput) selection.RepositoryRequest {
	return selection.RepositoryRequest{
		RawExplicit: input.RawExplicit,
		ExplicitSet: input.ExplicitSet,
		RawEnv:      input.RawEnv,
		EnvSet:      input.EnvSet,
		WorkingDir:  input.WorkingDir,
	}
}

// compile validates the repository and returns the current-platform plan.
func (service *Service) compile(identity RepositoryIdentity) (deployment.Plan, error) {
	plan, err := service.deps.Compiler.Compile(repository.CompileInput{
		Platform:       service.platform,
		RepositoryRoot: identity.Root,
		HomeRoot:       identity.Home,
	})
	if err != nil {
		return deployment.Plan{}, failure.New(failure.InvalidInput, "add: compile plan", err)
	}
	return plan, nil
}

// complete canonicalizes targets, infers ownership, preflights the batch, and
// either reports the dry-run plan or executes it.
func (service *Service) complete(ctx context.Context, batch resolvedBatch, request Request) (Result, error) {
	targets, err := resolveTargets(request.Repository.WorkingDir, batch.identity.Home, request.Targets)
	if err != nil {
		return Result{}, err
	}
	items, err := Infer(inferContext{identity: batch.identity, plan: batch.plan, platform: service.platform, targets: targets}, request)
	if err != nil {
		return Result{}, err
	}
	validated, err := Preflight(preflightContext(batch), items)
	if err != nil {
		return Result{}, err
	}
	planned, err := BuildPlan(validated)
	if err != nil {
		return Result{}, err
	}
	if request.DryRun {
		return DryRun(planned), nil
	}
	return service.execute(ctx, batch.identity, planned)
}

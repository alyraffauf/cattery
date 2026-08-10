package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/evaluation"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/secrets"
)

// Service evaluates an apply request before decisions, hooks, or mutations.
type Service struct {
	evaluator   *evaluation.Service
	state       StateReader
	secrets     *secrets.Client
	client      SecretClient
	replacer    AtomicReplacer
	baselines   BaselineStore
	transitions TransitionStore
	retirements RetirementStore
	hooks       HookExecutor
	probe       DependencyProbe
	resolver    DecisionResolver
}

// NewService constructs the apply service over its injected ports.
func NewService(dependencies Dependencies) *Service {
	return &Service{
		evaluator: evaluation.NewService(evaluation.Dependencies{
			RepositorySource:             dependencies.RepositorySource,
			Compiler:                     dependencies.Compiler,
			State:                        dependencies.State,
			Secrets:                      dependencies.Secrets,
			ProtectedTrees:               dependencies.ProtectedTrees,
			Platform:                     dependencies.Platform,
			CommandLabel:                 "apply",
			IncludeUnmanagedTargetDigest: true,
		}),
		state:       dependencies.State,
		secrets:     dependencies.Secrets,
		client:      dependencies.Client,
		replacer:    dependencies.Replacer,
		baselines:   dependencies.Baselines,
		transitions: dependencies.Transitions,
		retirements: dependencies.Retirements,
		hooks:       dependencies.Hooks,
		probe:       dependencies.Probe,
		resolver:    dependencies.Resolver,
	}
}

// Evaluate returns the immutable candidate set for one apply request.
func (service *Service) Evaluate(ctx context.Context, request Request) (Candidates, error) {
	return service.evaluate(ctx, request)
}

func (service *Service) evaluate(ctx context.Context, request Request) (Candidates, error) {
	shared, err := service.evaluator.Evaluate(ctx, evaluation.Request{
		Repository: request.Repository,
		Groups:     request.Groups,
	})
	if err != nil {
		return Candidates{}, err
	}
	records := shared.All()
	candidates := make([]Candidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, Candidate{
			record:     record.Evaluation,
			file:       record.File,
			alias:      record.Alias,
			retirement: record.Retirement,
			semantics:  record.Semantics,
		})
	}
	return Candidates{
		root:     shared.RepositoryRoot,
		home:     shared.HomePath,
		platform: shared.Platform,
		hooks:    shared.HooksCopy(),
		records:  candidates,
	}, nil
}

// Candidate is the apply-owned projection of one shared evaluation record.
// Candidate joins one target evaluation with its classifications and semantic
// fingerprints for the apply phases.
type Candidate struct {
	record     reconcile.Evaluation
	file       reconcile.FileClassification
	alias      reconcile.AliasClassification
	retirement reconcile.RetirementClassification
	semantics  reconcile.FileSemantics
}

// Candidates freezes the evaluated records of one apply in deterministic
// target-path order.
type Candidates struct {
	root     string
	home     string
	platform string
	hooks    []deployment.Hook
	records  []Candidate
}

// Root returns the canonical repository root.
func (c Candidates) Root() string { return c.root }

// Home returns the canonical home root.
func (c Candidates) Home() string { return c.home }

// Platform returns the selected deployment platform.
func (c Candidates) Platform() string { return c.platform }

// Hooks returns a defensive copy of the compiled hooks.
func (c Candidates) Hooks() []deployment.Hook {
	return append([]deployment.Hook(nil), c.hooks...)
}

// All returns a defensive copy of candidates in target-path order.
func (c Candidates) All() []Candidate {
	return append([]Candidate(nil), c.records...)
}

package inspect

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/evaluation"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

type Service struct {
	evaluator *evaluation.Service
}

// NewService constructs the inspection service bound to the dependencies.
func NewService(dependencies Dependencies) *Service {
	return &Service{evaluator: evaluation.NewService(evaluation.Dependencies{
		RepositorySource: dependencies.RepositorySource,
		Compiler:         dependencies.Compiler,
		State:            dependencies.State,
		Secrets:          dependencies.Secrets,
		ProtectedTrees:   dependencies.ProtectedTrees,
		Platform:         dependencies.Platform,
		CommandLabel:     "inspect",
	})}
}

// Evaluate performs one immutable selection, compile, snapshot, and
// classification evaluation with on-demand secret semantics. No status/diff
// rendering, hook, prompt, registration, or mutation
// occurs.
func (service *Service) Evaluate(ctx context.Context, request Request) (Result, error) {
	return service.evaluate(ctx, request)
}

func (service *Service) evaluate(ctx context.Context, request Request) (Result, error) {
	shared, err := service.evaluator.Evaluate(ctx, evaluation.Request{
		Repository: request.Repository,
		Groups:     request.Groups,
	})
	if err != nil {
		return Result{}, err
	}
	records := shared.All()
	evaluated := make([]evaluatedRecord, 0, len(records))
	for _, record := range records {
		evaluated = append(evaluated, evaluatedRecord{
			record:     record.Evaluation,
			file:       record.File,
			alias:      record.Alias,
			retirement: record.Retirement,
		})
	}
	return Result{home: shared.HomePath, records: evaluated}, nil
}

// evaluatedRecord is the inspect-owned projection used by status and diff.
type evaluatedRecord struct {
	record     reconcile.Evaluation
	file       reconcile.FileClassification
	alias      reconcile.AliasClassification
	retirement reconcile.RetirementClassification
}

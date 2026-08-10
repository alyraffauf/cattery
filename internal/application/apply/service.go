package apply

import (
	"context"
)

// Apply performs one complete apply: evaluation, dependency preflight,
// decision collection, plan preparation, the hook-gated filesystem phase
// with the all-source guard, and post-hook verification (PLAN.md Section
// 11.5). Dry runs return the planned records without any hook or write.
// The service contains no phase implementation; every step is a frozen
// phase above.
func (service *Service) Apply(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	candidates, err := service.Evaluate(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if err := service.Preflight(ctx, candidates); err != nil {
		return Result{}, err
	}
	decisions, err := service.CollectDecisions(ctx, candidates)
	if err != nil {
		return Result{}, err
	}
	plan, err := service.Prepare(ctx, PrepareInput{Request: request, Candidates: candidates, Decisions: decisions})
	if err != nil {
		return Result{}, err
	}
	if request.DryRun {
		return Result{Items: plan.Records(), Summary: plan.Summary()}, nil
	}
	return service.execute(ctx, executeInput{request: request, plan: plan, candidates: candidates})
}

// execute runs the hook-gated filesystem phase and post-hook verification,
// preserving the partial records on any error.
func (service *Service) execute(ctx context.Context, input executeInput) (Result, error) {
	records, err := service.RunHookPipeline(ctx, PipelineInput{Request: input.request, Plan: input.plan, Candidates: input.candidates})
	if err != nil {
		return Result{Items: records, Summary: summarize(records)}, err
	}
	records, err = service.Verify(ctx, records, input.candidates)
	if err != nil {
		return Result{Items: records, Summary: summarize(records)}, err
	}
	return Result{Items: records, Summary: summarize(records)}, nil
}

// executeInput bundles the request, plan, and candidates of one execution.
type executeInput struct {
	request    Request
	plan       PreparedPlan
	candidates Candidates
}

// summarize tallies the item records of one apply.
func summarize(records []ItemResult) Summary {
	summary := Summary{}
	for _, record := range records {
		switch record.Status {
		case StatusPlanned:
			summary.Planned++
		case StatusCompleted:
			summary.Completed++
		case StatusPartial:
			summary.Partial++
		}
	}
	return summary
}

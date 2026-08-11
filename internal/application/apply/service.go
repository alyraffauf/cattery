package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
)

// Apply performs one complete apply: evaluation, dependency preflight,
// decision collection, plan preparation, the hook-gated filesystem phase
// with the all-source guard, and post-hook verification. Dry runs return the
// planned records without any hook or write.
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
	decisions, err := service.collectDecisions(ctx, request, candidates)
	if err != nil {
		return Result{}, err
	}
	if err := service.confirmDecisions(ctx, request, decisions); err != nil {
		return Result{}, err
	}
	plan, err := service.Prepare(ctx, PrepareInput{Request: request, Candidates: candidates, Decisions: decisions})
	if err != nil {
		return Result{}, err
	}
	if request.DryRun {
		return service.dryOutcome(plan)
	}
	return service.execute(ctx, executeInput{request: request, plan: plan, candidates: candidates})
}

func (service *Service) confirmDecisions(ctx context.Context, request Request, decisions CollectedDecisions) error {
	if request.Force || len(decisions.All()) == 0 || service.confirmation == nil {
		return nil
	}
	resolutions := make([]Resolution, 0, len(decisions.All()))
	for _, decision := range decisions.All() {
		resolutions = append(resolutions, Resolution{Request: decision.request, Choice: decision.response.Choice})
	}
	confirmed, err := service.confirmation.Confirm(ctx, resolutions)
	if err != nil {
		return err
	}
	if !confirmed {
		return failure.New(failure.Difference, "apply: resolution review declined", nil)
	}
	return nil
}

// dryOutcome freezes the dry-run result, reporting pending changes as a
// difference so the CLI exits 2.
func (service *Service) dryOutcome(plan PreparedPlan) (Result, error) {
	result := Result{Items: plan.Records(), Summary: plan.Summary()}
	if result.Summary.Planned > 0 {
		return result, failure.New(failure.Difference, "apply: dry run reports pending changes", nil)
	}
	return result, nil
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
	return service.outcome(records)
}

// outcome freezes the final result and reports pending items as a
// difference so the CLI exits 2 for skips.
func (service *Service) outcome(records []ItemResult) (Result, error) {
	result := Result{Items: records, Summary: summarize(records)}
	if result.Summary.Planned > 0 {
		return result, failure.New(failure.Difference, "apply: unresolved items remain", nil)
	}
	return result, nil
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

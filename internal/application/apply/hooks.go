package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/hooks"
)

// RunHookPipeline runs the hook-gated apply filesystem phase: before hooks
// with CATTERY_RESULT=pending, the all-source guard and the file and alias
// executors, then after hooks only when the phase completed, with
// CATTERY_RESULT=success or partial (PLAN.md Sections 10.4-10.5). A
// mid-filesystem operational failure skips every after hook, and after
// failures never roll back completed writes.
// PipelineInput bundles the request, plan, and candidates of one
// hook-gated apply phase.
type PipelineInput struct {
	Request    Request
	Plan       PreparedPlan
	Candidates Candidates
}

func (service *Service) RunHookPipeline(ctx context.Context, input PipelineInput) ([]ItemResult, error) {
	records := input.Plan.Records()
	if input.Plan.WithHooks() {
		if err := service.runHooks(ctx, input, hookRequest{phase: deployment.HookBefore, result: "pending"}); err != nil {
			return nil, failure.New(failure.Hook, "apply: before hooks failed", err)
		}
	}
	files, err := service.ExecuteFiles(ctx, input.Plan, input.Candidates)
	records = append(records, files...)
	if err != nil {
		return records, err
	}
	aliases, err := service.ExecuteAliases(ctx, input.Plan, input.Candidates)
	records = append(records, aliases...)
	if err != nil {
		return records, err
	}
	if input.Plan.WithHooks() {
		return service.runAfterHooks(ctx, input, records)
	}
	return records, nil
}

// runAfterHooks aggregates the after phase with success or partial.
func (service *Service) runAfterHooks(ctx context.Context, input PipelineInput, records []ItemResult) ([]ItemResult, error) {
	phase := deployment.HookAfter
	result := "success"
	if hasSkipped(records) {
		result = "partial"
	}
	if err := service.runHooks(ctx, input, hookRequest{phase: phase, result: result}); err != nil {
		return records, failure.New(failure.Hook, "apply: after hooks failed", err)
	}
	return records, nil
}

// hookRequest bundles one hook phase and its result value.
type hookRequest struct {
	phase  deployment.HookPhase
	result string
}

// hasSkipped reports whether any record stayed planned, which happens only
// when the user skipped one or more unresolved items.
func hasSkipped(records []ItemResult) bool {
	for _, record := range records {
		if record.Status == StatusPlanned {
			return true
		}
	}
	return false
}

// runHooks executes one ordered hook phase with the exact result value.
func (service *Service) runHooks(ctx context.Context, input PipelineInput, request hookRequest) error {
	execution := hooks.ExecuteInput{
		RepositoryRoot: input.Candidates.Root(),
		HomePath:       input.Candidates.Home(),
		Platform:       input.Candidates.Platform(),
		Phase:          request.phase,
		Result:         request.result,
		DryRun:         input.Request.DryRun,
		NoHooks:        input.Request.NoHooks,
	}
	return service.hooks.Execute(ctx, execution, input.Candidates.Hooks())
}

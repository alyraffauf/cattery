package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/hooks"
)

const (
	hookResultPending = "pending"
	hookResultSuccess = "success"
	hookResultPartial = "partial"
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
		if err := service.runHooks(ctx, input, hookRequest{phase: deployment.HookBefore, result: hookResultPending}); err != nil {
			return nil, failure.New(failure.Hook, "apply: before hooks failed", err)
		}
	}
	if err := service.Revalidate(ctx, input.Candidates); err != nil {
		return nil, err
	}
	records, err := service.runExecutors(ctx, input, records)
	if err != nil {
		return records, err
	}
	if input.Plan.WithHooks() {
		return service.runAfterHooks(ctx, input, records)
	}
	return records, nil
}

// runExecutors performs the all-source guarded file and alias phases.
func (service *Service) runExecutors(ctx context.Context, input PipelineInput, records []ItemResult) ([]ItemResult, error) {
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
	return records, nil
}

// runAfterHooks aggregates the after phase with success or partial.
func (service *Service) runAfterHooks(ctx context.Context, input PipelineInput, records []ItemResult) ([]ItemResult, error) {
	phase := deployment.HookAfter
	result := hookResultSuccess
	if hasSkipped(records) {
		result = hookResultPartial
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
	}
	return service.hooks.Execute(ctx, execution, hooksForPhase(input.Candidates.Hooks(), request.phase))
}

// hooksForPhase keeps the hooks of one phase in the execution order, since
// the compiled plan carries the display order.
func hooksForPhase(all []deployment.Hook, phase deployment.HookPhase) []deployment.Hook {
	kept := make([]deployment.Hook, 0, len(all))
	for _, hook := range all {
		if hook.Phase == phase {
			kept = append(kept, hook)
		}
	}
	if phase == deployment.HookBefore {
		hooks.SortBefore(kept)
		return kept
	}
	hooks.SortAfter(kept)
	return kept
}

package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/state"
)

// ExecuteAliases runs the alias and retirement actions of one apply: each
// alias is created or replaced, file-alias representation transitions
// switch active state only after the durable write, and retirement tracks
// source removal without deleting targets.
func (service *Service) ExecuteAliases(ctx context.Context, plan PreparedPlan, candidates Candidates) ([]ItemResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	byPath := candidatesByPath(candidates)
	results := make([]ItemResult, 0)
	for _, action := range plan.Actions().Items() {
		if action.Kind != ActionKindRealizeAlias && action.Kind != ActionKindRetireFile && action.Kind != ActionKindRetireAlias {
			continue
		}
		job := aliasJob{action: action, candidate: byPath[action.TargetPath], root: candidates.Root(), home: candidates.Home()}
		record, err := service.executeAliasAction(ctx, job)
		results = append(results, record)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

// aliasJob bundles one alias or retirement action with its candidate and
// the repository pair locations.
type aliasJob struct {
	action    PlanAction
	candidate Candidate
	root      string
	home      string
}

// executeAliasAction runs one alias, transition, or retirement action.
func (service *Service) executeAliasAction(ctx context.Context, job aliasJob) (ItemResult, error) {
	switch job.action.Kind {
	case ActionKindRealizeAlias:
		return service.realizeAlias(ctx, job)
	case ActionKindRetireFile:
		return service.retireFile(job)
	case ActionKindRetireAlias:
		return service.retireAlias(job)
	}
	return ItemResult{}, nil
}

// realizeAlias writes the exact relative payload, then switches the active
// representation only when the link is durable.
func (service *Service) realizeAlias(ctx context.Context, job aliasJob) (ItemResult, error) {
	precondition, err := filesystem.Freeze(filesystem.Destination{Root: job.home, Relative: job.action.TargetPath})
	if err != nil {
		return aliasRecord(job, StatusPartial), failure.New(failure.Operational, "apply: freeze alias "+job.action.TargetPath, err)
	}
	payload, err := pathsafe.RelativeAliasPayload(
		job.candidate.record.Alias.CanonicalTargetRelativePath,
		job.candidate.record.Alias.AliasRelativePath,
	)
	if err != nil {
		return aliasRecord(job, StatusPartial), failure.New(failure.InvalidInput, "apply: payload for "+job.action.TargetPath, err)
	}
	if _, err := service.replacer.RealizeAlias(ctx, precondition, filesystem.AliasSpec{Payload: payload, Overwrite: job.action.Overwrite}); err != nil {
		return aliasRecord(job, StatusPartial), failure.New(failure.Operational, "apply: realize alias "+job.action.TargetPath, err)
	}
	if err := service.commitAliasState(ctx, job); err != nil {
		return aliasRecord(job, StatusPartial), err
	}
	return aliasRecord(job, StatusCompleted), nil
}

// commitAliasState switches the retained file row to the alias
// representation or upserts the alias baseline, only after a durable link.
func (service *Service) commitAliasState(ctx context.Context, job aliasJob) error {
	record := job.candidate.record
	if record.FileState != nil && record.FileState.Active() {
		_, err := service.transitions.TransitionToAlias(job.root, job.home, aliasRow(record))
		if err != nil {
			return failure.New(failure.Operational, "apply: transition to alias "+job.action.TargetPath, err)
		}
		return nil
	}
	_, err := service.baselines.UpsertAliasBaseline(job.root, job.home, aliasRow(record))
	if err != nil {
		return failure.New(failure.Operational, "apply: baseline alias "+job.action.TargetPath, err)
	}
	return nil
}

// aliasRow freezes the active alias baseline of one realized link.
func aliasRow(record reconcile.Evaluation) state.AliasBaseline {
	return state.AliasBaseline{
		AliasPath:           record.Alias.AliasRelativePath,
		CanonicalTargetPath: record.Alias.CanonicalTargetRelativePath,
		GroupName:           record.Alias.Scope.Group,
		Layer:               state.AliasLayer(record.Alias.Platform),
		Status:              state.StatusActive,
	}
}

// retireFile tracks the removal of one source without deleting its target.
func (service *Service) retireFile(job aliasJob) (ItemResult, error) {
	_, err := service.retirements.RetireFileBaseline(job.root, job.home, job.action.TargetPath)
	if err != nil {
		return aliasRecord(job, StatusPartial), failure.New(failure.Operational, "apply: retire file "+job.action.TargetPath, err)
	}
	return aliasRecord(job, StatusCompleted), nil
}

// retireAlias tracks the removal of one alias source without deleting the
// link.
func (service *Service) retireAlias(job aliasJob) (ItemResult, error) {
	_, err := service.retirements.RetireAliasBaseline(job.root, job.home, job.action.TargetPath)
	if err != nil {
		return aliasRecord(job, StatusPartial), failure.New(failure.Operational, "apply: retire alias "+job.action.TargetPath, err)
	}
	return aliasRecord(job, StatusCompleted), nil
}

// aliasRecord marks one alias or retirement outcome.
func aliasRecord(job aliasJob, status ItemStatus) ItemResult {
	return ItemResult{
		TargetPath: job.action.TargetPath,
		Status:     status,
		Kind:       job.action.Kind,
	}
}

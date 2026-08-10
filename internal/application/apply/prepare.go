package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// PreparedPlan freezes one apply: the ordered execution actions, the
// per-target records for rendering, and whether trusted hooks run.
type PreparedPlan struct {
	actions   ActionPlan
	records   []ItemResult
	withHooks bool
}

// Actions returns the ordered execution actions.
func (p PreparedPlan) Actions() ActionPlan { return p.actions }

// Records returns a defensive copy of the per-target records.
func (p PreparedPlan) Records() []ItemResult {
	return append([]ItemResult(nil), p.records...)
}

// WithHooks reports whether trusted hooks run for this plan.
func (p PreparedPlan) WithHooks() bool { return p.withHooks }

// Summary counts the per-target records of the plan.
func (p PreparedPlan) Summary() Summary {
	return summarize(p.records)
}

// PrepareInput bundles the request, candidates, and decisions of one apply
// preparation.
type PrepareInput struct {
	Request    Request
	Candidates Candidates
	Decisions  CollectedDecisions
}

// Prepare combines the resolved candidates and decisions into one immutable
// action plan with dry-run records and stable per-target kinds. No hook or
// managed mutation occurs, and refusal paths
// register nothing.
func (service *Service) Prepare(ctx context.Context, input PrepareInput) (PreparedPlan, error) {
	if err := ctx.Err(); err != nil {
		return PreparedPlan{}, err
	}
	pending := input.Decisions.Specs()
	if pending == nil {
		var err error
		pending, err = decisionSpecs(input.Candidates)
		if err != nil {
			return PreparedPlan{}, err
		}
	}
	if len(pending) > 0 && !input.Request.DryRun && input.Request.NonInteractive {
		return PreparedPlan{}, failure.New(failure.InvalidInput, "apply: non-interactive apply requires no pending decisions", nil)
	}
	actions, records, err := prepareActions(input.Candidates, decisionChoices(input.Decisions), input.Request.DryRun)
	if err != nil {
		return PreparedPlan{}, err
	}
	return PreparedPlan{
		actions:   NewActionPlan(actions),
		records:   records,
		withHooks: !input.Request.DryRun && !input.Request.NoHooks && len(input.Candidates.Hooks()) > 0,
	}, nil
}

// decisionChoices indexes the resolved decisions by target path.
func decisionChoices(decisions CollectedDecisions) map[string]DecisionChoice {
	choices := make(map[string]DecisionChoice, len(decisions.All()))
	for _, decision := range decisions.All() {
		choices[decision.request.TargetPath()] = decision.response.Choice
	}
	return choices
}

// prepareActions derives the ordered actions and records of one apply.
func prepareActions(candidates Candidates, choices map[string]DecisionChoice, dryRun bool) ([]PlanAction, []ItemResult, error) {
	actions := make([]PlanAction, 0)
	records := make([]ItemResult, 0)
	scope := prepareScope{choices: choices, dryRun: dryRun}
	for _, candidate := range candidates.All() {
		execute, kind, source, overwrite, kept, err := prepareOne(candidate, records, scope)
		if err != nil {
			return nil, nil, err
		}
		records = kept
		if execute {
			actions = append(actions, PlanAction{TargetPath: candidate.record.TargetPath, Kind: kind, SourcePath: source, Overwrite: overwrite})
		}
	}
	return actions, records, nil
}

// prepareScope bundles the per-preparation decision map and dry-run policy.
type prepareScope struct {
	choices map[string]DecisionChoice
	dryRun  bool
}

// prepareOne resolves one candidate into a skipped or dry-run record, or
// reports that its pending action executes.
func prepareOne(candidate Candidate, records []ItemResult, scope prepareScope) (bool, ActionKind, string, bool, []ItemResult, error) {
	action, source, err := underlyingAction(candidate)
	if err != nil {
		return false, "", "", false, records, err
	}
	kind, has := mapKind(action)
	if !has {
		return false, "", "", false, records, nil
	}
	choice, decided := scope.choices[candidate.record.TargetPath]
	if decided && choice == ChoiceSkip {
		return false, kind, "", false, append(records, plannedRecord(candidate, kind)), nil
	}
	if candidateNeedsDecision(candidate) && !decided && !scope.dryRun {
		return false, kind, "", false, records, failure.New(failure.InvalidInput, "apply: unresolved decision for "+candidate.record.TargetPath, nil)
	}
	if scope.dryRun {
		return false, kind, "", false, append(records, plannedRecord(candidate, kind)), nil
	}
	return true, kind, source, confirmedReplace(candidate, decided, choice), records, nil
}

// confirmedReplace reports whether the action may replace an occupied
// alias path: a decided overwrite or an automatic intact representation
// transition.
func confirmedReplace(candidate Candidate, decided bool, choice DecisionChoice) bool {
	if decided && choice == ChoiceOverwrite {
		return true
	}
	return candidate.record.Entry == reconcile.PlanEntryAlias && candidate.record.FileState != nil && candidate.record.FileState.Active()
}

func candidateNeedsDecision(candidate Candidate) bool {
	return candidate.file.Convergence == reconcile.ConvergenceDecisionRequired || candidate.alias.Convergence == reconcile.ConvergenceDecisionRequired
}

// plannedRecord builds the planned per-target record of one candidate.
func plannedRecord(candidate Candidate, kind ActionKind) ItemResult {
	return ItemResult{
		TargetPath: candidate.record.TargetPath,
		Status:     StatusPlanned,
		Kind:       kind,
	}
}

// underlyingAction resolves the intended action of one candidate: the
// classification action for automatic rows, the representation named by the
// plan entry for rows that required a decision.
func underlyingAction(candidate Candidate) (reconcile.Action, string, error) {
	decided := candidateNeedsDecision(candidate)
	if !decided {
		if action, source, pending := classificationAction(candidate); pending {
			return action, source, nil
		}
		return reconcile.ActionNoOp, "", nil
	}
	switch candidate.record.Entry {
	case reconcile.PlanEntryFile:
		return reconcile.ActionWriteSourceToTarget, candidate.record.File.SourceRepositoryPath, nil
	case reconcile.PlanEntryAlias:
		return reconcile.ActionCreateAlias, "", nil
	}
	return reconcile.ActionNoOp, "", nil
}

// classificationAction maps the automatic classifications to one action.
func classificationAction(candidate Candidate) (reconcile.Action, string, bool) {
	switch {
	case candidate.file.Action == reconcile.ActionCreateTarget || candidate.file.Action == reconcile.ActionWriteSourceToTarget || candidate.file.Action == reconcile.ActionCorrectMode:
		return candidate.file.Action, candidate.record.File.SourceRepositoryPath, true
	case candidate.alias.Action == reconcile.ActionCreateAlias || candidate.alias.Action == reconcile.ActionReplaceAlias || candidate.alias.Action == reconcile.ActionVerifyAlias:
		return candidate.alias.Action, "", true
	case candidate.retirement.Action == reconcile.ActionRetireFileState:
		return reconcile.ActionRetireFileState, "", true
	case candidate.retirement.Action == reconcile.ActionRetireAliasState:
		return reconcile.ActionRetireAliasState, "", true
	}
	return reconcile.ActionNoOp, "", false
}

// mapKind projects one reconcile action into the apply action vocabulary.
func mapKind(action reconcile.Action) (ActionKind, bool) {
	switch action {
	case reconcile.ActionCreateTarget, reconcile.ActionWriteSourceToTarget, reconcile.ActionEstablishBaseline:
		return ActionKindWriteSource, true
	case reconcile.ActionCorrectMode:
		return ActionKindReplaceFile, true
	case reconcile.ActionCreateAlias, reconcile.ActionReplaceAlias, reconcile.ActionVerifyAlias:
		return ActionKindRealizeAlias, true
	case reconcile.ActionRetireFileState:
		return ActionKindRetireFile, true
	case reconcile.ActionRetireAliasState:
		return ActionKindRetireAlias, true
	}
	return "", false
}

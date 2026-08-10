package apply

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// ResolvedDecision pairs one decision request with its validated response.
type ResolvedDecision struct {
	request  DecisionRequest
	response DecisionResponse
}

// CollectedDecisions freezes the resolved decisions in target-path order.
type CollectedDecisions struct {
	decisions []ResolvedDecision
	specs     []reconcile.DecisionSpec
}

// All returns the resolved decisions in bytewise target-path order.
func (c CollectedDecisions) All() []ResolvedDecision {
	return append([]ResolvedDecision(nil), c.decisions...)
}

// Specs returns the decision specifications collected for this apply.
func (c CollectedDecisions) Specs() []reconcile.DecisionSpec {
	return append([]reconcile.DecisionSpec(nil), c.specs...)
}

// CollectDecisions resolves every candidate that requires an explicit
// decision, in bytewise target-path order, and validates each response
// before any hook or mutation. An abort answer stops
// the whole apply; a diff answer re-requests the adapter, which shows the
// safe difference and asks again.
func (service *Service) CollectDecisions(ctx context.Context, candidates Candidates) (CollectedDecisions, error) {
	if err := ctx.Err(); err != nil {
		return CollectedDecisions{}, err
	}
	specs, err := decisionSpecs(candidates)
	if err != nil {
		return CollectedDecisions{}, err
	}
	ordered := reconcile.OrderedDecisionSpecs(specs)
	decisions := make([]ResolvedDecision, 0, len(ordered))
	for _, spec := range ordered {
		decision, err := service.collectOne(ctx, spec, candidates)
		if err != nil {
			return CollectedDecisions{}, err
		}
		decisions = append(decisions, decision)
	}
	return CollectedDecisions{decisions: decisions, specs: ordered}, nil
}

// collectOne projects one spec into a request, resolves it, and validates
// the response before any hook or mutation.
func (service *Service) collectOne(ctx context.Context, spec reconcile.DecisionSpec, candidates Candidates) (ResolvedDecision, error) {
	request, err := NewDecisionRequest(DecisionRequestInput{
		TargetPath: spec.TargetPath(),
		Choices:    projectChoices(spec.AllChoices()),
	})
	if err != nil {
		return ResolvedDecision{}, failure.New(failure.InvalidInput, "apply: project decision request", err)
	}
	response, err := service.resolveRepeatedly(ctx, request, service.differenceProvider(candidates))
	if err != nil {
		return ResolvedDecision{}, err
	}
	if response.Choice == ChoiceAbort {
		return ResolvedDecision{}, failure.New(failure.Difference, "apply: aborted by user", nil)
	}
	return ResolvedDecision{request: request, response: response}, nil
}

// decisionSpecs collects the frozen specs of every candidate that requires
// an explicit decision, in evaluation order.
func decisionSpecs(candidates Candidates) ([]reconcile.DecisionSpec, error) {
	records := candidates.All()
	specs := make([]reconcile.DecisionSpec, 0, len(records))
	for _, candidate := range records {
		spec, err := specFor(candidate)
		if err != nil {
			return nil, err
		}
		if spec != nil {
			specs = append(specs, *spec)
		}
	}
	return specs, nil
}

// specFor freezes the decision spec of one candidate, or returns nil when
// the candidate does not require a decision.
func specFor(candidate Candidate) (*reconcile.DecisionSpec, error) {
	if candidate.file.Convergence == reconcile.ConvergenceDecisionRequired {
		spec, err := reconcile.DecisionSpecForFile(candidate.file, candidate.record.File.Kind)
		if err != nil {
			return nil, failure.New(failure.InvalidInput, "apply: freeze file decision", err)
		}
		return &spec, nil
	}
	if candidate.alias.Convergence == reconcile.ConvergenceDecisionRequired {
		spec, err := reconcile.DecisionSpecForAlias(candidate.alias)
		if err != nil {
			return nil, failure.New(failure.InvalidInput, "apply: freeze alias decision", err)
		}
		return &spec, nil
	}
	return nil, nil
}

// projectChoices copies the reconcile choices into the application-owned
// decision vocabulary.
func projectChoices(choices []reconcile.DecisionChoice) []DecisionChoice {
	projected := make([]DecisionChoice, 0, len(choices))
	for _, choice := range choices {
		projected = append(projected, projectChoice(choice))
	}
	return projected
}

// projectChoice maps one reconcile choice to its application-owned name.
func projectChoice(choice reconcile.DecisionChoice) DecisionChoice {
	switch choice {
	case reconcile.ChoiceOverwrite:
		return ChoiceOverwrite
	case reconcile.ChoiceSkip:
		return ChoiceSkip
	case reconcile.ChoiceAbort:
		return ChoiceAbort
	case reconcile.ChoiceDiff:
		return ChoiceDiff
	}
	return ""
}

// resolveRepeatedly asks the resolver until it returns a final choice,
// re-requesting when it answers diff so the adapter can show the safe
// difference and ask again.
func (service *Service) resolveRepeatedly(ctx context.Context, request DecisionRequest, difference DifferenceProvider) (DecisionResponse, error) {
	for {
		if err := ctx.Err(); err != nil {
			return DecisionResponse{}, err
		}
		response, err := service.resolveOnce(decisionResolution{context: ctx, request: request, difference: difference})
		if err != nil {
			return DecisionResponse{}, err
		}
		if response.Choice != ChoiceDiff {
			return response, nil
		}
	}
}

// resolveOnce asks the resolver once and validates its response against the
// allowed choices of the request.
type decisionResolution struct {
	context    context.Context
	request    DecisionRequest
	difference DifferenceProvider
}

func (service *Service) resolveOnce(input decisionResolution) (DecisionResponse, error) {
	ctx, request, difference := input.context, input.request, input.difference
	if service.resolver == nil {
		return DecisionResponse{}, failure.New(failure.Operational, "apply: decision resolver is unavailable", nil)
	}
	response, err := resolveDecision(decisionResolutionInput{
		resolver: service.resolver, context: ctx, request: request, difference: difference,
	})
	if err != nil {
		return DecisionResponse{}, err
	}
	for _, choice := range request.Choices() {
		if choice == response.Choice {
			return response, nil
		}
	}
	return DecisionResponse{}, failure.New(failure.InvalidInput, "apply: invalid decision response for "+request.TargetPath(), nil)
}

type decisionResolutionInput struct {
	resolver   DecisionResolver
	context    context.Context
	request    DecisionRequest
	difference DifferenceProvider
}

func resolveDecision(input decisionResolutionInput) (DecisionResponse, error) {
	if differenceResolver, ok := input.resolver.(DifferenceResolver); ok {
		return differenceResolver.ResolveWithDifference(input.context, input.request, input.difference)
	}
	return input.resolver.Resolve(input.context, input.request)
}

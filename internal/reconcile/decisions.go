package reconcile

import (
	"fmt"
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// decisionReason reports whether reason names a row that requires an
// explicit user decision: file drift, unbaselined mismatch, and conflict
// rows never apply silently, and unexpected target types, alias occupation,
// and representation transitions always prompt (PLAN.md Sections 9.2, 9.3,
// and 9.5).
func decisionReason(reason Reason) bool {
	switch reason {
	case ReasonTargetDrift, ReasonConflict, ReasonUnbaselinedDiffer, ReasonUnexpectedTargetType,
		ReasonAliasWrong, ReasonAliasOccupied, ReasonRepresentationDrift:
		return true
	}
	return false
}

// diffEligible reports whether a decision may compare source and target
// bytes: only the ordinary file drift rows do. Secrets never expose a diff
// prompt, and an unexpected-type symlink target has no bytes to compare
// (PLAN.md Sections 9.4 and 9.6).
func diffEligible(reason Reason, kind deployment.FileKind) bool {
	if kind != deployment.FileOrdinary {
		return false
	}
	switch reason {
	case ReasonTargetDrift, ReasonConflict, ReasonUnbaselinedDiffer:
		return true
	}
	return false
}

// AllowedChoices returns the ordered choices one decision prompt may offer
// for the action/reason pair and source kind: every decision offers
// overwrite, skip, and abort, and a diff choice precedes them only for an
// ordinary file decision that compares exact bytes. A pair that does not
// require a decision yields no choices.
func AllowedChoices(action Action, reason Reason, kind deployment.FileKind) []DecisionChoice {
	if action != ActionNeedsDecision || !decisionReason(reason) {
		return nil
	}
	if diffEligible(reason, kind) {
		return []DecisionChoice{ChoiceDiff, ChoiceOverwrite, ChoiceSkip, ChoiceAbort}
	}
	return []DecisionChoice{ChoiceOverwrite, ChoiceSkip, ChoiceAbort}
}

// DecisionSpecForFile freezes one file classification that requires a
// decision into an immutable spec carrying exactly the choices allowed for
// its action, reason, and source kind. Classifications that do not require
// a decision are rejected: converged, pending, and rejected outcomes never
// prompt (PLAN.md Sections 9.2 and 9.3).
func DecisionSpecForFile(classification FileClassification, kind deployment.FileKind) (DecisionSpec, error) {
	if classification.Convergence != ConvergenceDecisionRequired {
		return DecisionSpec{}, fmt.Errorf("reconcile: file %q does not require a decision", classification.TargetPath)
	}
	return NewDecisionSpec(DecisionSpecInput{
		TargetPath: classification.TargetPath, Action: classification.Action,
		Reason:  classification.Reason,
		Choices: AllowedChoices(classification.Action, classification.Reason, kind),
	})
}

// DecisionSpecForAlias freezes one alias or representation classification
// that requires a decision into an immutable spec. Alias prompts never offer
// diff: an occupied path or drifted representation compares no target bytes
// (PLAN.md Sections 5.4 and 9.5). Alias reasons never qualify for diff, so
// the kind argument is irrelevant to the eligibility call.
func DecisionSpecForAlias(classification AliasClassification) (DecisionSpec, error) {
	if classification.Convergence != ConvergenceDecisionRequired {
		return DecisionSpec{}, fmt.Errorf("reconcile: alias %q does not require a decision", classification.TargetPath)
	}
	return NewDecisionSpec(DecisionSpecInput{
		TargetPath: classification.TargetPath, Action: classification.Action,
		Reason:  classification.Reason,
		Choices: AllowedChoices(classification.Action, classification.Reason, deployment.FileOrdinary),
	})
}

// ValidateDecisionSpec rejects any spec whose action and reason cannot
// prompt or whose choices are not exactly the allowed set for its action,
// reason, and source kind (PLAN.md Section 9.4).
func ValidateDecisionSpec(spec DecisionSpec, kind deployment.FileKind) error {
	allowed := AllowedChoices(spec.Action(), spec.Reason(), kind)
	if len(allowed) == 0 {
		return fmt.Errorf("reconcile: decision for %q is not allowed for action %d and reason %d",
			spec.TargetPath(), spec.Action(), spec.Reason())
	}
	if !equalChoices(spec.AllChoices(), allowed) {
		return fmt.Errorf("reconcile: decision for %q does not offer exactly the allowed choices", spec.TargetPath())
	}
	return nil
}

// equalChoices reports whether actual matches allowed choice for choice.
func equalChoices(actual, allowed []DecisionChoice) bool {
	if len(actual) != len(allowed) {
		return false
	}
	for index := range actual {
		if actual[index] != allowed[index] {
			return false
		}
	}
	return true
}

// OrderedDecisionSpecs returns a defensive copy of the specs sorted bytewise
// by target path, so prompts always follow normalized target-path order
// (PLAN.md Section 9.4).
func OrderedDecisionSpecs(specs []DecisionSpec) []DecisionSpec {
	ordered := append([]DecisionSpec(nil), specs...)
	sort.SliceStable(ordered, func(first, second int) bool {
		return ordered[first].TargetPath() < ordered[second].TargetPath()
	})
	return ordered
}

package reconcile

// RetirementClassification is one immutable classification of a state row
// without a plan producer at its path: the tracking action, its reason, and
// the convergence class. Retirement changes tracking only and never carries
// a target action.
type RetirementClassification struct {
	TargetPath  string
	Action      Action
	Reason      Reason
	Convergence Convergence
}

// ClassifyRetirement purely classifies one state-only evaluation record
// (PLAN.md Sections 8.5 and 9.5): an active row whose target has no producer
// anywhere in the complete current platform plan retires tracking only, never
// the target. Rows on an inactive platform layer stay active for this
// platform; rows already retired stay put. Records with a plan producer or
// without any row return the zero classification and belong to the file and
// alias classifiers.
func ClassifyRetirement(record Evaluation, platform string) RetirementClassification {
	base := RetirementClassification{TargetPath: record.TargetPath}
	if record.Entry != PlanEntryNone {
		return base
	}
	row, alias := record.FileState, record.AliasState
	switch {
	case row != nil && row.Active():
		return fileRetirement(record, platform)
	case alias != nil && alias.Active():
		return aliasRetirement(record, platform)
	case row == nil && alias == nil:
		return base
	}
	return withRetirementPath(retirementOutcome(ActionNoOp, ReasonAlreadyRetired, ConvergenceConverged), record)
}

// fileRetirement classifies one active file row without a producer: tracking
// retirement on the current platform, no action on an inactive platform
// layer.
func fileRetirement(record Evaluation, platform string) RetirementClassification {
	if record.FileState.Layer().InactiveOn(platform) {
		return withRetirementPath(retirementOutcome(ActionNoOp, ReasonInactivePlatform, ConvergenceConverged), record)
	}
	return withRetirementPath(retirementOutcome(ActionRetireFileState, ReasonSourceRemoved, ConvergencePending), record)
}

// aliasRetirement classifies one active alias row without a producer.
func aliasRetirement(record Evaluation, platform string) RetirementClassification {
	if record.AliasState.Layer().InactiveOn(platform) {
		return withRetirementPath(retirementOutcome(ActionNoOp, ReasonInactivePlatform, ConvergenceConverged), record)
	}
	return withRetirementPath(retirementOutcome(ActionRetireAliasState, ReasonSourceRemoved, ConvergencePending), record)
}

// retirementOutcome builds a bare classification from its three enum fields.
func retirementOutcome(action Action, reason Reason, convergence Convergence) RetirementClassification {
	return RetirementClassification{Action: action, Reason: reason, Convergence: convergence}
}

// withRetirementPath attaches the evaluation target path to a bare outcome.
func withRetirementPath(candidate RetirementClassification, record Evaluation) RetirementClassification {
	candidate.TargetPath = record.TargetPath
	return candidate
}

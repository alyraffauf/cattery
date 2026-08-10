package reconcile

import (
	"fmt"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

// AliasClassification is one immutable classification of an alias or
// representation transition at one path: the action, its reason, and the
// convergence class of the path.
type AliasClassification struct {
	TargetPath  string
	Action      Action
	Reason      Reason
	Convergence Convergence
}

// ClassifyAlias purely classifies one alias or representation evaluation. A
// plan alias classifies against its exact
// relative payload; a plan alias over an active file row, or a plan file over
// an active alias row, classifies the representation transition. The current
// target semantic fingerprint arrives precomputed because a secret file row
// compares keyed digests that the pure classifier must never derive. Records
// without an alias producer or an active opposite row return the zero
// classification and belong to the file and retirement classifiers.
func ClassifyAlias(record Evaluation, semantics FileSemantics) AliasClassification {
	switch {
	case record.Entry == PlanEntryFile && activeAliasRow(record):
		return classifyAliasToFile(record)
	case record.Entry != PlanEntryAlias:
		return AliasClassification{TargetPath: record.TargetPath}
	case activeFileRow(record):
		return classifyFileToAlias(record, semantics)
	}
	return classifyAliasEntry(record)
}

// classifyAliasEntry classifies a plan alias without an active file row: a
// missing link is recreated, an exact relative link is a no-op or an
// unbaselined adoption, and every other entry is an occupied alias path that
// requires an explicit decision or manual intervention.
func classifyAliasEntry(record Evaluation) AliasClassification {
	switch record.Target.Kind() {
	case KindDirectory, KindSpecial:
		return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonAliasOccupied, ConvergenceRejected), record)
	case KindAbsent:
		return aliasWithPath(aliasOutcome(ActionCreateAlias, ReasonUnbaselinedAbsent, ConvergencePending), record)
	case KindSymlink:
		if record.Target.Payload() != payloadFor(record.Alias.CanonicalTargetRelativePath, record.Alias.AliasRelativePath) {
			return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonAliasWrong, ConvergenceDecisionRequired), record)
		}
		if record.AliasState == nil {
			return aliasWithPath(aliasOutcome(ActionVerifyAlias, ReasonUnbaselinedEqual, ConvergenceConverged), record)
		}
		return aliasWithPath(aliasOutcome(ActionNoOp, ReasonAliasExact, ConvergenceConverged), record)
	}
	return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonAliasOccupied, ConvergenceDecisionRequired), record)
}

// classifyFileToAlias classifies a plan alias over an active file row: the
// repository-only change applies automatically only when the current target
// provably matches the retained file representation, otherwise an explicit
// decision is required and directories or special entries need intervention.
func classifyFileToAlias(record Evaluation, semantics FileSemantics) AliasClassification {
	switch record.Target.Kind() {
	case KindDirectory, KindSpecial:
		return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonRepresentationDrift, ConvergenceRejected), record)
	case KindFile:
		if representationIntact(record, semantics) {
			return aliasWithPath(aliasOutcome(ActionReplaceAlias, ReasonRepresentationIntact, ConvergencePending), record)
		}
	}
	return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonRepresentationDrift, ConvergenceDecisionRequired), record)
}

// classifyAliasToFile classifies a plan file over an active alias row: the
// symlink is replaced by the source only when its payload provably matches
// the retained alias representation, otherwise an explicit decision is
// required and directories or special entries need intervention.
func classifyAliasToFile(record Evaluation) AliasClassification {
	switch record.Target.Kind() {
	case KindDirectory, KindSpecial:
		return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonRepresentationDrift, ConvergenceRejected), record)
	case KindSymlink:
		if record.Target.Payload() == payloadFor(record.AliasState.CanonicalTargetPath(), record.AliasState.AliasPath()) {
			return aliasWithPath(aliasOutcome(ActionWriteSourceToTarget, ReasonRepresentationIntact, ConvergencePending), record)
		}
	}
	return aliasWithPath(aliasOutcome(ActionNeedsDecision, ReasonRepresentationDrift, ConvergenceDecisionRequired), record)
}

// representationIntact reports whether the current regular target provably
// matches the retained file row: the baseline semantic fingerprint plus the
// managed mode, which is executable bits for ordinary files and the exact
// forced 0600/0700 mode for secrets.
func representationIntact(record Evaluation, semantics FileSemantics) bool {
	row := record.FileState
	if record.Target.Kind() != KindFile || semantics.Target != row.BaselineContent() {
		return false
	}
	if row.SourceKind() == deployment.FileSecret {
		return record.Target.Mode() == deployment.SecretTargetMode(row.ExecutableBits())
	}
	return record.Target.Mode()&deployment.ExecutableBitMask == row.ExecutableBits()
}

// activeFileRow reports whether the record carries an active file row.
func activeFileRow(record Evaluation) bool {
	return record.FileState != nil && record.FileState.Active()
}

// activeAliasRow reports whether the record carries an active alias row.
func activeAliasRow(record Evaluation) bool {
	return record.AliasState != nil && record.AliasState.Active()
}

// payloadFor returns the exact relative payload the alias at alias must carry
// for canonical, or the empty string when the paths cannot describe a valid
// alias, so a real link can never match an invalid declaration.
func payloadFor(canonical, alias string) string {
	payload, err := relativePayload(canonical, alias)
	if err != nil {
		return ""
	}
	return payload
}

// relativePayload computes the exact relative symlink payload from the alias
// destination's parent directory to the canonical target, mirroring the
// route-activation derivation without importing it.
func relativePayload(canonical, alias string) (string, error) {
	canonicalSegments, err := pathsafe.Segments(canonical)
	if err != nil {
		return "", err
	}
	aliasSegments, err := pathsafe.Segments(alias)
	if err != nil {
		return "", err
	}
	parent := aliasSegments[:len(aliasSegments)-1]
	common := 0
	for common < len(canonicalSegments) && common < len(parent) && canonicalSegments[common] == parent[common] {
		common++
	}
	remaining := canonicalSegments[common:]
	if len(remaining) == 0 {
		return "", fmt.Errorf("reconcile: alias %q descends into canonical %q", alias, canonical)
	}
	up := len(parent) - common
	if up == 0 {
		return strings.Join(remaining, "/"), nil
	}
	return strings.Repeat("../", up) + strings.Join(remaining, "/"), nil
}

// aliasWithPath attaches the evaluation target path to a bare outcome.
func aliasWithPath(candidate AliasClassification, record Evaluation) AliasClassification {
	candidate.TargetPath = record.TargetPath
	return candidate
}

// aliasOutcome builds a bare alias classification from its three enum fields.
func aliasOutcome(action Action, reason Reason, convergence Convergence) AliasClassification {
	return AliasClassification{Action: action, Reason: reason, Convergence: convergence}
}

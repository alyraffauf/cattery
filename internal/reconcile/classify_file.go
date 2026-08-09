package reconcile

import (
	"io/fs"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// FileSemantics carries the current semantic fingerprints of one file
// evaluation: unkeyed BLAKE3 digests for ordinary content, keyed digests for
// secrets, computed by the caller on demand (PLAN.md Sections 8.3 and 9.1).
// The classifier never decrypts and never touches the filesystem.
type FileSemantics struct {
	Source deployment.Digest
	Target deployment.Digest
}

// FileClassification is one immutable classification of a file evaluation:
// the action, its reason, and the convergence class of the target path.
type FileClassification struct {
	TargetPath  string
	Action      Action
	Reason      Reason
	Convergence Convergence
}

// ClassifyFile purely classifies one complete file evaluation against its
// persisted baseline: the five core matrix rows of PLAN.md Section 9.2, the
// unbaselined safety rows of Section 9.3, independent executable-bit
// reconciliation per Section 7.1, and unexpected target types per Section
// 7.3. A retired row with a current producer reconciles against its retained
// baseline (Section 9.5 reactivation). Records without a file producer
// belong to the alias and retirement classifiers and return the zero
// classification.
func ClassifyFile(record Evaluation, semantics FileSemantics) FileClassification {
	if record.Entry != PlanEntryFile {
		return FileClassification{TargetPath: record.TargetPath}
	}
	if record.Target.Kind() != KindAbsent && record.Target.Kind() != KindFile {
		return classifyUnexpectedType(record)
	}
	if record.FileState == nil {
		return classifyUnbaselined(record, semantics)
	}
	return classifyBaselined(record, record.FileState, semantics)
}

// classifyUnexpectedType classifies a symlink, directory, or special entry
// at a regular-file target: never written through, never replaced
// automatically (PLAN.md Section 7.3). A symlink is drift subject to an
// explicit decision; directories and special entries require manual
// intervention and are rejected.
func classifyUnexpectedType(record Evaluation) FileClassification {
	base := outcome(ActionNeedsDecision, ReasonUnexpectedTargetType, DecisionRequired)
	if record.Target.Kind() == KindDirectory || record.Target.Kind() == KindSpecial {
		base.Convergence = Rejected
	}
	return withPath(base, record)
}

// classifyUnbaselined classifies the database-loss rows of PLAN.md Section
// 9.3: create from source, adopt equal content as an operational baseline,
// or require an explicit decision for differing content.
func classifyUnbaselined(record Evaluation, semantics FileSemantics) FileClassification {
	if record.Target.Kind() != KindFile {
		return withPath(outcome(ActionCreateTarget, ReasonUnbaselinedAbsent, ActionPending), record)
	}
	if semantics.Source != semantics.Target {
		return withPath(outcome(ActionNeedsDecision, ReasonUnbaselinedDiffer, DecisionRequired), record)
	}
	return withPath(settle(outcome(ActionEstablishBaseline, ReasonUnbaselinedEqual, Converged), record), record)
}

// classifyBaselined maps the five core matrix rows of PLAN.md Section 9.2.
// For a secret source, raw storage equality with the baseline source proves
// the source unchanged without any semantic digest; raw change is a semantic
// source change only when the keyed plaintext digest also differs, so a
// SOPS re-encryption with unchanged plaintext stays converged (Section 8.3).
func classifyBaselined(record Evaluation, row *FileState, semantics FileSemantics) FileClassification {
	sourceChanged := semantics.Source != row.BaselineSource()
	if row.SourceKind() == deployment.FileSecret {
		rawUnchanged := record.Source.Snapshot().Storage() == row.BaselineSource()
		sourceChanged = !rawUnchanged && semantics.Source != row.BaselineContent()
	}
	targetChanged := semantics.Target != row.BaselineContent()
	switch {
	case !sourceChanged && !targetChanged:
		return withPath(settle(outcome(ActionNoOp, ReasonNoChange, Converged), record), record)
	case sourceChanged && !targetChanged:
		return withPath(outcome(ActionWriteSourceToTarget, ReasonSourceChanged, ActionPending), record)
	case !sourceChanged:
		return withPath(outcome(ActionNeedsDecision, ReasonTargetDrift, DecisionRequired), record)
	case semantics.Source == semantics.Target:
		return withPath(settle(outcome(ActionEstablishBaseline, ReasonAlreadyConverged, Converged), record), record)
	}
	return withPath(outcome(ActionNeedsDecision, ReasonConflict, DecisionRequired), record)
}

// settle upgrades a content-converged classification into a mode-only
// correction when the target's executable bits differ from the source's:
// automatic in both directions and independent of content drift (PLAN.md
// Section 7.1), with exact 0600/0700 forced for secrets per Section 4.5.
func settle(candidate FileClassification, record Evaluation) FileClassification {
	if candidate.Action != ActionNoOp && candidate.Action != ActionEstablishBaseline {
		return candidate
	}
	if !modeMismatch(record, sourceKind(record)) {
		return candidate
	}
	candidate.Action = ActionCorrectMode
	candidate.Reason = ReasonModeCorrection
	candidate.Convergence = ActionPending
	return candidate
}

// modeMismatch reports whether the regular target's permission mode differs
// from the mode a source write would apply: executable bits only for
// ordinary files (read/write bits are preserved), the exact forced secret
// mode for secrets.
func modeMismatch(record Evaluation, kind deployment.FileKind) bool {
	if record.Target.Kind() != KindFile {
		return false
	}
	executable := record.Source.Snapshot().Executable()
	if kind == deployment.FileSecret {
		expected := fs.FileMode(0o600)
		if executable != 0 {
			expected = 0o700
		}
		return record.Target.Mode() != expected
	}
	return record.Target.Mode()&0o111 != executable
}

// sourceKind reports the semantic mode of the current source: the state row
// decides for baselined rows, the plan entry for unbaselined ones.
func sourceKind(record Evaluation) deployment.FileKind {
	if record.FileState != nil {
		return record.FileState.SourceKind()
	}
	return record.File.Kind
}

// withPath attaches the evaluation target path to a bare outcome.
func withPath(candidate FileClassification, record Evaluation) FileClassification {
	candidate.TargetPath = record.TargetPath
	return candidate
}

// outcome builds a bare classification from its three enum fields.
func outcome(action Action, reason Reason, convergence Convergence) FileClassification {
	return FileClassification{Action: action, Reason: reason, Convergence: convergence}
}

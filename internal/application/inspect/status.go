package inspect

import (
	"context"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// StatusKind names the representation class of one status record.
type StatusKind int

const (
	StatusKindFile StatusKind = iota
	StatusKindAlias
	StatusKindRetired
)

// String returns the stable lowercase name of the kind.
func (kind StatusKind) String() string {
	switch kind {
	case StatusKindAlias:
		return "alias"
	case StatusKindRetired:
		return "retired"
	}
	return "file"
}

// StatusRecord is one immutable semantic status row of a destination: its
// representation class, stable action and reason names, and convergence.
type StatusRecord struct {
	targetPath string
	kind       StatusKind
	action     string
	reason     string
	converged  bool
}

func (record StatusRecord) TargetPath() string { return record.targetPath }
func (record StatusRecord) Kind() StatusKind   { return record.kind }
func (record StatusRecord) Action() string     { return record.action }
func (record StatusRecord) Reason() string     { return record.reason }
func (record StatusRecord) Converged() bool    { return record.converged }

// StatusResult is the frozen outcome of one status translation: the
// path-sorted semantic status records, the per-kind counts, and the overall
// convergence of the selected state.
type StatusResult struct {
	records   []StatusRecord
	files     int
	aliases   int
	retired   int
	converged bool
}

// Records returns a defensive copy of the path-sorted status records.
func (result StatusResult) Records() []StatusRecord {
	return append([]StatusRecord(nil), result.records...)
}

func (result StatusResult) Files() int      { return result.files }
func (result StatusResult) Aliases() int    { return result.aliases }
func (result StatusResult) Retired() int    { return result.retired }
func (result StatusResult) Converged() bool { return result.converged }

// Status evaluates one request and translates the evaluation into sorted
// semantic status/retired records, counts, and convergence. A Difference
// failure accompanies the partial result whenever drift remains, so
// renderers print the records and the exit mapper applies Section 11.8.
func (service *Service) Status(ctx context.Context, request Request) (StatusResult, error) {
	evaluation, err := service.evaluate(ctx, request)
	if err != nil {
		return StatusResult{}, err
	}
	return statusOutcome(evaluation)
}

// statusOutcome translates one evaluation into status records, per-kind
// counts, and convergence, failing with a Difference when drift remains.
func statusOutcome(evaluation Result) (StatusResult, error) {
	var records []StatusRecord
	for _, evaluated := range evaluation.records {
		records = append(records, statusRecordsOf(evaluated)...)
	}
	files, aliases, retired := statusCounts(records)
	result := StatusResult{records: records, files: files, aliases: aliases,
		retired: retired, converged: recordsConverged(records)}
	if !result.converged {
		return result, failure.New(failure.Difference, "status: selected state is not converged", nil)
	}
	return result, nil
}

// statusRecordsOf maps one evaluated record to its status records: every
// non-no-op file and alias classification plus the retirement record of a
// row without a producer.
func statusRecordsOf(evaluated evaluatedRecord) []StatusRecord {
	var records []StatusRecord
	if evaluated.file.Action != reconcile.ActionNoOp {
		records = append(records, statusRecord(classificationInput{
			path: evaluated.file.TargetPath, kind: StatusKindFile,
			action: evaluated.file.Action, reason: evaluated.file.Reason, convergence: evaluated.file.Convergence,
		}))
	}
	if evaluated.alias.Action != reconcile.ActionNoOp {
		records = append(records, statusRecord(classificationInput{
			path: evaluated.alias.TargetPath, kind: StatusKindAlias,
			action: evaluated.alias.Action, reason: evaluated.alias.Reason, convergence: evaluated.alias.Convergence,
		}))
	}
	if record, keep := retirementRecordOf(evaluated.retirement); keep {
		records = append(records, record)
	}
	return records
}

// retirementRecordOf maps a retirement classification to a retired record:
// pending tracking retirement and already-retired rows are reported, while
// rows of an inactive platform layer stay hidden.
func retirementRecordOf(classification reconcile.RetirementClassification) (StatusRecord, bool) {
	if classification.Action == reconcile.ActionNoOp && classification.Reason != reconcile.ReasonAlreadyRetired {
		return StatusRecord{}, false
	}
	return statusRecord(classificationInput{
		path: classification.TargetPath, kind: StatusKindRetired,
		action: classification.Action, reason: classification.Reason, convergence: classification.Convergence,
	}), true
}

// classificationInput bundles the classification fields of one status row.
type classificationInput struct {
	path        string
	kind        StatusKind
	action      reconcile.Action
	reason      reconcile.Reason
	convergence reconcile.Convergence
}

// statusRecord projects one classification onto its immutable status row.
func statusRecord(input classificationInput) StatusRecord {
	return StatusRecord{targetPath: input.path, kind: input.kind, action: actionName(input.action),
		reason: reasonName(input.reason), converged: input.convergence == reconcile.Converged}
}

// statusCounts tallies the per-kind records of one status result.
func statusCounts(records []StatusRecord) (files, aliases, retired int) {
	return countRecordKinds(records)
}

func countRecordKinds[T interface{ Kind() StatusKind }](records []T) (files, aliases, retired int) {
	for _, record := range records {
		switch record.Kind() {
		case StatusKindFile:
			files++
		case StatusKindAlias:
			aliases++
		case StatusKindRetired:
			retired++
		}
	}
	return files, aliases, retired
}

// recordsConverged reports whether every status record is converged; a
// record-free evaluation is converged by definition.
func recordsConverged(records []StatusRecord) bool {
	return recordsConvergedGeneric(records)
}

func recordsConvergedGeneric[T interface{ Converged() bool }](records []T) bool {
	for _, record := range records {
		if !record.Converged() {
			return false
		}
	}
	return true
}

// actionName returns the stable status name of one reconciliation action.
// The keyed array literal names every constant explicitly, so a reordered or
// extended enum cannot silently rename a record.
func actionName(action reconcile.Action) string {
	names := [reconcile.ActionRetireAliasState + 1]string{
		reconcile.ActionNoOp:                "no-op",
		reconcile.ActionCorrectMode:         "correct-mode",
		reconcile.ActionCreateTarget:        "create-target",
		reconcile.ActionWriteSourceToTarget: "write-source-to-target",
		reconcile.ActionEstablishBaseline:   "establish-baseline",
		reconcile.ActionNeedsDecision:       "needs-decision",
		reconcile.ActionRetireState:         "retire-state",
		reconcile.ActionCreateAlias:         "create-alias",
		reconcile.ActionReplaceAlias:        "replace-alias",
		reconcile.ActionVerifyAlias:         "verify-alias",
		reconcile.ActionRetireAliasState:    "retire-alias-state",
	}
	if int(action) < 0 || int(action) >= len(names) {
		return "no-op"
	}
	return names[action]
}

// reasonName returns the stable status name of one reconciliation reason.
// The keyed array literal names every constant explicitly, so a reordered or
// extended enum cannot silently rename a record.
func reasonName(reason reconcile.Reason) string {
	names := [reconcile.ReasonAlreadyRetired + 1]string{
		reconcile.ReasonNoChange:             "no-change",
		reconcile.ReasonModeCorrection:       "mode-correction",
		reconcile.ReasonSourceChanged:        "source-changed",
		reconcile.ReasonTargetDrift:          "target-drift",
		reconcile.ReasonAlreadyConverged:     "already-converged",
		reconcile.ReasonConflict:             "conflict",
		reconcile.ReasonUnbaselinedAbsent:    "unbaselined-absent",
		reconcile.ReasonUnbaselinedEqual:     "unbaselined-equal",
		reconcile.ReasonUnbaselinedDiffer:    "unbaselined-differ",
		reconcile.ReasonUnexpectedTargetType: "unexpected-target-type",
		reconcile.ReasonSourceRemoved:        "source-removed",
		reconcile.ReasonAliasExact:           "alias-exact",
		reconcile.ReasonAliasWrong:           "alias-wrong",
		reconcile.ReasonAliasOccupied:        "alias-occupied",
		reconcile.ReasonRepresentationIntact: "representation-intact",
		reconcile.ReasonRepresentationDrift:  "representation-drift",
		reconcile.ReasonInactivePlatform:     "inactive-platform",
		reconcile.ReasonAlreadyRetired:       "already-retired",
	}
	if int(reason) < 0 || int(reason) >= len(names) {
		return "no-change"
	}
	return names[reason]
}

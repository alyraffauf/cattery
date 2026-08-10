package inspect

import (
	"context"

	"github.com/alyraffauf/cattery/internal/application/evaluation"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/diff"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// DiffRecord is one immutable sorted diff row of a destination: the same
// semantic status fields as StatusRecord plus the output-safe diff payload
// of a file classification. Alias and retired records carry the zero safe
// payload because no content diff exists to render.
type DiffRecord struct {
	status StatusRecord
	safe   diff.SafeRecord
}

// DiffRecordInput carries the renderable fields of one diff record.
type DiffRecordInput struct {
	TargetPath  string
	Kind        StatusKind
	Action      string
	Tag         string
	SourceLabel string
	TargetLabel string
	Lines       string
	SourceSize  int64
	TargetSize  int64
}

// NewDiffRecord freezes one diff record over the given renderable fields,
// so the CLI renderer tests can build records across the boundary.
func NewDiffRecord(input DiffRecordInput) DiffRecord {
	return DiffRecord{
		status: StatusRecord{targetPath: input.TargetPath, kind: input.Kind, action: input.Action},
		safe: diff.NewSafeRecord(diff.SafeRecordInput{
			TargetPath:  input.TargetPath,
			Tag:         parseDiffTag(input.Tag),
			SourceLabel: input.SourceLabel,
			TargetLabel: input.TargetLabel,
			Lines:       input.Lines,
			SourceSize:  input.SourceSize,
			TargetSize:  input.TargetSize,
		}),
	}
}

// parseDiffTag preserves the application boundary while delegating the tag
// vocabulary to the diff package.
func parseDiffTag(name string) diff.Tag {
	return diff.ParseTag(name)
}

// DiffTagName returns the stable lowercase name of one record's safe tag.
func DiffTagName(record DiffRecord) string {
	return record.safe.Tag().String()
}

func (record DiffRecord) TargetPath() string  { return record.status.TargetPath() }
func (record DiffRecord) Kind() StatusKind    { return record.status.Kind() }
func (record DiffRecord) Action() string      { return record.status.Action() }
func (record DiffRecord) Reason() string      { return record.status.Reason() }
func (record DiffRecord) Converged() bool     { return record.status.Converged() }
func (record DiffRecord) Tag() diff.Tag       { return record.safe.Tag() }
func (record DiffRecord) SourceLabel() string { return record.safe.SourceLabel() }
func (record DiffRecord) TargetLabel() string { return record.safe.TargetLabel() }
func (record DiffRecord) Lines() string       { return record.safe.Lines() }
func (record DiffRecord) SourceSize() int64   { return record.safe.SourceSize() }
func (record DiffRecord) TargetSize() int64   { return record.safe.TargetSize() }
func (record DiffRecord) SourceHash() deployment.Digest {
	return record.safe.SourceHash()
}
func (record DiffRecord) TargetHash() deployment.Digest {
	return record.safe.TargetHash()
}

// DiffResult is the frozen outcome of one diff translation: the
// path-sorted safe diff/status records, the per-kind counts, and the overall
// convergence of the selected state.
type DiffResult struct {
	records   []DiffRecord
	files     int
	aliases   int
	retired   int
	converged bool
}

// NewDiffResult freezes one diff result over the given records and the
// convergence flag, keeping the record slice defensive.
func NewDiffResult(records []DiffRecord, converged bool) DiffResult {
	result := DiffResult{records: append([]DiffRecord(nil), records...), converged: converged}
	for _, record := range result.records {
		switch record.status.kind {
		case StatusKindAlias:
			result.aliases++
		case StatusKindRetired:
			result.retired++
		default:
			result.files++
		}
	}
	return result
}

// Records returns a defensive copy of the path-sorted diff records.
func (result DiffResult) Records() []DiffRecord {
	return append([]DiffRecord(nil), result.records...)
}

func (result DiffResult) Files() int      { return result.files }
func (result DiffResult) Aliases() int    { return result.aliases }
func (result DiffResult) Retired() int    { return result.retired }
func (result DiffResult) Converged() bool { return result.converged }

// Diff evaluates one request and translates the same evaluation as Status
// into sorted safe diff/status records, counts, and convergence. A
// Difference failure accompanies the partial result whenever drift remains;
// no rendering, prompt, mutation, or
// second snapshot occurs.
func (service *Service) Diff(ctx context.Context, request Request) (DiffResult, error) {
	evaluation, err := service.evaluate(ctx, request)
	if err != nil {
		return DiffResult{}, err
	}
	return diffOutcome(evaluation)
}

// diffOutcome translates one evaluation into safe diff/status records,
// per-kind counts, and convergence, failing with a Difference when drift
// remains.
func diffOutcome(evaluation Result) (DiffResult, error) {
	var records []DiffRecord
	for _, evaluated := range evaluation.records {
		converted, err := diffRecordsOf(evaluation.home, evaluated)
		if err != nil {
			return DiffResult{}, err
		}
		records = append(records, converted...)
	}
	files, aliases, retired := diffCounts(records)
	result := DiffResult{records: records, files: files, aliases: aliases,
		retired: retired, converged: recordsConvergedGeneric(records)}
	if !result.converged {
		return result, failure.New(failure.Difference, "diff: selected state is not converged", nil)
	}
	return result, nil
}

// diffRecordsOf maps one evaluated record to its diff records: every
// non-no-op file classification gains a safe diff record, and every alias
// and retirement classification a status record without a payload.
func diffRecordsOf(home string, evaluated evaluatedRecord) ([]DiffRecord, error) {
	var records []DiffRecord
	if evaluated.file.Action != reconcile.ActionNoOp {
		record, err := fileDiffRecord(home, evaluated)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if evaluated.alias.Action != reconcile.ActionNoOp {
		records = append(records, DiffRecord{status: statusRecord(classificationInput{
			path: evaluated.alias.TargetPath, kind: StatusKindAlias,
			action: evaluated.alias.Action, reason: evaluated.alias.Reason, convergence: evaluated.alias.Convergence,
		})})
	}
	if record, keep := retirementRecordOf(evaluated.retirement); keep {
		records = append(records, DiffRecord{status: record})
	}
	return records, nil
}

// fileDiffRecord projects one file classification onto its status fields
// and builds the output-safe record from the exact current source and
// target bytes; absent and non-file targets compare against an empty side.
func fileDiffRecord(home string, evaluated evaluatedRecord) (DiffRecord, error) {
	status := statusRecord(classificationInput{
		path: evaluated.file.TargetPath, kind: StatusKindFile,
		action: evaluated.file.Action, reason: evaluated.file.Reason, convergence: evaluated.file.Convergence,
	})
	content, err := evaluation.ReadTargetContent(home, evaluated.record, "diff")
	if err != nil {
		return DiffRecord{}, err
	}
	safe, err := diff.Build(evaluated.record, content)
	if err != nil {
		return DiffRecord{}, failure.New(failure.Operational, "diff: build record "+evaluated.record.TargetPath, err)
	}
	return DiffRecord{status: status, safe: safe}, nil
}

// diffCounts tallies the per-kind records of one diff result.
func diffCounts(records []DiffRecord) (files, aliases, retired int) {
	return countRecordKinds(records)
}

package reconcile

import (
	"io/fs"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

const (
	aliasPath     = "bin/tool"
	canonicalPath = "files/tool"
	aliasPayload  = "../files/tool"
)

// TestAliasClassification exhaustively classifies every alias and
// representation combination: fresh, baselined, and reactivated alias rows,
// intact and drifted file-to-alias and alias-to-file transitions, unexpected
// target types, and the records the alias classifier must leave untouched
// (PLAN.md Sections 5.4 and 9.5).
func TestAliasClassification(t *testing.T) {
	for _, row := range aliasClassificationCases {
		t.Run(row.name, func(t *testing.T) {
			got := ClassifyAlias(aliasRecordAt(row), aliasSemanticsAt(row))
			if got != row.want {
				t.Fatalf("classification = %+v, want %+v", got, row.want)
			}
		})
	}
}

// aliasCase is one named row of the exhaustive alias classification table.
type aliasCase struct {
	name     string
	entry    PlanEntryKind
	none     bool
	kind     EntryKind
	payload  string
	content  []byte
	mode     fs.FileMode
	secret   bool
	fileRow  *FileState
	aliasRow *AliasState
	want     AliasClassification
}

// aliasClassificationCases enumerates the alias classification matrix: every
// target entry state against a fresh, baselined, reactivated, or
// representation-transition record, plus records the classifier must ignore.
var aliasClassificationCases = []aliasCase{
	{name: "alias create", kind: KindAbsent,
		want: want(ActionCreateAlias, ReasonUnbaselinedAbsent, ActionPending)},
	{name: "alias exact unbaselined", kind: KindSymlink, payload: aliasPayload,
		want: want(ActionVerifyAlias, ReasonUnbaselinedEqual, Converged)},
	{name: "alias wrong relative link", kind: KindSymlink, payload: "files/tool",
		want: want(ActionNeedsDecision, ReasonAliasWrong, DecisionRequired)},
	{name: "alias absolute link", kind: KindSymlink, payload: "/home/user/files/tool",
		want: want(ActionNeedsDecision, ReasonAliasWrong, DecisionRequired)},
	{name: "alias occupied file", kind: KindFile, content: targetX, mode: 0o644,
		want: want(ActionNeedsDecision, ReasonAliasOccupied, DecisionRequired)},
	{name: "alias occupied directory", kind: KindDirectory,
		want: want(ActionNeedsDecision, ReasonAliasOccupied, Rejected)},
	{name: "alias occupied special", kind: KindSpecial,
		want: want(ActionNeedsDecision, ReasonAliasOccupied, Rejected)},
	{name: "alias exact baselined", aliasRow: aliasStateActive(), kind: KindSymlink, payload: aliasPayload,
		want: want(ActionNoOp, ReasonAliasExact, Converged)},
	{name: "alias missing recreated", aliasRow: aliasStateActive(), kind: KindAbsent,
		want: want(ActionCreateAlias, ReasonUnbaselinedAbsent, ActionPending)},
	{name: "alias wrong baselined", aliasRow: aliasStateActive(), kind: KindSymlink, payload: "files/tool",
		want: want(ActionNeedsDecision, ReasonAliasWrong, DecisionRequired)},
	{name: "alias occupied baselined", aliasRow: aliasStateActive(), kind: KindFile, content: targetX, mode: 0o644,
		want: want(ActionNeedsDecision, ReasonAliasOccupied, DecisionRequired)},
	{name: "alias directory baselined", aliasRow: aliasStateActive(), kind: KindDirectory,
		want: want(ActionNeedsDecision, ReasonAliasOccupied, Rejected)},
	{name: "alias retired exact", aliasRow: aliasStateRetired(), kind: KindSymlink, payload: aliasPayload,
		want: want(ActionNoOp, ReasonAliasExact, Converged)},
	{name: "alias retired wrong", aliasRow: aliasStateRetired(), kind: KindSymlink, payload: "files/tool",
		want: want(ActionNeedsDecision, ReasonAliasWrong, DecisionRequired)},
	{name: "alias over retired file", fileRow: retired(stateAt(sourceA, sourceA, 0)), kind: KindAbsent,
		want: want(ActionCreateAlias, ReasonUnbaselinedAbsent, ActionPending)},
	{name: "file to alias intact", fileRow: stateAt(sourceA, sourceA, 0), kind: KindFile, content: sourceA, mode: 0o644,
		want: want(ActionReplaceAlias, ReasonRepresentationIntact, ActionPending)},
	{name: "file to alias exec intact", fileRow: stateAt(sourceA, sourceA, 0o111), kind: KindFile, content: sourceA, mode: 0o755,
		want: want(ActionReplaceAlias, ReasonRepresentationIntact, ActionPending)},
	{name: "file to alias mode drift", fileRow: stateAt(sourceA, sourceA, 0), kind: KindFile, content: sourceA, mode: 0o755,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "file to alias content drift", fileRow: stateAt(sourceA, sourceA, 0), kind: KindFile, content: sourceB, mode: 0o644,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "file to alias missing", fileRow: stateAt(sourceA, sourceA, 0), kind: KindAbsent,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "file to alias link", fileRow: stateAt(sourceA, sourceA, 0), kind: KindSymlink, payload: aliasPayload,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "file to alias directory", fileRow: stateAt(sourceA, sourceA, 0), kind: KindDirectory,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, Rejected)},
	{name: "secret file to alias intact", secret: true, fileRow: secretStateAt(plainA, cipherA, 0), kind: KindFile, content: plainA, mode: 0o600,
		want: want(ActionReplaceAlias, ReasonRepresentationIntact, ActionPending)},
	{name: "secret file to alias exec", secret: true, fileRow: secretStateAt(plainA, cipherA, 0o100), kind: KindFile, content: plainA, mode: 0o700,
		want: want(ActionReplaceAlias, ReasonRepresentationIntact, ActionPending)},
	{name: "secret file to alias mode drift", secret: true, fileRow: secretStateAt(plainA, cipherA, 0), kind: KindFile, content: plainA, mode: 0o644,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "secret file to alias content drift", secret: true, fileRow: secretStateAt(plainA, cipherA, 0), kind: KindFile, content: plainB, mode: 0o600,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "alias to file intact", entry: PlanEntryFile, aliasRow: aliasStateActive(), kind: KindSymlink, payload: aliasPayload,
		want: want(ActionWriteSourceToTarget, ReasonRepresentationIntact, ActionPending)},
	{name: "alias to file wrong link", entry: PlanEntryFile, aliasRow: aliasStateActive(), kind: KindSymlink, payload: "files/tool",
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "alias to file absolute", entry: PlanEntryFile, aliasRow: aliasStateActive(), kind: KindSymlink, payload: "/abs",
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "alias to file missing", entry: PlanEntryFile, aliasRow: aliasStateActive(), kind: KindAbsent,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "alias to file file", entry: PlanEntryFile, aliasRow: aliasStateActive(), kind: KindFile, content: sourceA, mode: 0o644,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, DecisionRequired)},
	{name: "alias to file directory", entry: PlanEntryFile, aliasRow: aliasStateActive(), kind: KindDirectory,
		want: want(ActionNeedsDecision, ReasonRepresentationDrift, Rejected)},
	{name: "file entry untouched", entry: PlanEntryFile, kind: KindAbsent,
		want: AliasClassification{TargetPath: aliasPath}},
	{name: "file with retired alias untouched", entry: PlanEntryFile, aliasRow: aliasStateRetired(), kind: KindFile, content: sourceA, mode: 0o644,
		want: AliasClassification{TargetPath: aliasPath}},
	{name: "retirement record untouched", none: true, fileRow: stateAt(sourceA, sourceA, 0), kind: KindAbsent,
		want: AliasClassification{TargetPath: aliasPath}},
	{name: "retired transition untouched", none: true, fileRow: retired(stateAt(sourceA, sourceA, 0)), aliasRow: aliasStateActive(), kind: KindAbsent,
		want: AliasClassification{TargetPath: aliasPath}},
}

// aliasRecordAt assembles one complete alias evaluation from its table row;
// rows without an explicit entry default to an alias plan producer.
func aliasRecordAt(row aliasCase) Evaluation {
	entry := row.entry
	if entry == PlanEntryNone && !row.none {
		entry = PlanEntryAlias
	}
	return Evaluation{TargetPath: aliasPath, Entry: entry,
		Alias: deployment.Alias{Platform: "linux", AliasRelativePath: aliasPath,
			CanonicalTargetRelativePath: canonicalPath},
		Target: aliasTargetAt(row), FileState: row.fileRow, AliasState: row.aliasRow}
}

// aliasTargetAt freezes the target snapshot of a row: a link payload for
// symlinks, or the kind, bytes, and mode of every other entry kind.
func aliasTargetAt(row aliasCase) TargetSnapshot {
	if row.kind == KindSymlink {
		return TargetSnapshot{kind: KindSymlink, payload: row.payload}
	}
	return targetAt(row.kind, row.content, row.mode)
}

// aliasSemanticsAt derives the current target semantic fingerprint of a row:
// keyed for secret file rows, unkeyed for ordinary ones.
func aliasSemanticsAt(row aliasCase) FileSemantics {
	if row.secret {
		return FileSemantics{Target: keyed(row.content)}
	}
	return FileSemantics{Target: digestOf(row.content)}
}

// want builds one expected classification over the fixed alias path.
func want(action Action, reason Reason, convergence Convergence) AliasClassification {
	return AliasClassification{TargetPath: aliasPath, Action: action, Reason: reason, Convergence: convergence}
}

// aliasStateActive is the retained row of a successfully realized alias.
func aliasStateActive() *AliasState {
	return &AliasState{aliasPath: aliasPath, canonicalTargetPath: canonicalPath, active: true}
}

// aliasStateRetired is an inactive copy of the alias row.
func aliasStateRetired() *AliasState {
	row := aliasStateActive()
	row.active = false
	return row
}

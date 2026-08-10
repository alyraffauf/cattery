package reconcile

import (
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/state"
)

// TestRetirementClassification classifies every state-only row shape: source
// removal of files and aliases, whole deleted scopes, cross-scope ownership
// moves that preserve the baseline, inactive platform layers, and rows that
// are already retired, without any target action. Snapshot-level scenarios
// verify complete-plan producer checks and
// selected state subsets.
func TestRetirementClassification(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"per-record matrix", testRetirementMatrix},
		{"complete-plan producer checks", testRetirementCompletePlan},
		{"selected subsets", testRetirementSelectedSubsets},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// retirementCase is one named row of the per-record retirement matrix.
type retirementCase struct {
	name     string
	platform string
	entry    PlanEntryKind
	fileRow  *FileState
	aliasRow *AliasState
	want     RetirementClassification
}

// retirementClassificationCases enumerates the retirement matrix: active rows
// without producers retire, inactive platform layers and already-retired rows
// stay put, and every record with a producer belongs to the file and alias
// classifiers.
var retirementClassificationCases = []retirementCase{
	{name: "file source removed", fileRow: fileRowState(deployment.LayerBase),
		want: retirementWant(ActionRetireFileState, ReasonSourceRemoved, ConvergencePending)},
	{name: "linux file source removed", fileRow: fileRowState(deployment.LayerLinux),
		want: retirementWant(ActionRetireFileState, ReasonSourceRemoved, ConvergencePending)},
	{name: "base file row on darwin", platform: "darwin", fileRow: fileRowState(deployment.LayerBase),
		want: retirementWant(ActionRetireFileState, ReasonSourceRemoved, ConvergencePending)},
	{name: "darwin file row on linux", platform: "linux", fileRow: fileRowState(deployment.LayerDarwin),
		want: retirementWant(ActionNoOp, ReasonInactivePlatform, ConvergenceConverged)},
	{name: "linux file row on darwin", platform: "darwin", fileRow: fileRowState(deployment.LayerLinux),
		want: retirementWant(ActionNoOp, ReasonInactivePlatform, ConvergenceConverged)},
	{name: "file already retired", fileRow: retiredFileRow(deployment.LayerBase),
		want: retirementWant(ActionNoOp, ReasonAlreadyRetired, ConvergenceConverged)},
	{name: "alias source removed", aliasRow: aliasRowState(state.LayerAll),
		want: retirementWant(ActionRetireAliasState, ReasonSourceRemoved, ConvergencePending)},
	{name: "linux alias source removed", aliasRow: aliasRowState(state.LayerLinux),
		want: retirementWant(ActionRetireAliasState, ReasonSourceRemoved, ConvergencePending)},
	{name: "darwin alias row on linux", platform: "linux", aliasRow: aliasRowState(state.LayerDarwin),
		want: retirementWant(ActionNoOp, ReasonInactivePlatform, ConvergenceConverged)},
	{name: "linux alias row on darwin", platform: "darwin", aliasRow: aliasRowState(state.LayerLinux),
		want: retirementWant(ActionNoOp, ReasonInactivePlatform, ConvergenceConverged)},
	{name: "alias already retired", aliasRow: retiredAliasRow(state.LayerAll),
		want: retirementWant(ActionNoOp, ReasonAlreadyRetired, ConvergenceConverged)},
	{name: "moved file ownership", entry: PlanEntryFile, fileRow: fileRowState(deployment.LayerBase),
		want: retirementUntouched},
	{name: "moved alias ownership", entry: PlanEntryAlias, aliasRow: aliasRowState(state.LayerAll),
		want: retirementUntouched},
	{name: "retired row with producer", entry: PlanEntryFile, fileRow: retiredFileRow(deployment.LayerBase),
		want: retirementUntouched},
	{name: "retired alias with producer", entry: PlanEntryAlias, aliasRow: retiredAliasRow(state.LayerAll),
		want: retirementUntouched},
	{name: "representation transition", entry: PlanEntryFile, aliasRow: aliasRowState(state.LayerAll),
		want: retirementUntouched},
	{name: "unmanaged record", want: retirementUntouched},
}

// testRetirementMatrix runs every named row of the per-record matrix.
func testRetirementMatrix(t *testing.T) {
	for _, row := range retirementClassificationCases {
		t.Run(row.name, func(t *testing.T) {
			got := ClassifyRetirement(retirementRecordAt(row), platformAt(row))
			if got != row.want {
				t.Fatalf("classification = %+v, want %+v", got, row.want)
			}
		})
	}
}

// testRetirementCompletePlan verifies producer checks against the complete
// current platform plan: a row whose target has a producer anywhere, even
// under a different scope, keeps its baseline, while every row without a
// producer retires, including all rows of a wholly deleted scope.
func testRetirementCompletePlan(t *testing.T) {
	repo, _ := fixtureDir(t)
	files := []deployment.ManagedFile{planFile(t, repo, "a.conf"), planFile(t, repo, "b.conf"), planFile(t, repo, "moved.conf")}
	files[1].Scope = deployment.Scope{Group: "tools"}
	files[2].Scope = deployment.Scope{Group: "tools"}
	rows := StateRows{Files: []state.FileBaseline{
		fileRow("a.conf", "apps", "a.conf"),
		fileRow("b.conf", "apps", "b.conf"),
		fileRow("moved.conf", "old", "moved.conf"),
		fileRow("gone.conf", "apps", "gone.conf"),
		fileRow("old.conf", "old", "old.conf"),
	}, Aliases: []state.AliasBaseline{aliasRow("bin/gone", "files/gone", "old")}}
	snapshot := mustAssemble(t, samplePlan(repo, files, nil), sampleState(t, repo, rows))
	expectations := []retirementExpectation{
		{path: "a.conf", want: retirementZero("a.conf")},
		{path: "b.conf", want: retirementZero("b.conf")},
		{path: "moved.conf", want: retirementZero("moved.conf")},
		{path: "gone.conf", want: retiring("gone.conf")},
		{path: "old.conf", want: retiring("old.conf")},
		{path: "bin/gone", want: retiringAlias("bin/gone")},
	}
	for _, expectation := range expectations {
		requireRetirement(t, snapshot, expectation)
	}
}

// testRetirementSelectedSubsets verifies group-selective evaluation: a target
// owned by an unselected scope keeps its producer in the complete plan and is
// never retired, while state-only rows of the selected scope retire.
func testRetirementSelectedSubsets(t *testing.T) {
	repo, _ := fixtureDir(t)
	files := []deployment.ManagedFile{planFile(t, repo, "a.conf"), planFile(t, repo, "b.conf")}
	files[1].Scope = deployment.Scope{Group: "tools"}
	rows := StateRows{Files: []state.FileBaseline{
		fileRow("a.conf", "apps", "a.conf"),
		fileRow("gone.conf", "apps", "gone.conf"),
	}}
	snapshot := mustAssemble(t, samplePlan(repo, files, nil), sampleState(t, repo, rows))
	expectations := []retirementExpectation{
		{path: "a.conf", want: retirementZero("a.conf")},
		{path: "b.conf", want: retirementZero("b.conf")},
		{path: "gone.conf", want: retiring("gone.conf")},
	}
	for _, expectation := range expectations {
		requireRetirement(t, snapshot, expectation)
	}
}

// retirementRecordAt assembles one state-only evaluation from its table row.
func retirementRecordAt(row retirementCase) Evaluation {
	return Evaluation{TargetPath: retirementPath, Entry: row.entry, FileState: row.fileRow, AliasState: row.aliasRow}
}

// platformAt returns the default linux platform when a row leaves it unset.
func platformAt(row retirementCase) string {
	if row.platform == "" {
		return "linux"
	}
	return row.platform
}

// fileRowState is one active ordinary file row on the given layer.
func fileRowState(layer deployment.Layer) *FileState {
	return &FileState{targetPath: retirementPath, groupName: "apps", sourceKind: deployment.FileOrdinary, layer: layer, active: true}
}

// retiredFileRow is an inactive copy of an ordinary file row.
func retiredFileRow(layer deployment.Layer) *FileState {
	row := fileRowState(layer)
	row.active = false
	return row
}

// aliasRowState is one active alias row on the given layer.
func aliasRowState(layer state.AliasLayer) *AliasState {
	return &AliasState{aliasPath: "bin/gone", canonicalTargetPath: "files/gone", groupName: "apps", layer: layer, active: true}
}

// retiredAliasRow is an inactive copy of an alias row.
func retiredAliasRow(layer state.AliasLayer) *AliasState {
	row := aliasRowState(layer)
	row.active = false
	return row
}

// retirementWant builds the expected classification over the fixed path.
func retirementWant(action Action, reason Reason, convergence Convergence) RetirementClassification {
	return RetirementClassification{TargetPath: retirementPath, Action: action, Reason: reason, Convergence: convergence}
}

// retiring is the source-removal classification of one file target.
func retiring(path string) RetirementClassification {
	return RetirementClassification{TargetPath: path, Action: ActionRetireFileState, Reason: ReasonSourceRemoved, Convergence: ConvergencePending}
}

// retiringAlias is the source-removal classification of one alias target.
func retiringAlias(path string) RetirementClassification {
	return RetirementClassification{TargetPath: path, Action: ActionRetireAliasState, Reason: ReasonSourceRemoved, Convergence: ConvergencePending}
}

// retirementZero is the untouched classification of a record another
// classifier owns.
func retirementZero(path string) RetirementClassification {
	return RetirementClassification{TargetPath: path}
}

// retirementExpectation names one snapshot path and its expected class.
type retirementExpectation struct {
	path string
	want RetirementClassification
}

// requireRetirement classifies one snapshot record and fails on mismatch.
func requireRetirement(t *testing.T, snapshot EvaluationSnapshot, expectation retirementExpectation) {
	t.Helper()
	got := ClassifyRetirement(findRecord(t, snapshot.All(), expectation.path), "linux")
	if got != expectation.want {
		t.Fatalf("%s classification = %+v, want %+v", expectation.path, got, expectation.want)
	}
}

// retirementUntouched is the classification of records owned elsewhere.
var retirementUntouched = RetirementClassification{TargetPath: retirementPath}

const retirementPath = "gone.conf"

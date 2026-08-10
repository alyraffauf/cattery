package reconcile

import (
	"reflect"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

const (
	decisionPath      = "a.conf"
	aliasDecisionPath = "bin/tool"
)

var (
	overwriteSkipAbort     = []DecisionChoice{ChoiceOverwrite, ChoiceSkip, ChoiceAbort}
	diffOverwriteSkipAbort = []DecisionChoice{ChoiceDiff, ChoiceOverwrite, ChoiceSkip, ChoiceAbort}
)

// TestDecisionSpecification pins the decision-spec contract of PLAN.md
// Section 9.4: overwrite/skip/abort/diff eligibility by action, reason, and
// source kind, bytewise target-path ordering of produced specs, and
// rejection of specs whose choices are not exactly the allowed set.
func TestDecisionSpecification(t *testing.T) {
	for _, row := range decisionEligibilityCases {
		t.Run(row.name, func(t *testing.T) {
			got := AllowedChoices(row.action, row.reason, row.kind)
			if !reflect.DeepEqual(got, row.want) {
				t.Fatalf("AllowedChoices = %v, want %v", got, row.want)
			}
		})
	}
	for _, row := range decisionSpecCases {
		t.Run(row.name, func(t *testing.T) {
			checkDecisionSpec(t, row)
		})
	}
	for _, row := range invalidDecisionCases {
		t.Run(row.name, func(t *testing.T) {
			checkInvalidDecision(t, row)
		})
	}
	for _, row := range decisionOrderCases {
		t.Run(row.name, func(t *testing.T) {
			checkDecisionOrder(t, row)
		})
	}
}

// eligibilityCase is one row of the allowed-choices matrix.
type eligibilityCase struct {
	name   string
	action Action
	reason Reason
	kind   deployment.FileKind
	want   []DecisionChoice
}

// decisionEligibilityCases enumerates the Section 9.4 prompt rule: every
// decision offers overwrite, skip, and abort; diff rides only on ordinary
// byte-comparing file rows; automatic and non-decision pairs offer nothing.
var decisionEligibilityCases = []eligibilityCase{
	{name: "ordinary drift", action: ActionNeedsDecision, reason: ReasonTargetDrift, kind: deployment.FileOrdinary, want: diffOverwriteSkipAbort},
	{name: "ordinary conflict", action: ActionNeedsDecision, reason: ReasonConflict, kind: deployment.FileOrdinary, want: diffOverwriteSkipAbort},
	{name: "ordinary unbaselined differ", action: ActionNeedsDecision, reason: ReasonUnbaselinedDiffer, kind: deployment.FileOrdinary, want: diffOverwriteSkipAbort},
	{name: "secret drift", action: ActionNeedsDecision, reason: ReasonTargetDrift, kind: deployment.FileSecret, want: overwriteSkipAbort},
	{name: "secret conflict", action: ActionNeedsDecision, reason: ReasonConflict, kind: deployment.FileSecret, want: overwriteSkipAbort},
	{name: "secret unbaselined differ", action: ActionNeedsDecision, reason: ReasonUnbaselinedDiffer, kind: deployment.FileSecret, want: overwriteSkipAbort},
	{name: "unexpected target type", action: ActionNeedsDecision, reason: ReasonUnexpectedTargetType, kind: deployment.FileOrdinary, want: overwriteSkipAbort},
	{name: "alias wrong", action: ActionNeedsDecision, reason: ReasonAliasWrong, kind: deployment.FileOrdinary, want: overwriteSkipAbort},
	{name: "alias occupied", action: ActionNeedsDecision, reason: ReasonAliasOccupied, kind: deployment.FileSecret, want: overwriteSkipAbort},
	{name: "representation drift", action: ActionNeedsDecision, reason: ReasonRepresentationDrift, kind: deployment.FileOrdinary, want: overwriteSkipAbort},
	{name: "automatic action", action: ActionWriteSourceToTarget, reason: ReasonSourceChanged, kind: deployment.FileOrdinary},
	{name: "automatic reason", action: ActionNeedsDecision, reason: ReasonNoChange, kind: deployment.FileOrdinary},
	{name: "unknown reason", action: ActionNeedsDecision, reason: Reason(99), kind: deployment.FileOrdinary},
}

// specCase is one row of the spec-production matrix.
type specCase struct {
	name        string
	path        string
	action      Action
	reason      Reason
	convergence Convergence
	kind        deployment.FileKind
	alias       bool
	want        []DecisionChoice
	wantErr     bool
}

// decisionSpecCases enumerates production over every decision-relevant
// classification: only DecisionRequired outcomes produce a spec, carrying
// exactly the allowed choices of their action, reason, and source kind.
var decisionSpecCases = []specCase{
	{name: "file drift", path: decisionPath, action: ActionNeedsDecision, reason: ReasonTargetDrift, convergence: ConvergenceDecisionRequired, kind: deployment.FileOrdinary, want: diffOverwriteSkipAbort},
	{name: "file drift secret", path: decisionPath, action: ActionNeedsDecision, reason: ReasonTargetDrift, convergence: ConvergenceDecisionRequired, kind: deployment.FileSecret, want: overwriteSkipAbort},
	{name: "file conflict", path: decisionPath, action: ActionNeedsDecision, reason: ReasonConflict, convergence: ConvergenceDecisionRequired, kind: deployment.FileOrdinary, want: diffOverwriteSkipAbort},
	{name: "file unbaselined differ", path: decisionPath, action: ActionNeedsDecision, reason: ReasonUnbaselinedDiffer, convergence: ConvergenceDecisionRequired, kind: deployment.FileOrdinary, want: diffOverwriteSkipAbort},
	{name: "file unexpected type", path: decisionPath, action: ActionNeedsDecision, reason: ReasonUnexpectedTargetType, convergence: ConvergenceDecisionRequired, kind: deployment.FileOrdinary, want: overwriteSkipAbort},
	{name: "file converged rejected", path: decisionPath, action: ActionNoOp, reason: ReasonNoChange, convergence: ConvergenceConverged, kind: deployment.FileOrdinary, wantErr: true},
	{name: "file pending rejected", path: decisionPath, action: ActionWriteSourceToTarget, reason: ReasonSourceChanged, convergence: ConvergencePending, kind: deployment.FileOrdinary, wantErr: true},
	{name: "file rejected rejected", path: decisionPath, action: ActionNeedsDecision, reason: ReasonUnexpectedTargetType, convergence: ConvergenceRejected, kind: deployment.FileOrdinary, wantErr: true},
	{name: "alias wrong", path: aliasDecisionPath, action: ActionNeedsDecision, reason: ReasonAliasWrong, convergence: ConvergenceDecisionRequired, kind: deployment.FileOrdinary, alias: true, want: overwriteSkipAbort},
	{name: "alias occupied", path: aliasDecisionPath, action: ActionNeedsDecision, reason: ReasonAliasOccupied, convergence: ConvergenceDecisionRequired, kind: deployment.FileSecret, alias: true, want: overwriteSkipAbort},
	{name: "alias representation drift", path: aliasDecisionPath, action: ActionNeedsDecision, reason: ReasonRepresentationDrift, convergence: ConvergenceDecisionRequired, kind: deployment.FileOrdinary, alias: true, want: overwriteSkipAbort},
	{name: "alias converged rejected", path: aliasDecisionPath, action: ActionNoOp, reason: ReasonAliasExact, convergence: ConvergenceConverged, alias: true, wantErr: true},
	{name: "alias rejected rejected", path: aliasDecisionPath, action: ActionNeedsDecision, reason: ReasonAliasOccupied, convergence: ConvergenceRejected, alias: true, wantErr: true},
}

// invalidCase is one row of the choice-validation matrix.
type invalidCase struct {
	name    string
	spec    DecisionSpec
	kind    deployment.FileKind
	wantErr bool
}

// invalidDecisionCases enumerates validation: a spec is rejected unless its
// choices are exactly the allowed set for its action, reason, and kind.
var invalidDecisionCases = []invalidCase{
	{name: "diff on secret", spec: specOf(ReasonTargetDrift, diffOverwriteSkipAbort), kind: deployment.FileSecret, wantErr: true},
	{name: "duplicate overwrite", spec: specOf(ReasonTargetDrift, []DecisionChoice{ChoiceOverwrite, ChoiceOverwrite, ChoiceSkip, ChoiceAbort}), kind: deployment.FileOrdinary, wantErr: true},
	{name: "missing abort", spec: specOf(ReasonTargetDrift, []DecisionChoice{ChoiceDiff, ChoiceOverwrite, ChoiceSkip}), kind: deployment.FileOrdinary, wantErr: true},
	{name: "diff on alias reason", spec: specOf(ReasonAliasWrong, diffOverwriteSkipAbort), kind: deployment.FileOrdinary, wantErr: true},
	{name: "diff on unexpected type", spec: specOf(ReasonUnexpectedTargetType, diffOverwriteSkipAbort), kind: deployment.FileOrdinary, wantErr: true},
	{name: "choices on automatic row", spec: specOf(ReasonNoChange, overwriteSkipAbort), kind: deployment.FileOrdinary, wantErr: true},
	{name: "valid ordinary drift", spec: specOf(ReasonTargetDrift, diffOverwriteSkipAbort), kind: deployment.FileOrdinary},
	{name: "valid secret drift", spec: specOf(ReasonTargetDrift, overwriteSkipAbort), kind: deployment.FileSecret},
	{name: "valid alias wrong", spec: specOf(ReasonAliasWrong, overwriteSkipAbort), kind: deployment.FileSecret},
}

// orderCase is one row of the ordering matrix.
type orderCase struct {
	name  string
	specs []DecisionSpec
	want  []string
}

// decisionOrderCases enumerates bytewise target-path ordering of specs.
var decisionOrderCases = []orderCase{
	{name: "bytewise", specs: []DecisionSpec{orderSpec("z.conf"), orderSpec("a.conf"), orderSpec("m.conf")}, want: []string{"a.conf", "m.conf", "z.conf"}},
	{name: "slash after dot", specs: []DecisionSpec{orderSpec("a/b.conf"), orderSpec("a.conf")}, want: []string{"a.conf", "a/b.conf"}},
}

// specOf builds one decision spec over the fixed file path.
func specOf(reason Reason, choices []DecisionChoice) DecisionSpec {
	return mustDecisionSpec(DecisionSpecInput{TargetPath: decisionPath, Action: ActionNeedsDecision, Reason: reason, Choices: choices})
}

// orderSpec builds one ordinary drift spec at path.
func orderSpec(path string) DecisionSpec {
	return mustDecisionSpec(DecisionSpecInput{TargetPath: path, Action: ActionNeedsDecision, Reason: ReasonTargetDrift, Choices: diffOverwriteSkipAbort})
}

// checkDecisionSpec produces one spec from its row and compares the result.
func checkDecisionSpec(t *testing.T, row specCase) {
	t.Helper()
	spec, err := specAt(row)
	if row.wantErr {
		if err == nil {
			t.Fatalf("spec %s must be rejected", row.name)
		}
		return
	}
	if err != nil {
		t.Fatalf("spec %s: %v", row.name, err)
	}
	want := mustDecisionSpec(DecisionSpecInput{TargetPath: row.path, Action: row.action, Reason: row.reason, Choices: row.want})
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("spec = %+v, want %+v", spec, want)
	}
}

// specAt produces one decision spec from its table row.
func specAt(row specCase) (DecisionSpec, error) {
	if row.alias {
		return DecisionSpecForAlias(AliasClassification{TargetPath: row.path, Action: row.action, Reason: row.reason, Convergence: row.convergence})
	}
	return DecisionSpecForFile(FileClassification{TargetPath: row.path, Action: row.action, Reason: row.reason, Convergence: row.convergence}, row.kind)
}

func mustDecisionSpec(input DecisionSpecInput) DecisionSpec {
	spec, err := NewDecisionSpec(input)
	if err != nil {
		panic(err)
	}
	return spec
}

// checkInvalidDecision validates one spec row against the eligibility rule.
func checkInvalidDecision(t *testing.T, row invalidCase) {
	t.Helper()
	err := ValidateDecisionSpec(row.spec, row.kind)
	if row.wantErr && err == nil {
		t.Fatalf("choices for %d must be rejected", row.spec.Reason())
	}
	if !row.wantErr && err != nil {
		t.Fatalf("choices for %d rejected: %v", row.spec.Reason(), err)
	}
}

// checkDecisionOrder orders one spec row and pins input immutability.
func checkDecisionOrder(t *testing.T, row orderCase) {
	t.Helper()
	original := append([]DecisionSpec(nil), row.specs...)
	got := OrderedDecisionSpecs(row.specs)
	if !reflect.DeepEqual(pathsOf(got), row.want) {
		t.Fatalf("ordered paths = %v, want %v", pathsOf(got), row.want)
	}
	if !reflect.DeepEqual(row.specs, original) {
		t.Fatal("OrderedDecisionSpecs mutated its input")
	}
}

// pathsOf projects a spec slice onto its target paths.
func pathsOf(specs []DecisionSpec) []string {
	paths := make([]string, len(specs))
	for index, spec := range specs {
		paths[index] = spec.TargetPath()
	}
	return paths
}

package apply

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/hooks"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestApplyContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"decision requests validate and copy", testContractDecisionRequest},
		{"safe differences copy lines", testContractSafeDifference},
		{"action plans copy defensively", testContractActionPlan},
		{"summaries tally partial outcomes", testContractPartialSummaries},
		{"dependency seams accept fakes", testContractDependencySeams},
		{"dto fields are application-owned", testContractCleanTypes},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testContractDecisionRequest(t *testing.T) {
	request, err := NewDecisionRequest(DecisionRequestInput{TargetPath: "a.conf", Choices: []DecisionChoice{ChoiceOverwrite, ChoiceSkip}})
	if err != nil {
		t.Fatalf("new decision request: %v", err)
	}
	if request.TargetPath() != "a.conf" {
		t.Fatalf("target path = %q, want a.conf", request.TargetPath())
	}
	if !reflect.DeepEqual(request.Choices(), []DecisionChoice{ChoiceOverwrite, ChoiceSkip}) {
		t.Fatalf("choices = %v, want [overwrite skip]", request.Choices())
	}
	choices := request.Choices()
	choices[0] = ChoiceAbort
	if request.Choices()[0] != ChoiceOverwrite {
		t.Fatal("mutating a Choices copy must not reach the request")
	}
	rejected := []struct {
		name  string
		input DecisionRequestInput
	}{
		{"empty target", DecisionRequestInput{}},
		{"empty choices", DecisionRequestInput{TargetPath: "a.conf"}},
		{"invalid choice", DecisionRequestInput{TargetPath: "a.conf", Choices: []DecisionChoice{"prompt"}}},
	}
	for _, scenario := range rejected {
		t.Run(scenario.name, func(t *testing.T) {
			if _, err := NewDecisionRequest(scenario.input); err == nil {
				t.Fatalf("%s must be rejected", scenario.name)
			}
		})
	}
}

func testContractSafeDifference(t *testing.T) {
	difference := SafeDifference{Tag: DiffTagText, SourceSize: 3, TargetSize: 5,
		SourceHash: "aa", TargetHash: "bb", Lines: []string{"-a", "+b"}}
	lines := difference.LinesCopy()
	lines[0] = "mutated"
	if difference.Lines[0] != "-a" {
		t.Fatal("mutating a Lines copy must not reach the difference")
	}
	if difference.Tag != DiffTagText || difference.SourceSize != 3 || difference.TargetSize != 5 {
		t.Fatal("safe difference fields must be preserved")
	}
}

func testContractActionPlan(t *testing.T) {
	plan := NewActionPlan([]PlanAction{
		{TargetPath: "b", Kind: ActionKindReplaceFile, SourcePath: "files/b"},
		{TargetPath: "a", Kind: ActionKindWriteSource, SourcePath: "files/a"},
	})
	if got := plan.Items(); len(got) != 2 || got[0].Kind != ActionKindReplaceFile {
		t.Fatalf("action plan must freeze the ordered actions, got %+v", got)
	}
	actions := plan.Items()
	actions[0].Kind = ActionKindRetireFile
	if plan.Items()[0].Kind != ActionKindReplaceFile {
		t.Fatal("mutating an Actions copy must not reach the plan")
	}
	empty := NewActionPlan(nil)
	if len(empty.Items()) != 0 {
		t.Fatal("an empty action plan must stay empty")
	}
}

func testContractPartialSummaries(t *testing.T) {
	result := Result{Items: []ItemResult{
		{TargetPath: "a", Status: StatusCompleted, Kind: ActionKindReplaceFile},
		{TargetPath: "b", Status: StatusPartial, Kind: ActionKindWriteSource},
		{TargetPath: "c", Status: StatusPlanned, Kind: ActionKindRealizeAlias},
	}, Summary: Summary{Planned: 1, Completed: 1, Partial: 1}}
	if result.Summary.Planned != 1 || result.Summary.Completed != 1 || result.Summary.Partial != 1 {
		t.Fatalf("summary = %+v, want one planned, one completed, one partial", result.Summary)
	}
	items := result.ItemsCopy()
	items[0].Status = StatusPartial
	if result.Items[0].Status != StatusCompleted {
		t.Fatal("mutating an ItemsCopy must not reach the result")
	}
	if len(result.ItemsCopy()) != 3 {
		t.Fatal("ItemsCopy must preserve the record count")
	}
}

type fakeRepositorySource struct {
	identity RepositoryIdentity
}

func (f fakeRepositorySource) Resolve(selection.RepositoryRequest) (RepositoryIdentity, error) {
	return f.identity, nil
}

type fakeCompiler struct {
	plan deployment.Plan
}

func (f fakeCompiler) Compile(repository.CompileInput) (deployment.Plan, error) {
	return f.plan, nil
}

type fakeStateReader struct {
	files   []state.FileBaseline
	aliases []state.AliasBaseline
	key     [32]byte
	keyErr  error
}

func (f fakeStateReader) FileBaselines(root, home string) ([]state.FileBaseline, error) {
	return f.files, nil
}

func (f fakeStateReader) AliasBaselines(root, home string) ([]state.AliasBaseline, error) {
	return f.aliases, nil
}

func (f fakeStateReader) RecoverHashKey() ([32]byte, error) {
	return f.key, f.keyErr
}

type fakeBaselineStore struct {
	baseline state.FileBaseline
}

func (f fakeBaselineStore) UpsertFileBaseline(root, home string, baseline state.FileBaseline) (state.FileBaseline, error) {
	return f.baseline, nil
}

func (f fakeBaselineStore) UpsertAliasBaseline(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error) {
	return state.AliasBaseline{}, nil
}

type fakeTransitionStore struct {
	alias    state.AliasBaseline
	baseline state.FileBaseline
}

func (f fakeTransitionStore) TransitionToAlias(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error) {
	return f.alias, nil
}

func (f fakeTransitionStore) TransitionToFile(root, home string, baseline state.FileBaseline) (state.FileBaseline, error) {
	return f.baseline, nil
}

type fakeRetirementStore struct {
	file  state.FileBaseline
	alias state.AliasBaseline
}

func (f fakeRetirementStore) RetireFileBaseline(root, home, target string) (state.FileBaseline, error) {
	return f.file, nil
}

func (f fakeRetirementStore) RetireAliasBaseline(root, home, aliasPath string) (state.AliasBaseline, error) {
	return f.alias, nil
}

type fakeSecretClient struct{}

func (f fakeSecretClient) ValidateCandidate(context.Context, secrets.Candidate) ([]byte, error) {
	return nil, nil
}

type fakeReplacer struct{}

func (f fakeReplacer) ReplaceResult(context.Context, filesystem.Precondition, filesystem.ReplacementSpec) (filesystem.ReplaceResult, error) {
	return filesystem.ReplaceResult{}, nil
}

func (f fakeReplacer) RealizeAlias(context.Context, filesystem.Precondition, filesystem.AliasSpec) (filesystem.AliasRealization, error) {
	return 0, nil
}

type fakeHookExecutor struct{}

func (f fakeHookExecutor) Execute(context.Context, hooks.ExecuteInput, []deployment.Hook) error {
	return nil
}

type fakeProbe struct{}

func (f fakeProbe) Probe(context.Context) error {
	return nil
}

type fakeResolver struct{}

func (f fakeResolver) Resolve(context.Context, DecisionRequest) (DecisionResponse, error) {
	return DecisionResponse{}, nil
}

func testContractDependencySeams(t *testing.T) {
	dependencies := Dependencies{
		RepositorySource: fakeRepositorySource{identity: RepositoryIdentity{Root: "/repo", Home: "/home"}},
		Compiler:         fakeCompiler{},
		State:            fakeStateReader{},
		Baselines:        fakeBaselineStore{},
		Transitions:      fakeTransitionStore{},
		Retirements:      fakeRetirementStore{},
		Client:           fakeSecretClient{},
		Secrets:          &secrets.Client{},
		Replacer:         fakeReplacer{},
		Hooks:            fakeHookExecutor{},
		Probe:            fakeProbe{},
		Resolver:         fakeResolver{},
		ProtectedTrees:   []string{},
		Platform:         "linux",
	}
	for _, seam := range []any{dependencies.RepositorySource, dependencies.Compiler, dependencies.State,
		dependencies.Baselines, dependencies.Transitions, dependencies.Retirements, dependencies.Client,
		dependencies.Replacer, dependencies.Hooks, dependencies.Probe, dependencies.Resolver} {
		if seam == nil {
			t.Fatal("every dependency seam must accept fakes")
		}
	}
	if dependencies.Platform != "linux" || dependencies.Secrets == nil {
		t.Fatal("platform, secrets, and protected trees must be preserved")
	}
}

func testContractCleanTypes(t *testing.T) {
	assertCleanImports(t)
	if containsBackendField(DecisionRequest{}) || containsBackendField(DecisionResponse{}) ||
		containsBackendField(SafeDifference{}) || containsBackendField(Request{}) || containsBackendField(Result{}) {
		t.Fatal("a CLI-facing DTO must expose only application-owned fields")
	}
}

// assertCleanImports requires every production source of this package to
// avoid Cobra, pflag, and the CLI package entirely.
func assertCleanImports(t *testing.T) {
	t.Helper()
	for _, name := range packageSources(t) {
		assertSourceImports(t, name)
	}
}

func assertSourceImports(t *testing.T, name string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	for _, spec := range file.Imports {
		if isForbiddenImport(strings.Trim(spec.Path.Value, `"`)) {
			t.Fatalf("%s imports %q", name, spec.Path.Value)
		}
	}
}

func containsBackendField(value any) bool {
	reflected := reflect.TypeOf(value)
	for index := 0; index < reflected.NumField(); index++ {
		field := reflected.Field(index)
		if field.Type.PkgPath() != "" && !strings.Contains(field.Type.PkgPath(), "/internal/application/") {
			return true
		}
	}
	return false
}

func packageSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			sources = append(sources, name)
		}
	}
	return sources
}

func isForbiddenImport(path string) bool {
	return strings.HasPrefix(path, "github.com/spf13/cobra") ||
		strings.HasPrefix(path, "github.com/spf13/pflag") ||
		strings.HasSuffix(path, "/internal/cli")
}

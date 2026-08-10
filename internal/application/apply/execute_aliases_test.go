package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestApplyAliasExecution(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"intact transition", testAliasIntactTransition},
		{"drifted transition", testAliasDriftedTransition},
		{"alias race", testAliasRace},
		{"state rollback", testAliasStateRollback},
		{"deleted scopes", testAliasRetirement},
		{"continuation", testAliasContinuation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// transitionFake records representation switches and can fail.
type transitionFake struct {
	calls int
	err   error
}

func (t *transitionFake) TransitionToAlias(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error) {
	t.calls++
	if t.err != nil {
		return state.AliasBaseline{}, t.err
	}
	return baseline, nil
}

func (t *transitionFake) TransitionToFile(root, home string, baseline state.FileBaseline) (state.FileBaseline, error) {
	t.calls++
	if t.err != nil {
		return state.FileBaseline{}, t.err
	}
	return baseline, nil
}

// retirementFake records retirements and can fail.
type retirementFake struct {
	fileCalls  int
	aliasCalls int
	err        error
}

func (r *retirementFake) RetireFileBaseline(root, home, target string) (state.FileBaseline, error) {
	r.fileCalls++
	if r.err != nil {
		return state.FileBaseline{}, r.err
	}
	return state.FileBaseline{}, nil
}

func (r *retirementFake) RetireAliasBaseline(root, home, aliasPath string) (state.AliasBaseline, error) {
	r.aliasCalls++
	if r.err != nil {
		return state.AliasBaseline{}, r.err
	}
	return state.AliasBaseline{}, nil
}

// evalAliasPlan freezes one platform plan over the given aliases.
func evalAliasPlan(t *testing.T, repo string, aliases ...deployment.Alias) deployment.Plan {
	t.Helper()
	plan, err := deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: repo, Platform: "linux", Aliases: aliases,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

// aliasPair bundles one evaluated alias apply with its seams.
type aliasPair struct {
	service     *Service
	candidates  Candidates
	home        string
	transitions *transitionFake
	retirements *retirementFake
}

// aliasFixture evaluates one alias entry over a regular target with the
// given target bytes and baseline rows.
func aliasFixture(t *testing.T, targetContent []byte, rows stateRows) aliasPair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	alias := toolAlias(t)
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if targetContent != nil {
		if err := os.WriteFile(filepath.Join(home, "bin", "tool"), targetContent, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	transitions := &transitionFake{}
	retirements := &retirementFake{}
	service := evalFixture(t, evalInput{
		repo: repo, home: home, plan: evalAliasPlan(t, repo, alias),
		rows: rows, transitions: transitions, retirements: retirements, replacer: &replacerFake{},
	})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return aliasPair{service: service, candidates: candidates, home: home, transitions: transitions, retirements: retirements}
}

// toolAlias freezes the fixture link declaration.
func toolAlias(t *testing.T) deployment.Alias {
	t.Helper()
	alias, err := deployment.NewAlias(deployment.Alias{
		Scope: deployment.NewScope(""), Platform: "linux",
		AliasRelativePath: "bin/tool", CanonicalTargetRelativePath: "files/tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	return alias
}

// aliasPlan freezes one realize-alias action for the fixture link.
func aliasPlan(overwrite bool) PreparedPlan {
	return PreparedPlan{actions: NewActionPlan([]PlanAction{
		{TargetPath: "bin/tool", Kind: ActionKindRealizeAlias, Overwrite: overwrite},
	})}
}

// transitionRow freezes the active file baseline of the old representation.
func transitionRow(target string) state.FileBaseline {
	return state.FileBaseline{
		TargetPath: target, SourcePath: "files/tool",
		SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.Ordinary([]byte("tool content")),
		BaselineSourceHash:  deployment.Ordinary([]byte("tool content")),
		Status:              state.StatusActive,
	}
}

// requireLink checks the alias link payload at one path.
func requireLink(t *testing.T, path, payload string) {
	t.Helper()
	content, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("readlink %s: %v", path, err)
	}
	if content != payload {
		t.Fatalf("payload = %q, want %q", content, payload)
	}
}

func testAliasIntactTransition(t *testing.T) {
	pair := aliasFixture(t, []byte("tool content"), stateRows{files: []state.FileBaseline{transitionRow("bin/tool")}})
	results, err := pair.service.ExecuteAliases(context.Background(), aliasPlan(true), pair.candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(results) != 1 || results[0].Status != StatusCompleted {
		t.Fatalf("results = %+v, want one completed record", results)
	}
	requireLink(t, targetPath(pair.home, "bin/tool"), "../files/tool")
	if pair.transitions.calls != 1 {
		t.Fatalf("transition calls = %d, want 1", pair.transitions.calls)
	}
}

func testAliasDriftedTransition(t *testing.T) {
	pair := aliasFixture(t, []byte("drifted content"), stateRows{files: []state.FileBaseline{transitionRow("bin/tool")}})
	results, err := pair.service.ExecuteAliases(context.Background(), aliasPlan(true), pair.candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if results[0].Status != StatusCompleted {
		t.Fatalf("results = %+v, want completed", results)
	}
	requireLink(t, targetPath(pair.home, "bin/tool"), "../files/tool")
	if pair.transitions.calls != 1 {
		t.Fatalf("transition calls = %d, want 1", pair.transitions.calls)
	}
}

func testAliasRace(t *testing.T) {
	pair := aliasFixture(t, nil, stateRows{})
	if err := os.WriteFile(filepath.Join(pair.home, "bin", "tool"), []byte("intruder"), 0o600); err != nil {
		t.Fatal(err)
	}
	results, err := pair.service.ExecuteAliases(context.Background(), aliasPlan(false), pair.candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("race error = %v, want operational", err)
	}
	if results[0].Status != StatusPartial {
		t.Fatalf("results = %+v, want partial", results)
	}
	if pair.transitions.calls != 0 {
		t.Fatalf("no transition may follow a race, calls = %d", pair.transitions.calls)
	}
}

func testAliasStateRollback(t *testing.T) {
	pair := aliasFixture(t, []byte("tool content"), stateRows{files: []state.FileBaseline{transitionRow("bin/tool")}})
	pair.transitions.err = os.ErrNotExist
	results, err := pair.service.ExecuteAliases(context.Background(), aliasPlan(true), pair.candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("rollback error = %v, want operational", err)
	}
	if results[0].Status != StatusPartial {
		t.Fatalf("results = %+v, want partial", results)
	}
	requireLink(t, targetPath(pair.home, "bin/tool"), "../files/tool")
}

func testAliasRetirement(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "gone.conf", Relative: "files/gone"}, []byte("a"))
	rows := stateRows{files: []state.FileBaseline{targetRow("gone.conf", "", "files/gone")}}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), rows: rows, retirements: &retirementFake{}})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	plan := PreparedPlan{actions: NewActionPlan([]PlanAction{
		{TargetPath: "gone.conf", Kind: ActionKindRetireFile},
	})}
	writeTarget(t, targetPath(home, "gone.conf"), []byte("kept"))
	results, err := service.ExecuteAliases(context.Background(), plan, candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if results[0].Status != StatusCompleted {
		t.Fatalf("results = %+v, want completed", results)
	}
	if _, err := os.Stat(targetPath(home, "gone.conf")); err != nil {
		t.Fatal("retirement must never delete the deployed target")
	}
}

func testAliasContinuation(t *testing.T) {
	service, candidates, home := continuationFixture(t)
	plan := PreparedPlan{actions: NewActionPlan([]PlanAction{
		{TargetPath: "bin/a", Kind: ActionKindRealizeAlias},
		{TargetPath: "bin/b", Kind: ActionKindRealizeAlias},
	})}
	results, err := service.ExecuteAliases(context.Background(), plan, candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("continuation error = %v, want operational", err)
	}
	if len(results) != 2 || results[0].Status != StatusCompleted || results[1].Status != StatusPartial {
		t.Fatalf("results = %+v, want completed then partial", results)
	}
	requireLink(t, targetPath(home, "bin/a"), "../files/a")
}

// continuationFixture evaluates two aliases where the second path is
// occupied by an intruder file.
func continuationFixture(t *testing.T) (*Service, Candidates, string) {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	first := simpleAlias(t, "bin/a", "files/a")
	second := simpleAlias(t, "bin/b", "files/b")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "bin", "b"), []byte("intruder"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalAliasPlan(t, repo, first, second), replacer: &replacerFake{}, baselines: &baselineFake{}})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return service, candidates, home
}

// simpleAlias freezes one alias declaration.
func simpleAlias(t *testing.T, aliasPath, canonical string) deployment.Alias {
	t.Helper()
	alias, err := deployment.NewAlias(deployment.Alias{
		Scope: deployment.NewScope(""), Platform: "linux",
		AliasRelativePath: aliasPath, CanonicalTargetRelativePath: canonical,
	})
	if err != nil {
		t.Fatal(err)
	}
	return alias
}

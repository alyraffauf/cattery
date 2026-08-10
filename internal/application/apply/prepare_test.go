package apply

import (
	"context"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
)

func TestApplyPreparation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"dry run records", testPrepareDryRun},
		{"noninteractive refusal", testPrepareNoninteractive},
		{"skip decision", testPrepareSkip},
		{"no-op stays empty", testPrepareNoop},
		{"required hooks", testPrepareHooks},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// driftFixture evaluates one drifting target per name and returns the
// service and candidates.
func driftFixture(t *testing.T, targets ...string) (*Service, Candidates) {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	files := make([]deployment.ManagedFile, 0, len(targets))
	for _, name := range targets {
		files = append(files, ordinarySource(t, fileSpec{Repo: repo, Target: name, Relative: "files/" + name}, []byte("source "+name)))
		writeTarget(t, targetPath(home, name), []byte("target "+name))
	}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, files...)})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return service, candidates
}

// resolveAll collects the decisions of every pending candidate.
func resolveAll(t *testing.T, service *Service, candidates Candidates, choices ...DecisionChoice) CollectedDecisions {
	t.Helper()
	resolver := &resolverFake{}
	for _, choice := range choices {
		resolver.responses = append(resolver.responses, DecisionResponse{Choice: choice})
	}
	service.resolver = resolver
	collected, err := service.CollectDecisions(context.Background(), candidates)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return collected
}

func testPrepareDryRun(t *testing.T) {
	service, candidates := driftFixture(t, "a.conf")
	plan, err := service.Prepare(context.Background(), Request{DryRun: true, DryRunSet: true}, candidates, CollectedDecisions{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	records := plan.Records()
	if len(records) != 1 || records[0].Status != StatusPlanned || records[0].Kind != ActionKindWriteSource {
		t.Fatalf("dry-run records = %+v, want one planned write-source record", records)
	}
	if len(plan.Actions().Actions()) != 0 || plan.WithHooks() {
		t.Fatal("dry-run must carry no actions and no hooks")
	}
	if plan.Summary().Planned != 1 {
		t.Fatalf("summary = %+v, want one planned", plan.Summary())
	}
}

func testPrepareNoninteractive(t *testing.T) {
	service, candidates := driftFixture(t, "a.conf")
	_, err := service.Prepare(context.Background(), Request{NonInteractive: true, NonInteractiveSet: true}, candidates, CollectedDecisions{})
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("noninteractive refusal error = %v, want an invalid input failure", err)
	}
	plan, err := service.Prepare(context.Background(), Request{DryRun: true, NonInteractive: true, NonInteractiveSet: true}, candidates, CollectedDecisions{})
	if err != nil {
		t.Fatalf("dry-run must not refuse pending decisions: %v", err)
	}
	if len(plan.Records()) != 1 {
		t.Fatalf("dry-run records = %d, want 1", len(plan.Records()))
	}
}

func testPrepareSkip(t *testing.T) {
	service, candidates := driftFixture(t, "a.conf")
	decisions := resolveAll(t, service, candidates, ChoiceSkip)
	plan, err := service.Prepare(context.Background(), Request{}, candidates, decisions)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	records := plan.Records()
	if len(records) != 1 || records[0].Status != StatusPlanned {
		t.Fatalf("skip records = %+v, want one planned record", records)
	}
	if len(plan.Actions().Actions()) != 0 || plan.WithHooks() {
		t.Fatal("a skipped plan must carry no actions and no hooks")
	}
}

func testPrepareNoop(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("same"))
	writeTarget(t, targetPath(home, "a.conf"), []byte("same"))
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file)})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	plan, err := service.Prepare(context.Background(), Request{}, candidates, CollectedDecisions{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(plan.Actions().Actions()) != 0 || len(plan.Records()) != 0 || plan.WithHooks() {
		t.Fatal("a converged apply must stay empty")
	}
}

func testPrepareHooks(t *testing.T) {
	service, candidates := driftFixture(t, "a.conf")
	decisions := resolveAll(t, service, candidates, ChoiceOverwrite)
	plan, err := service.Prepare(context.Background(), Request{}, candidates, decisions)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !plan.WithHooks() {
		t.Fatal("an executing plan with actions must run hooks")
	}
	actions := plan.Actions().Actions()
	if len(actions) != 1 || actions[0].Kind != ActionKindWriteSource || actions[0].TargetPath != "a.conf" {
		t.Fatalf("actions = %+v, want one write-source action", actions)
	}
	plan, err = service.Prepare(context.Background(), Request{NoHooks: true, NoHooksSet: true}, candidates, decisions)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if plan.WithHooks() {
		t.Fatal("--no-hooks must suppress hooks")
	}
	plan, err = service.Prepare(context.Background(), Request{DryRun: true, DryRunSet: true}, candidates, CollectedDecisions{})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if plan.WithHooks() {
		t.Fatal("dry-run must suppress hooks")
	}
}

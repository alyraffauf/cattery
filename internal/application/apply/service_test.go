package apply

import (
	"context"
	"errors"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
)

func TestApplyService(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"no-op apply", testServiceNoop},
		{"dry-run branch", testServiceDryRun},
		{"exact phase order", testServiceOrder},
		{"evaluate failure", testServiceEvaluateFailure},
		{"preflight failure", testServicePreflightFailure},
		{"decision failure", testServiceDecisionFailure},
		{"prepare refusal", testServicePrepareRefusal},
		{"partial results preserved", testServicePartialPreserved},
		{"verification downgrades", testServiceVerifyDowngrade},
		{"cancellation", testServiceCancellation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// servicePair bundles a fully wired apply service for one drifted target.
type servicePair struct {
	service   *Service
	home      string
	resolver  *resolverFake
	probe     *probeFake
	baselines *baselineFake
	replacer  *replacerFake
}

// serviceFixture wires every apply phase over one drifted target.
func serviceFixture(t *testing.T) servicePair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source"))
	writeTarget(t, targetPath(home, "a.conf"), []byte("target"))
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceOverwrite}}}
	probe := &probeFake{}
	baselines := &baselineFake{}
	replacer := &replacerFake{}
	service := evalFixture(t, evalInput{
		repo: repo, home: home, plan: evalPlan(t, repo, file),
		resolver: resolver, probe: probe, baselines: baselines, replacer: replacer, hooks: &hookFake{},
	})
	return servicePair{service: service, home: home, resolver: resolver, probe: probe, baselines: baselines, replacer: replacer}
}

func testServiceNoop(t *testing.T) {
	pair := serviceFixture(t)
	result, err := pair.service.Apply(context.Background(), Request{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != StatusCompleted {
		t.Fatalf("result = %+v, want one completed record", result.Items)
	}
	if result.Summary.Completed != 1 {
		t.Fatalf("summary = %+v, want one completed", result.Summary)
	}
}

func testServiceDryRun(t *testing.T) {
	pair := serviceFixture(t)
	result, err := pair.service.Apply(context.Background(), Request{DryRun: true, DryRunSet: true})
	if err == nil || !kindIs(err, failure.Difference) {
		t.Fatalf("apply: %v, want a difference failure for a pending dry run", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != StatusPlanned {
		t.Fatalf("result = %+v, want one planned record", result.Items)
	}
	if pair.replacer.calls != 0 || pair.baselines.calls != 0 {
		t.Fatalf("dry run must not write, replacer = %d baselines = %d", pair.replacer.calls, pair.baselines.calls)
	}
}

func testServiceOrder(t *testing.T) {
	pair := serviceFixture(t)
	result, err := pair.service.Apply(context.Background(), Request{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(pair.resolver.requests) != 1 || pair.replacer.calls != 1 || result.Summary.Completed != 1 {
		t.Fatalf("order: requests = %v replacer = %d summary = %+v, want decision, write, complete", pair.resolver.requests, pair.replacer.calls, result.Summary)
	}
}

func testServiceEvaluateFailure(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo), compilerErr: errors.New("broken repo")})
	_, err := service.Apply(context.Background(), Request{})
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("evaluate failure error = %v, want invalid input", err)
	}
}

func testServicePreflightFailure(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	probe := &probeFake{err: failure.New(failure.Dependency, "apply: sops missing", nil)}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), probe: probe, client: sopsClient(t, []byte("plaintext"))})
	_, err := service.Apply(context.Background(), Request{})
	if err == nil || !kindIs(err, failure.Dependency) {
		t.Fatalf("preflight failure error = %v, want a dependency failure", err)
	}
}

func testServiceDecisionFailure(t *testing.T) {
	pair := serviceFixture(t)
	pair.resolver.err = failure.New(failure.InvalidInput, "cli: EOF", nil)
	_, err := pair.service.Apply(context.Background(), Request{})
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("decision failure error = %v, want invalid input", err)
	}
	if pair.replacer.calls != 0 {
		t.Fatalf("no write may follow a decision failure, calls = %d", pair.replacer.calls)
	}
}

func testServicePrepareRefusal(t *testing.T) {
	pair := serviceFixture(t)
	_, err := pair.service.Apply(context.Background(), Request{NonInteractive: true, NonInteractiveSet: true})
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("refusal error = %v, want invalid input", err)
	}
	if pair.replacer.calls != 0 || pair.baselines.calls != 0 {
		t.Fatalf("a refused apply must register nothing, replacer = %d baselines = %d", pair.replacer.calls, pair.baselines.calls)
	}
}

func testServicePartialPreserved(t *testing.T) {
	pair := serviceFixture(t)
	pair.replacer.err = errors.New("write failed")
	result, err := pair.service.Apply(context.Background(), Request{})
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("pipeline failure error = %v, want operational", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != StatusPartial {
		t.Fatalf("result = %+v, want the partial record preserved", result.Items)
	}
	if result.Summary.Partial != 1 {
		t.Fatalf("summary = %+v, want one partial", result.Summary)
	}
}

func testServiceVerifyDowngrade(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source"))
	writeTarget(t, targetPath(home, "a.conf"), []byte("target"))
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceOverwrite}}}
	service := evalFixture(t, evalInput{
		repo: repo, home: home, plan: evalPlan(t, repo, file),
		resolver: resolver, baselines: &baselineFake{}, replacer: &corruptReplacer{}, hooks: &hookFake{},
	})
	result, err := service.Apply(context.Background(), Request{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Summary.Partial != 1 {
		t.Fatalf("summary = %+v, want one partial after post-hook drift", result.Summary)
	}
}

// corruptReplacer writes one extra byte so the deployed target never
// matches its source, simulating a hook edit between write and verify.
type corruptReplacer struct {
	replacerFake
}

func (c *corruptReplacer) ReplaceResult(ctx context.Context, precondition filesystem.Precondition, spec filesystem.ReplacementSpec) (filesystem.ReplaceResult, error) {
	spec.Content = append(append([]byte(nil), spec.Content...), 'x')
	return c.replacerFake.ReplaceResult(ctx, precondition, spec)
}

func testServiceCancellation(t *testing.T) {
	pair := serviceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pair.service.Apply(ctx, Request{})
	if err == nil {
		t.Fatal("cancelled apply must fail")
	}
}

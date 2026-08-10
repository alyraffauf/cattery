package apply

import (
	"context"
	"errors"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/hooks"
)

func TestApplyHookPipeline(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"no-op stays quiet", testHooksNoop},
		{"dry-run runs nothing", testHooksDryRun},
		{"no-hooks runs executors", testHooksNoHooks},
		{"before failure writes nothing", testHooksBeforeFailure},
		{"executor failure skips after", testHooksSkippedAfter},
		{"partial result for skips", testHooksPartial},
		{"aggregated after failures", testHooksAfterFailure},
		{"cancellation", testHooksCancellation},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// hookFake records phases and result values and can fail.
type hookFake struct {
	phases  []deployment.HookPhase
	results []string
	calls   int
	failOn  int
	err     error
}

func (h *hookFake) Execute(ctx context.Context, input hooks.ExecuteInput, ordered []deployment.Hook) error {
	h.calls++
	h.phases = append(h.phases, input.Phase)
	h.results = append(h.results, input.Result)
	if h.failOn > 0 && h.calls >= h.failOn {
		return h.err
	}
	return nil
}

// hookPair bundles the hook-pipeline seams of one drifted apply.
type hookPair struct {
	service     *Service
	candidates  Candidates
	plan        PreparedPlan
	hooks       *hookFake
	baselines   *baselineFake
	replacer    *replacerFake
	home        string
	transitions *transitionFake
}

// hookFixture evaluates one drifting file target over a write plan.
func hookFixture(t *testing.T, withHooks bool) hookPair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source"))
	writeTarget(t, targetPath(home, "a.conf"), []byte("target"))
	hk := &hookFake{}
	baselines := &baselineFake{}
	replacer := &replacerFake{}
	transitions := &transitionFake{}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), baselines: baselines, replacer: replacer, hooks: hk, transitions: transitions})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	plan := PreparedPlan{actions: NewActionPlan([]PlanAction{{TargetPath: "a.conf", Kind: ActionKindWriteSource, SourcePath: "files/a"}}), withHooks: withHooks}
	return hookPair{service: service, candidates: candidates, plan: plan, hooks: hk, baselines: baselines, replacer: replacer, home: home, transitions: transitions}
}

func testHooksNoop(t *testing.T) {
	pair := hookFixture(t, false)
	pair.plan.actions = NewActionPlan(nil)
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if len(records) != 0 || pair.hooks.calls != 0 || pair.replacer.calls != 0 {
		t.Fatalf("records = %+v hooks = %d replacer = %d, want all quiet", records, pair.hooks.calls, pair.replacer.calls)
	}
}

func testHooksDryRun(t *testing.T) {
	pair := hookFixture(t, false)
	pair.plan.actions = NewActionPlan(nil)
	pair.plan.records = []ItemResult{{TargetPath: "a.conf", Status: StatusPlanned, Kind: ActionKindWriteSource}}
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{DryRun: true}, Plan: pair.plan, Candidates: pair.candidates})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if len(records) != 1 || records[0].Status != StatusPlanned || pair.hooks.calls != 0 || pair.replacer.calls != 0 {
		t.Fatalf("records = %+v hooks = %d replacer = %d, want the planned record only", records, pair.hooks.calls, pair.replacer.calls)
	}
}

func testHooksNoHooks(t *testing.T) {
	pair := hookFixture(t, false)
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{NoHooks: true, NoHooksSet: true}, Plan: pair.plan, Candidates: pair.candidates})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if records[0].Status != StatusCompleted || pair.hooks.calls != 0 {
		t.Fatalf("records = %+v hooks = %d, want one completed write without hooks", records, pair.hooks.calls)
	}
}

func testHooksBeforeFailure(t *testing.T) {
	pair := hookFixture(t, true)
	pair.hooks.failOn = 1
	pair.hooks.err = errors.New("hook exploded")
	_, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err == nil || !kindIs(err, failure.Hook) {
		t.Fatalf("before failure error = %v, want a hook failure", err)
	}
	if pair.replacer.calls != 0 || pair.baselines.calls != 0 {
		t.Fatalf("before-hook failure must cause zero writes, replacer = %d baselines = %d", pair.replacer.calls, pair.baselines.calls)
	}
	if len(pair.hooks.phases) != 1 || pair.hooks.phases[0] != deployment.HookBefore {
		t.Fatalf("phases = %v, want before only", pair.hooks.phases)
	}
}

func testHooksSkippedAfter(t *testing.T) {
	pair := hookFixture(t, true)
	pair.replacer.err = errors.New("write failed")
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("executor failure error = %v, want operational", err)
	}
	if len(pair.hooks.phases) != 1 || pair.hooks.phases[0] != deployment.HookBefore {
		t.Fatalf("after hooks must be skipped on a mid-filesystem failure, phases = %v", pair.hooks.phases)
	}
	if len(records) != 1 || records[0].Status != StatusPartial {
		t.Fatalf("records = %+v, want one partial record", records)
	}
}

func testHooksPartial(t *testing.T) {
	pair := hookFixture(t, true)
	pair.plan.records = []ItemResult{{TargetPath: "b.conf", Status: StatusPlanned, Kind: ActionKindWriteSource}}
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if len(pair.hooks.results) != 2 || pair.hooks.results[0] != "pending" || pair.hooks.results[1] != "partial" {
		t.Fatalf("results = %v, want pending then partial", pair.hooks.results)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want the skip and the write", len(records))
	}
}

func testHooksAfterFailure(t *testing.T) {
	pair := hookFixture(t, true)
	pair.hooks.failOn = 2
	pair.hooks.err = errors.New("after exploded")
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err == nil || !kindIs(err, failure.Hook) {
		t.Fatalf("after failure error = %v, want a hook failure", err)
	}
	if len(records) != 1 || records[0].Status != StatusCompleted {
		t.Fatalf("completed writes must survive an after failure, records = %+v", records)
	}
}

func testHooksCancellation(t *testing.T) {
	pair := hookFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pair.service.RunHookPipeline(ctx, PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err == nil {
		t.Fatal("cancelled pipeline must fail")
	}
}

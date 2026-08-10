package apply

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestApplyFileExecution(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"create new target", testExecuteCreate},
		{"update existing", testExecuteUpdate},
		{"mode correction", testExecuteModeFix},
		{"baseline failure is partial", testExecuteBaselineFailure},
		{"replace failure", testExecuteReplaceFailure},
		{"equality recovery", testExecuteRecovery},
		{"secret write clears", testExecuteSecret},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// baselineFake records upserts and can fail from a given call onward.
type baselineFake struct {
	baseline state.FileBaseline
	calls    int
	failCall int
}

func (b *baselineFake) UpsertFileBaseline(root, home string, baseline state.FileBaseline) (state.FileBaseline, error) {
	b.calls++
	if b.failCall > 0 && b.calls >= b.failCall {
		return state.FileBaseline{}, os.ErrNotExist
	}
	b.baseline = baseline
	return baseline, nil
}

func (b *baselineFake) UpsertAliasBaseline(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error) {
	b.calls++
	if b.failCall > 0 && b.calls >= b.failCall {
		return state.AliasBaseline{}, os.ErrNotExist
	}
	return baseline, nil
}

// replacerFake delegates to the real replacer and can fail or count.
type replacerFake struct {
	calls int
	err   error
}

func (r *replacerFake) ReplaceResult(ctx context.Context, precondition filesystem.Precondition, spec filesystem.ReplacementSpec) (filesystem.ReplaceResult, error) {
	r.calls++
	if r.err != nil {
		return filesystem.ReplaceResult{}, r.err
	}
	return filesystem.NewReplacer().ReplaceResult(ctx, precondition, spec)
}

func (r *replacerFake) RealizeAlias(ctx context.Context, precondition filesystem.Precondition, spec filesystem.AliasSpec) (filesystem.AliasRealization, error) {
	r.calls++
	if r.err != nil {
		return 0, r.err
	}
	return filesystem.NewReplacer().RealizeAlias(ctx, precondition, spec)
}

// executePair bundles one evaluated service, its candidates, and the home.
type executePair struct {
	service    *Service
	candidates Candidates
	home       string
	baselines  *baselineFake
	replacer   *replacerFake
}

// executeFixture evaluates one source/target pair with the given target
// content and returns the seams for execution.
func executeFixture(t *testing.T, targetContent []byte) executePair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source"))
	if targetContent != nil {
		writeTarget(t, targetPath(home, "a.conf"), targetContent)
	}
	baselines := &baselineFake{}
	replacer := &replacerFake{}
	service := evalFixture(t, evalInput{
		repo: repo, home: home, plan: evalPlan(t, repo, file),
		baselines: baselines, replacer: replacer,
	})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return executePair{service: service, candidates: candidates, home: home, baselines: baselines, replacer: replacer}
}

// filePlan builds one write-source plan over the candidate target.
func filePlan(target string) PreparedPlan {
	return PreparedPlan{actions: NewActionPlan([]PlanAction{{TargetPath: target, Kind: ActionKindWriteSource, SourcePath: "files/a"}})}
}

// targetContent reads the HOME-relative target bytes.
func targetContent(t *testing.T, home, relative string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	return content
}

func testExecuteCreate(t *testing.T) {
	pair := executeFixture(t, nil)
	results, err := pair.service.ExecuteFiles(context.Background(), filePlan("a.conf"), pair.candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(results) != 1 || results[0].Status != StatusCompleted {
		t.Fatalf("results = %+v, want one completed record", results)
	}
	if string(targetContent(t, pair.home, "a.conf")) != "source" {
		t.Fatal("target must carry the exact source bytes")
	}
	if pair.baselines.calls != 1 || pair.baselines.baseline.Status != state.StatusActive {
		t.Fatalf("baseline = %+v, want one active row", pair.baselines.baseline)
	}
	if pair.replacer.calls != 1 {
		t.Fatalf("replacer calls = %d, want 1", pair.replacer.calls)
	}
}

func testExecuteUpdate(t *testing.T) {
	pair := executeFixture(t, []byte("stale"))
	results, err := pair.service.ExecuteFiles(context.Background(), filePlan("a.conf"), pair.candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if results[0].Status != StatusCompleted || string(targetContent(t, pair.home, "a.conf")) != "source" {
		t.Fatalf("target must be updated and completed, results = %+v", results)
	}
}

func testExecuteModeFix(t *testing.T) {
	pair := modeFixture(t)
	results, err := pair.service.ExecuteFiles(context.Background(), filePlan("a.conf"), pair.candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if results[0].Status != StatusCompleted {
		t.Fatalf("results = %+v, want completed", results)
	}
	info, err := os.Stat(targetPath(pair.home, "a.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("mode = %o, want preserved executable bits", info.Mode().Perm())
	}
	if pair.replacer.calls != 1 {
		t.Fatalf("mode fix must rewrite the target, calls = %d", pair.replacer.calls)
	}
}

func testExecuteBaselineFailure(t *testing.T) {
	pair := twoTargetFixture(t)
	pair.baselines.failCall = 2
	plan := twoTargetPlan()
	results, err := pair.service.ExecuteFiles(context.Background(), plan, pair.candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("baseline failure error = %v, want operational", err)
	}
	if len(results) != 2 || results[0].Status != StatusCompleted || results[1].Status != StatusPartial {
		t.Fatalf("results = %+v, want completed then partial", results)
	}
	if string(targetContent(t, pair.home, "a.conf")) != "source a" {
		t.Fatal("the earlier target must remain accurate")
	}
}

// twoTargetFixture evaluates two drifting sources with equal target bytes.
func twoTargetFixture(t *testing.T) executePair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	first := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source a"))
	second := ordinarySource(t, fileSpec{Repo: repo, Target: "b.conf", Relative: "files/b"}, []byte("source b"))
	baselines := &baselineFake{}
	replacer := &replacerFake{}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, first, second), baselines: baselines, replacer: replacer})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return executePair{service: service, candidates: candidates, home: home, baselines: baselines, replacer: replacer}
}

// twoTargetPlan freezes the write-source actions of both targets.
func twoTargetPlan() PreparedPlan {
	return PreparedPlan{actions: NewActionPlan([]PlanAction{
		{TargetPath: "a.conf", Kind: ActionKindWriteSource, SourcePath: "files/a"},
		{TargetPath: "b.conf", Kind: ActionKindWriteSource, SourcePath: "files/b"},
	})}
}

func testExecuteReplaceFailure(t *testing.T) {
	pair := executeFixture(t, []byte("stale"))
	pair.replacer.err = os.ErrPermission
	_, err := pair.service.ExecuteFiles(context.Background(), filePlan("a.conf"), pair.candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("replace failure error = %v, want operational", err)
	}
	if string(targetContent(t, pair.home, "a.conf")) != "stale" {
		t.Fatal("a failed replace must leave the old target intact")
	}
	if pair.baselines.calls != 0 {
		t.Fatalf("no baseline may be written after a failed replace, calls = %d", pair.baselines.calls)
	}
}

func testExecuteRecovery(t *testing.T) {
	pair := executeFixture(t, nil)
	pair.baselines.failCall = 1
	results, err := pair.service.ExecuteFiles(context.Background(), filePlan("a.conf"), pair.candidates)
	if err == nil || results[0].Status != StatusPartial {
		t.Fatalf("results = %+v err = %v, want partial plus an error", results, err)
	}
	pair.baselines.failCall = 0
	results, err = pair.service.ExecuteFiles(context.Background(), filePlan("a.conf"), pair.candidates)
	if err != nil || results[0].Status != StatusCompleted {
		t.Fatalf("retry results = %+v err = %v, want completed", results, err)
	}
	if pair.replacer.calls != 1 {
		t.Fatalf("recovery must skip the second write, calls = %d", pair.replacer.calls)
	}
}

func testExecuteSecret(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	baselines := &baselineFake{}
	replacer := &replacerFake{}
	service := evalFixture(t, evalInput{
		repo: repo, home: home, plan: evalPlan(t, repo, file),
		client: sopsClient(t, []byte("plaintext")), baselines: baselines, replacer: replacer,
	})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	plan := PreparedPlan{actions: NewActionPlan([]PlanAction{{TargetPath: "target", Kind: ActionKindWriteSource, SourcePath: "files/token"}})}
	results, err := service.ExecuteFiles(context.Background(), plan, candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	assertSecretResult(t, secretOutcome{results: results, home: home, baselines: baselines})
}

// assertSecretResult verifies the completed secret record, the plaintext
// target with mode 0600, and the keyed baseline fingerprints.
func assertSecretResult(t *testing.T, outcome secretOutcome) {
	t.Helper()
	if len(outcome.results) != 1 || outcome.results[0].Status != StatusCompleted {
		t.Fatalf("results = %+v, want a completed secret record", outcome.results)
	}
	if string(targetContent(t, outcome.home, "target")) != "plaintext" {
		t.Fatal("the target must carry the decrypted plaintext")
	}
	info, err := os.Stat(targetPath(outcome.home, "target"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := fs.FileMode(info.Mode().Perm()); mode != 0o600 {
		t.Fatalf("secret mode = %o, want 0600", mode)
	}
	content := outcome.baselines.baseline.BaselineContentHash
	want := deployment.SecretSemantic([]byte("plaintext"), [32]byte{7})
	if content != want {
		t.Fatal("the baseline must carry the keyed content fingerprint")
	}
	if outcome.baselines.baseline.BaselineSourceHash != deployment.RawStorage([]byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)) {
		t.Fatal("the baseline must carry the raw ciphertext fingerprint")
	}
}

// secretOutcome bundles the results, home, and baselines of one secret
// execution.
type secretOutcome struct {
	results   []ItemResult
	home      string
	baselines *baselineFake
}

// modeFixture evaluates one executable-bit source against a 0644 target
// that already carries the exact content.
func modeFixture(t *testing.T) executePair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	source := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source"))
	source.SourceExecutableBits = 0o111
	writeTarget(t, targetPath(home, "a.conf"), []byte("source"))
	if err := os.Chmod(targetPath(home, "a.conf"), 0o644); err != nil {
		t.Fatal(err)
	}
	baselines := &baselineFake{}
	replacer := &replacerFake{}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, source), baselines: baselines, replacer: replacer})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return executePair{service: service, candidates: candidates, home: home, baselines: baselines, replacer: replacer}
}

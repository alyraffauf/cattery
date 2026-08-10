package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestApplyEvaluation(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"deterministic order", testApplyEvaluationOrder},
		{"state-only scopes", testApplyEvaluationStateOnly},
		{"invalid unselected scope", testApplyEvaluationInvalidGroup},
		{"secret demand", testApplyEvaluationSecretDemand},
		{"source race", testApplyEvaluationSourceRace},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// planCompiler returns a frozen plan from its fields.
type planCompiler struct {
	plan deployment.Plan
	err  error
}

func (c planCompiler) Compile(repository.CompileInput) (deployment.Plan, error) {
	return c.plan, c.err
}

// evalFixture builds isolated home/repository directories and an evaluation
// service over the given plan and rows.
// evalInput bundles the isolated directories, plan, rows, and key behavior
// of one evaluation fixture.
type evalInput struct {
	repo        string
	home        string
	plan        deployment.Plan
	rows        stateRows
	keyErr      error
	probe       DependencyProbe
	client      *secrets.Client
	replacer    AtomicReplacer
	baselines   BaselineStore
	transitions TransitionStore
	retirements RetirementStore
	hooks       HookExecutor
}

// evalFixture builds an evaluation service over the frozen input.
func evalFixture(t *testing.T, input evalInput) *Service {
	t.Helper()
	return NewService(Dependencies{
		RepositorySource: fakeRepositorySource{identity: RepositoryIdentity{Root: input.repo, Home: input.home}},
		Compiler:         planCompiler{plan: input.plan},
		State:            fakeStateReader{files: input.rows.files, aliases: input.rows.aliases, key: [32]byte{7}, keyErr: input.keyErr},
		Baselines:        input.baselines,
		Transitions:      input.transitions,
		Retirements:      input.retirements,
		Hooks:            input.hooks,
		Secrets:          input.client,
		Replacer:         input.replacer,
		Probe:            input.probe,
		ProtectedTrees:   []string{filepath.Join(input.repo, "state")},
		Platform:         "linux",
	})
}

// evalRequest builds a request for the given raw groups.
func evalRequest(groups ...string) Request {
	return Request{Groups: groups}
}

// ordinarySource writes one ordinary source and returns its managed file.
func ordinarySource(t *testing.T, spec fileSpec, content []byte) deployment.ManagedFile {
	t.Helper()
	path := filepath.Join(spec.Repo, filepath.FromSlash(spec.Relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return managedFile(t, spec, deployment.FileOrdinary)
}

// secretSource writes one valid sops envelope source and returns its file.
func secretSource(t *testing.T, spec fileSpec) deployment.ManagedFile {
	t.Helper()
	path := filepath.Join(spec.Repo, filepath.FromSlash(spec.Relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	envelope := []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)
	if err := os.WriteFile(path, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	return managedFile(t, spec, deployment.FileSecret)
}

// managedFile freezes one plan file entry at the given source path.
// fileSpec names one planned file: the repository, the HOME-relative target,
// and the repository-relative source path.
type fileSpec struct {
	Repo     string
	Target   string
	Relative string
}

// managedFile freezes one plan file entry over the spec.
func managedFile(t *testing.T, spec fileSpec, kind deployment.FileKind) deployment.ManagedFile {
	t.Helper()
	path := filepath.Join(spec.Repo, filepath.FromSlash(spec.Relative))
	file, err := deployment.NewManagedFile(deployment.ManagedFile{
		Scope: deployment.NewScope(""), Layer: deployment.LayerBase, Kind: kind,
		SourceAbsolutePath: path, SourceRepositoryPath: spec.Relative, TargetRelativePath: spec.Target,
	})
	if err != nil {
		t.Fatalf("managed file %s: %v", spec.Relative, err)
	}
	return file
}

// evalPlan freezes one platform plan over the given files.
func evalPlan(t *testing.T, repo string, files ...deployment.ManagedFile) deployment.Plan {
	t.Helper()
	plan, err := deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: repo, Platform: "linux", Files: files,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

// targetRow freezes one active ordinary baseline at the given target path.
func targetRow(target, group, source string) state.FileBaseline {
	return state.FileBaseline{
		TargetPath: target, GroupName: group, SourcePath: source,
		SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.Ordinary([]byte("baseline-" + target)),
		BaselineSourceHash:  deployment.Ordinary([]byte("storage-" + target)),
		Status:              state.StatusActive,
	}
}

// writeTarget creates the HOME-relative target file.
func writeTarget(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// targetPath joins one HOME-relative target path.
func targetPath(home, relative string) string {
	return filepath.Join(home, filepath.FromSlash(relative))
}

// requirePaths checks the candidate target paths in bytewise order.
func requirePaths(t *testing.T, candidates Candidates, want ...string) {
	t.Helper()
	records := candidates.All()
	if len(records) != len(want) {
		t.Fatalf("records = %d, want %d", len(records), len(want))
	}
	for index, record := range records {
		if record.record.TargetPath != want[index] {
			t.Fatalf("record %d path = %q, want %q", index, record.record.TargetPath, want[index])
		}
	}
}

func testApplyEvaluationOrder(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	first := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("a"))
	second := ordinarySource(t, fileSpec{Repo: repo, Target: "c.conf", Relative: "files/c"}, []byte("c"))
	middle := ordinarySource(t, fileSpec{Repo: repo, Target: "b.conf", Relative: "files/b"}, []byte("b"))
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, second, middle, first)})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	requirePaths(t, candidates, "a.conf", "b.conf", "c.conf")
	for _, record := range candidates.All() {
		if record.file.Action != reconcile.ActionCreateTarget {
			t.Fatalf("action for %s = %v, want create target", record.record.TargetPath, record.file.Action)
		}
	}
}

func testApplyEvaluationStateOnly(t *testing.T) {
	repo := t.TempDir()
	plan, err := deployment.NewPlan(deployment.PlanInput{RepositoryRoot: repo, Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: plan, rows: stateRows{files: []state.FileBaseline{targetRow("gone", "", "files/gone")}}})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	requirePaths(t, candidates, "gone")
	if candidates.All()[0].retirement.Action != reconcile.ActionRetireState {
		t.Fatalf("retirement action = %v, want retire state", candidates.All()[0].retirement.Action)
	}
}

func testApplyEvaluationInvalidGroup(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo)})
	_, err := service.Evaluate(context.Background(), evalRequest("missing"))
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("unknown group error = %v, want an invalid input failure", err)
	}
}

func testApplyEvaluationSecretDemand(t *testing.T) {
	repo := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	plan := evalPlan(t, repo, file)
	cipher := []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)
	rows := stateRows{files: []state.FileBaseline{{
		TargetPath: "target", SourcePath: "files/token", SourceKind: deployment.FileSecret, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.SecretSemantic([]byte("secret"), [32]byte{7}),
		BaselineSourceHash:  deployment.RawStorage(cipher),
		Status:              state.StatusActive,
	}}}
	home := t.TempDir()
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: plan, rows: rows})
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate secret: %v", err)
	}
	requirePaths(t, candidates, "target")
	if candidates.All()[0].file.Action != reconcile.ActionNoOp {
		t.Fatalf("secret action = %v, want a converged record", candidates.All()[0].file.Action)
	}
	service = evalFixture(t, evalInput{repo: repo, home: home, plan: plan, rows: rows, keyErr: os.ErrNotExist})
	if _, err := service.Evaluate(context.Background(), evalRequest()); err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("missing hash key error = %v, want an operational failure", err)
	}
}

func testApplyEvaluationSourceRace(t *testing.T) {
	repo := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "gone.conf", Relative: "files/gone"}, []byte("a"))
	_ = file
	plan := evalPlan(t, repo, file)
	if err := os.Remove(file.SourceAbsolutePath); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: plan})
	_, err := service.Evaluate(context.Background(), evalRequest())
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("vanished source error = %v, want an operational failure", err)
	}
}

// kindIs reports whether err carries the given failure kind.
func kindIs(err error, want failure.Kind) bool {
	kind, ok := failure.HasKind(err)
	return ok && kind == want
}

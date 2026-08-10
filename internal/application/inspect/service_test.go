package inspect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestInspectionEvaluation(t *testing.T) {
	t.Run("state-only selection evaluates only selected scopes", testEvaluationStateOnlySelection)
	t.Run("secret decryption happens only when required", testEvaluationDecryptConditions)
	t.Run("evaluations are deterministic", testEvaluationDeterminism)
	t.Run("injected failures categorize correctly", testEvaluationFailures)
}

type fixture struct {
	service *Service
	source  *fakeSource
	rows    *fakeState
	root    string
	home    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	source := &fakeSource{identity: RepositoryIdentity{Root: root, Home: home}}
	rows := &fakeState{}
	service := NewService(Dependencies{
		RepositorySource: source,
		Compiler:         compileFunc(repository.Compile),
		State:            rows,
		ProtectedTrees:   []string{filepath.Join(home, "state")},
		Platform:         "linux",
	})
	return &fixture{service: service, source: source, rows: rows, root: root, home: home}
}

type compileFunc func(repository.CompileInput) (deployment.Plan, error)

func (adapter compileFunc) Compile(input repository.CompileInput) (deployment.Plan, error) {
	return adapter(input)
}

type fakeSource struct {
	identity RepositoryIdentity
	fail     error
}

func (fake *fakeSource) Resolve(request selection.RepositoryRequest) (RepositoryIdentity, error) {
	if fake.fail != nil {
		return RepositoryIdentity{}, fake.fail
	}
	return fake.identity, nil
}

type fakeState struct {
	rows     stateRows
	key      [32]byte
	fail     error
	keyCalls int
}

func (fake *fakeState) FileBaselines(root, home string) ([]state.FileBaseline, error) {
	if fake.fail != nil {
		return nil, fake.fail
	}
	return fake.rows.files, nil
}

func (fake *fakeState) AliasBaselines(root, home string) ([]state.AliasBaseline, error) {
	if fake.fail != nil {
		return nil, fake.fail
	}
	return fake.rows.aliases, nil
}

func (fake *fakeState) RecoverHashKey() ([32]byte, error) {
	fake.keyCalls++
	return fake.key, nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePrivateFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repositoryTree(t *testing.T, groups []string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".bashrc"), "export X=1\n")
	for _, group := range groups {
		writeFile(t, filepath.Join(root, group, group+"-file.conf"), "source "+group+"\n")
	}
	return root
}

func useRepository(t *testing.T, fx *fixture, groups []string) {
	t.Helper()
	fx.root = repositoryTree(t, groups)
	fx.source.identity.Root = fx.root
}

type baselineInput struct {
	target  string
	group   string
	source  []byte
	content []byte
}

// fileRow builds an active baselined row; fingerprints follow the source
// kind: unkeyed for ordinary content, keyed for secrets.
func fileRow(input baselineInput, key *[32]byte) state.FileBaseline {
	row := state.FileBaseline{
		TargetPath: input.target, GroupName: input.group, SourcePath: input.target,
		Layer:              deployment.LayerBase,
		BaselineSourceHash: deployment.RawStorage(input.source),
		Status:             state.StatusActive,
	}
	if key == nil {
		row.SourceKind = deployment.FileOrdinary
		row.BaselineContentHash = deployment.Ordinary(input.content)
		return row
	}
	row.SourceKind = deployment.FileSecret
	row.BaselineContentHash = deployment.SecretSemantic(input.content, *key)
	return row
}

func secretFixtureJSON() []byte {
	return []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)
}

func assertKind(t *testing.T, err error, want failure.Kind) {
	t.Helper()
	if kind, matched := failure.HasKind(err); !matched || kind != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

// mustEvaluate evaluates one request and fails the test on error.
func mustEvaluate(t *testing.T, service *Service, request Request) Result {
	t.Helper()
	evaluation, err := service.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return evaluation
}

// assertPaths requires the evaluation records to carry exactly the paths.
func assertPaths(t *testing.T, evaluation Result, want []string) {
	t.Helper()
	var paths []string
	for _, evaluated := range evaluation.records {
		paths = append(paths, evaluated.record.TargetPath)
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

// assertSameEvaluation requires two evaluations to carry identical records.
func assertSameEvaluation(t *testing.T, first, second Result) {
	t.Helper()
	if len(first.records) != len(second.records) {
		t.Fatalf("record count = %d, want %d", len(second.records), len(first.records))
	}
	for index := range first.records {
		got, want := second.records[index], first.records[index]
		if got.record.TargetPath != want.record.TargetPath || got.file != want.file ||
			got.alias != want.alias || got.retirement != want.retirement {
			t.Fatalf("record %d differs", index)
		}
	}
}

// fakeClient wires a fake sops executable that echoes its stdin as plaintext.
func fakeClient(t *testing.T) *secrets.Client {
	t.Helper()
	executable := sops.Build(t)
	command, err := executable.Command(sops.Behavior{EchoStdin: true})
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewClient(executable.Path, t.TempDir(), command.Env)
}

func testEvaluationStateOnlySelection(t *testing.T) {
	fx := newFixture(t)
	useRepository(t, fx, []string{"g1"})
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
		fileRow(baselineInput{target: "old.conf", group: "g2", source: []byte("stale\n"), content: []byte("stale\n")}, nil),
	}}
	evaluation := mustEvaluate(t, fx.service, Request{Groups: []string{"g2"}})
	assertRetirement(t, evaluation)
	assertPaths(t, evaluation, []string{"old.conf"})
	assertPaths(t, mustEvaluate(t, fx.service, Request{}), []string{".bashrc", "g1-file.conf", "old.conf"})
	_, err := fx.service.Evaluate(context.Background(), Request{Groups: []string{"ghost"}})
	assertKind(t, err, failure.InvalidInput)
}

// assertRetirement requires the only record to be the pending tracking
// retirement of the state-only row.
func assertRetirement(t *testing.T, evaluation Result) {
	t.Helper()
	retirement := evaluation.records[0].retirement
	if retirement.Action != reconcile.ActionRetireFileState || retirement.Reason != reconcile.ReasonSourceRemoved {
		t.Fatalf("retirement = %+v, want pending retire-state", retirement)
	}
}

func testEvaluationDecryptConditions(t *testing.T) {
	scenarios := []decryptCase{
		{"unchanged raw storage never decrypts", true, false, true, reconcile.ActionNoOp},
		{"changed raw storage requires decryption", true, true, true, reconcile.ActionNeedsDecision},
		{"unbaselined regular target requires decryption", false, false, true, reconcile.ActionNeedsDecision},
		{"absent target skips decryption entirely", false, false, false, reconcile.ActionCreateTarget},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) { assertDecryptCase(t, scenario) })
	}
	t.Run("decrypted semantics classify correctly", testDecryptClassifies)
}

// decryptCase is one decryption-condition scenario: the row baseline
// shape, the target presence, and the expected classification action.
type decryptCase struct {
	name       string
	baseline   bool
	rawChanged bool
	target     bool
	action     reconcile.Action
}

// secretRepository adds one secret source file to a fixture repository.
func secretRepository(t *testing.T, fx *fixture) {
	t.Helper()
	useRepository(t, fx, []string{"g1"})
	writeFile(t, filepath.Join(fx.root, "g1", "_secrets", "token"), string(secretFixtureJSON()))
}

// assertDecryptCase evaluates one decryption-condition scenario: the hash
// key is recovered exactly when any fingerprint is needed.
func assertDecryptCase(t *testing.T, scenario decryptCase) {
	t.Helper()
	fx := newFixture(t)
	secretRepository(t, fx)
	if scenario.target {
		writePrivateFile(t, filepath.Join(fx.home, "token"), "plain\n")
	}
	if scenario.baseline {
		source := []byte("older ciphertext\n")
		if !scenario.rawChanged {
			source = secretFixtureJSON()
		}
		fx.rows.rows = stateRows{files: []state.FileBaseline{
			fileRow(baselineInput{target: "token", group: "g1", source: source, content: []byte("plain\n")}, &fx.rows.key),
		}}
	}
	evaluation, err := fx.service.Evaluate(context.Background(), Request{})
	if err != nil {
		assertDecryptFailure(t, scenario, err)
		return
	}
	assertDecryptKeyCalls(t, fx, scenario.target)
	if evaluation.records[2].file.Action != scenario.action {
		t.Fatalf("classification = %+v, want action %d", evaluation.records[2].file, scenario.action)
	}
}

// assertDecryptFailure requires an operational failure exactly when the
// scenario demands decryption.
func assertDecryptFailure(t *testing.T, scenario decryptCase, err error) {
	t.Helper()
	if !scenario.rawChanged && !scenario.target {
		t.Fatalf("Evaluate: %v", err)
	}
	assertKind(t, err, failure.Operational)
}

// assertDecryptKeyCalls requires the hash key to be recovered exactly
// when any secret fingerprint is needed.
func assertDecryptKeyCalls(t *testing.T, fx *fixture, target bool) {
	t.Helper()
	want := 0
	if target {
		want = 1
	}
	if fx.rows.keyCalls != want {
		t.Fatalf("key recoveries = %d, want %d", fx.rows.keyCalls, want)
	}
}

func testDecryptClassifies(t *testing.T) {
	fx := newFixture(t)
	secretRepository(t, fx)
	ciphertext := secretFixtureJSON()
	writePrivateFile(t, filepath.Join(fx.home, "token"), string(ciphertext))
	fx.service = NewService(Dependencies{
		RepositorySource: fx.source,
		Compiler:         compileFunc(repository.Compile),
		State:            fx.rows,
		Secrets:          fakeClient(t),
		ProtectedTrees:   []string{filepath.Join(fx.home, "state")},
		Platform:         "linux",
	})
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "token", group: "g1", source: []byte("older ciphertext\n"), content: ciphertext}, &fx.rows.key),
	}}
	evaluation := mustEvaluate(t, fx.service, Request{})
	classification := evaluation.records[2].file
	if classification.Action != reconcile.ActionNoOp || classification.Convergence != reconcile.ConvergenceConverged {
		t.Fatalf("classification = %+v, want converged no-op", classification)
	}
}

func testEvaluationDeterminism(t *testing.T) {
	fx := newFixture(t)
	useRepository(t, fx, []string{"g1"})
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
	}}
	assertSameEvaluation(t, mustEvaluate(t, fx.service, Request{}), mustEvaluate(t, fx.service, Request{}))
}

func testEvaluationFailures(t *testing.T) {
	t.Run("repository resolution fails", testFailureResolution)
	t.Run("invalid platform is rejected", testFailurePlatform)
	t.Run("invalid unselected scope fails compilation", testFailureCompilation)
	t.Run("state row read fails", testFailureStateRead)
	t.Run("symlink target parent is rejected", testFailureTargetCapture)
}

// evaluateFailure evaluates an empty request after the fixture mutation and
// requires the failure kind.
func evaluateFailure(t *testing.T, kind failure.Kind, mutate func(*testing.T, *fixture)) {
	t.Helper()
	fx := newFixture(t)
	mutate(t, fx)
	_, err := fx.service.Evaluate(context.Background(), Request{})
	assertKind(t, err, kind)
}

func testFailureResolution(t *testing.T) {
	evaluateFailure(t, failure.InvalidInput, func(t *testing.T, fx *fixture) { fx.source.fail = errors.New("no default repository") })
}

func testFailurePlatform(t *testing.T) {
	evaluateFailure(t, failure.InvalidInput, func(t *testing.T, fx *fixture) {
		fx.service = NewService(Dependencies{Platform: ""})
	})
}

func testFailureCompilation(t *testing.T) {
	evaluateFailure(t, failure.InvalidInput, func(t *testing.T, fx *fixture) {
		useRepository(t, fx, []string{"g1"})
		writeFile(t, filepath.Join(fx.root, "g1", "_routes.toml"), "not [valid toml")
	})
}

func testFailureStateRead(t *testing.T) {
	evaluateFailure(t, failure.Operational, func(t *testing.T, fx *fixture) { fx.rows.fail = errors.New("database closed") })
}

func testFailureTargetCapture(t *testing.T) {
	evaluateFailure(t, failure.Operational, func(t *testing.T, fx *fixture) {
		useRepository(t, fx, []string{"g1"})
		writeFile(t, filepath.Join(fx.root, "g1", "sub", "x.conf"), "x\n")
		if err := os.Symlink(t.TempDir(), filepath.Join(fx.home, "sub")); err != nil {
			t.Fatal(err)
		}
	})
}

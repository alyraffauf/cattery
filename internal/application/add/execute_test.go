package add

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/state"
	testdb "github.com/alyraffauf/cattery/internal/testfixture/database"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestAddBatchExecution(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary item completed with baseline", testExecuteOrdinaryCompleted},
		{"secret item completed with keyed baseline", testExecuteSecretCompleted},
		{"multiple items complete in order", testExecuteMultipleItems},
		{"write failure continues batch", testExecuteWriteFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testExecuteOrdinaryCompleted(t *testing.T) {
	stage := newExecutionStage(t, ordinaryPayload{relative: ".bashrc", content: []byte("shell"), secret: false})
	result, err := stage.service.execute(context.Background(), stage.identity, stage.plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Completed != 1 {
		t.Fatalf("completed = %d, want 1", result.Summary.Completed)
	}
	row := fileRow(t, stage, ".bashrc")
	if row.BaselineContentHash != deployment.Ordinary([]byte("shell")) {
		t.Fatal("ordinary content baseline was not established")
	}
	if row.BaselineSourceHash != deployment.RawStorage([]byte("shell")) {
		t.Fatal("ordinary source baseline was not the written bytes")
	}
}

func testExecuteSecretCompleted(t *testing.T) {
	plaintext := []byte(`{"secret":"token"}`)
	stage := newExecutionStage(t, ordinaryPayload{relative: ".aws/creds", content: plaintext, secret: true})
	result, err := stage.service.execute(context.Background(), stage.identity, stage.plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Completed != 1 {
		t.Fatalf("completed = %d, want 1", result.Summary.Completed)
	}
	row := fileRow(t, stage, ".aws/creds")
	key, err := stage.fixture.Store.RecoverHashKey()
	if err != nil {
		t.Fatal(err)
	}
	if row.BaselineContentHash != deployment.SecretSemantic(plaintext, key) {
		t.Fatal("keyed secret content baseline was not established")
	}
}

func testExecuteMultipleItems(t *testing.T) {
	stage := newExecutionStage(t,
		ordinaryPayload{relative: "zeta", content: []byte("z"), secret: false},
		ordinaryPayload{relative: "alpha", content: []byte("a"), secret: false},
	)
	result, err := stage.service.execute(context.Background(), stage.identity, stage.plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Completed != 2 {
		t.Fatalf("completed = %d, want 2", result.Summary.Completed)
	}
	if result.Items[0].Target != "alpha" || result.Items[1].Target != "zeta" {
		t.Fatalf("records = %q %q, want sorted", result.Items[0].Target, result.Items[1].Target)
	}
}

func testExecuteWriteFailure(t *testing.T) {
	stage := newExecutionStage(t, ordinaryPayload{relative: "absent", content: []byte("x"), secret: false})
	if err := os.Remove(filepath.Join(stage.identity.Home, "absent")); err != nil {
		t.Fatal(err)
	}
	result, err := stage.service.execute(context.Background(), stage.identity, stage.plan)
	if err == nil {
		t.Fatal("execute accepted an unwritable target")
	}
	if result.Summary.Completed != 0 {
		t.Fatalf("completed = %d, want 0 after failure", result.Summary.Completed)
	}
}

// ordinaryPayload describes one target materialized beneath home.
type ordinaryPayload struct {
	relative string
	content  []byte
	secret   bool
}

// executionStage bundles the service, identity, plan, and state fixture.
type executionStage struct {
	service  *Service
	identity RepositoryIdentity
	plan     BatchPlan
	fixture  *testdb.Fixture
	repo     string
}

func newExecutionStage(t *testing.T, payloads ...ordinaryPayload) executionStage {
	t.Helper()
	fixture := testdb.New(t)
	repo := t.TempDir()
	builder := testfs.New(fixture.Home)
	var items []ItemPlanInput
	for _, payload := range payloads {
		builder = builder.File(payload.relative, payload.content, 0o600)
		items = append(items, payloadItem(repo, fixture.Home, payload))
	}
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(items)
	if err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{
		Writer:    filesystem.NewReplacer(),
		Baselines: fixture.Store,
		HashKey:   fixture.Store,
	}
	if anySecret(payloads) {
		deps.Secrets = roundTripClient(t, repo)
	}
	return executionStage{
		service: NewService(deps), identity: RepositoryIdentity{Root: repo, Home: fixture.Home},
		plan: plan, fixture: fixture, repo: repo,
	}
}

func payloadItem(repo, home string, payload ordinaryPayload) ItemPlanInput {
	source := payload.relative
	if payload.secret {
		source = "_secrets/" + payload.relative
	}
	kind := deployment.FileOrdinary
	if payload.secret {
		kind = deployment.FileSecret
	}
	return ItemPlanInput{
		Layer: deployment.LayerBase, Kind: kind,
		TargetAbsolutePath:   filepath.Join(home, payload.relative),
		TargetRelativePath:   payload.relative,
		SourceRepositoryPath: source,
		SourceAbsolutePath:   filepath.Join(repo, source),
	}
}

func anySecret(payloads []ordinaryPayload) bool {
	for _, payload := range payloads {
		if payload.secret {
			return true
		}
	}
	return false
}

func roundTripClient(t *testing.T, repo string) *secrets.Client {
	t.Helper()
	executable := sops.Build(t)
	behavior := sops.Behavior{Stdout: []byte(`{"secret":"token"}`)}
	cmd, err := executable.Command(behavior)
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewClient(executable.Path, repo, cmd.Env)
}

func fileRow(t *testing.T, stage executionStage, target string) state.FileBaseline {
	t.Helper()
	rows, err := stage.fixture.Store.FileBaselines(stage.repo, stage.identity.Home)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.TargetPath == target {
			return row
		}
	}
	t.Fatalf("no baseline row for target %q", target)
	return state.FileBaseline{}
}

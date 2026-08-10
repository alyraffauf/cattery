package apply

import (
	"context"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestApplyDependencyPreflight(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary only", testPreflightOrdinary},
		{"unchanged secret", testPreflightUnchangedSecret},
		{"changed secret", testPreflightChangedSecret},
		{"missing executable", testPreflightMissingExecutable},
		{"launched failure", testPreflightLaunchedFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// probeFake counts probe calls and returns a fixed error.
type probeFake struct {
	calls int
	err   error
}

func (probe *probeFake) Probe(ctx context.Context) error {
	probe.calls++
	return probe.err
}

// preflightFixture evaluates one plan over the given rows with a counting
// probe and returns the service, the candidates, the probe, and the home.
func preflightFixture(t *testing.T, input evalInput) (*Service, Candidates) {
	t.Helper()
	service := evalFixture(t, input)
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return service, candidates
}

// secretRow freezes one active secret baseline over the given ciphertext.
func secretRow(target string) state.FileBaseline {
	cipher := []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)
	return state.FileBaseline{
		TargetPath: target, SourcePath: "files/token", SourceKind: deployment.FileSecret, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.SecretSemantic([]byte("secret"), [32]byte{7}),
		BaselineSourceHash:  deployment.RawStorage(cipher),
		Status:              state.StatusActive,
	}
}

func testPreflightOrdinary(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("a"))
	service, candidates := preflightFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), probe: &probeFake{}, client: sopsClient(t, []byte("plaintext"))})
	if err := service.Preflight(context.Background(), candidates); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	probe := service.probe.(*probeFake)
	if probe.calls != 0 {
		t.Fatalf("ordinary-only preflight probed %d times, want 0", probe.calls)
	}
}

func testPreflightUnchangedSecret(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	service, candidates := preflightFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), rows: stateRows{files: []state.FileBaseline{secretRow("target")}}, probe: &probeFake{}})
	if err := service.Preflight(context.Background(), candidates); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	probe := service.probe.(*probeFake)
	if probe.calls != 0 {
		t.Fatalf("unchanged secret preflight probed %d times, want 0", probe.calls)
	}
}

func testPreflightChangedSecret(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	service, candidates := preflightFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), probe: &probeFake{}, client: sopsClient(t, []byte("plaintext"))})
	if err := service.Preflight(context.Background(), candidates); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	probe := service.probe.(*probeFake)
	if probe.calls != 1 {
		t.Fatalf("changed secret preflight probed %d times, want 1", probe.calls)
	}
}

// sopsClient builds a fake sops executable that decrypts to the given
// plaintext and returns a client over it.
func sopsClient(t *testing.T, plaintext []byte) *secrets.Client {
	t.Helper()
	executable := sops.Build(t)
	cmd, err := executable.Command(sops.Behavior{Stdout: plaintext})
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewClient(executable.Path, t.TempDir(), cmd.Env)
}

func testPreflightMissingExecutable(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	probe := &probeFake{err: failure.New(failure.Dependency, "apply: sops executable missing", nil)}
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	service, candidates := preflightFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), probe: probe, client: sopsClient(t, []byte("plaintext"))})
	err := service.Preflight(context.Background(), candidates)
	if err == nil || !kindIs(err, failure.Dependency) {
		t.Fatalf("missing executable error = %v, want a dependency failure", err)
	}
	if probe.calls != 1 {
		t.Fatalf("missing executable preflight probed %d times, want 1", probe.calls)
	}
}

func testPreflightLaunchedFailure(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	probe := &probeFake{err: failure.New(failure.Operational, "apply: sops launch failed", nil)}
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	service, candidates := preflightFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), probe: probe, client: sopsClient(t, []byte("plaintext"))})
	err := service.Preflight(context.Background(), candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("launched failure error = %v, want an operational failure", err)
	}
}

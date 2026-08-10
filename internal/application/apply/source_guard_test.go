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

func TestApplySourceGuard(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary storage", testGuardOrdinary},
		{"secret storage", testGuardSecret},
		{"source mode", testGuardSourceMode},
		{"target identity", testGuardTargetIdentity},
		{"parent identity", testGuardParent},
		{"hook edit", testGuardHookEdit},
		{"target race", testGuardTargetRace},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// guardFixture evaluates one ordinary file pair and returns the service,
// the candidates, and the paths.
func guardFixture(t *testing.T) (*Service, Candidates, string, string) {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	ordinarySource(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}, []byte("source"))
	writeTarget(t, targetPath(home, "a.conf"), []byte("target"))
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, planFile(t, fileSpec{Repo: repo, Target: "a.conf", Relative: "files/a"}))})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return service, candidates, repo, home
}

// planFile freezes one ordinary plan entry over the spec.
func planFile(t *testing.T, spec fileSpec) deployment.ManagedFile {
	t.Helper()
	path := filepath.Join(spec.Repo, filepath.FromSlash(spec.Relative))
	file, err := deployment.NewManagedFile(deployment.ManagedFile{
		Scope: deployment.NewScope(""), Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		SourceAbsolutePath: path, SourceRepositoryPath: spec.Relative, TargetRelativePath: spec.Target,
	})
	if err != nil {
		t.Fatalf("managed file: %v", err)
	}
	return file
}

// requireGuardStable requires revalidation to pass.
func requireGuardStable(t *testing.T, service *Service, candidates Candidates) {
	t.Helper()
	if err := service.Revalidate(context.Background(), candidates); err != nil {
		t.Fatalf("revalidate: %v", err)
	}
}

// requireGuardMismatch requires revalidation to fail operationally.
func requireGuardMismatch(t *testing.T, service *Service, candidates Candidates) {
	t.Helper()
	err := service.Revalidate(context.Background(), candidates)
	if err == nil || !kindIs(err, failure.Operational) {
		t.Fatalf("revalidate error = %v, want an operational failure", err)
	}
}

func testGuardOrdinary(t *testing.T) {
	service, candidates, _, home := guardFixture(t)
	requireGuardStable(t, service, candidates)
	if err := os.WriteFile(targetPath(home, "a.conf"), []byte("mutated"), 0o600); err != nil {
		t.Fatal(err)
	}
	requireGuardMismatch(t, service, candidates)
}

func testGuardSecret(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	rows := stateRows{files: []state.FileBaseline{{
		TargetPath: "target", SourcePath: "files/token", SourceKind: deployment.FileSecret, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.SecretSemantic([]byte("secret"), [32]byte{7}),
		BaselineSourceHash:  deployment.RawStorage([]byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)),
		Status:              state.StatusActive,
	}}}
	writeTarget(t, targetPath(home, "target"), []byte("secret"))
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), rows: rows})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	requireGuardStable(t, service, candidates)
	envelope := []byte(`{"data":"Y2hhbmdlZA==","sops":{"version":"3.9.0"}}`)
	if err := os.WriteFile(file.SourceAbsolutePath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	requireGuardMismatch(t, service, candidates)
}

func testGuardSourceMode(t *testing.T) {
	service, candidates, repo, _ := guardFixture(t)
	if err := os.Chmod(filepath.Join(repo, "files", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	requireGuardMismatch(t, service, candidates)
}

func testGuardTargetIdentity(t *testing.T) {
	service, candidates, _, home := guardFixture(t)
	path := targetPath(home, "a.conf")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("files/a", path); err != nil {
		t.Fatal(err)
	}
	requireGuardMismatch(t, service, candidates)
}

func testGuardParent(t *testing.T) {
	service, candidates, _, home := guardFixture(t)
	parent := filepath.Dir(targetPath(home, "a.conf"))
	backup := parent + "-backup"
	if err := os.Rename(parent, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTarget(t, targetPath(home, "a.conf"), []byte("target"))
	requireGuardMismatch(t, service, candidates)
	_ = backup
}

func testGuardHookEdit(t *testing.T) {
	service, candidates, repo, _ := guardFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "files", "a"), []byte("hook edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	requireGuardMismatch(t, service, candidates)
}

func testGuardTargetRace(t *testing.T) {
	service, candidates, _, home := guardFixture(t)
	if err := os.Remove(targetPath(home, "a.conf")); err != nil {
		t.Fatal(err)
	}
	requireGuardMismatch(t, service, candidates)
}

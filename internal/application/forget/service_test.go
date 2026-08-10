package forget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	backend "github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
	testdb "github.com/alyraffauf/cattery/internal/testfixture/database"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func TestForgetRemovesSourcesAndRetiresStateWithoutDeletingTargets(t *testing.T) {
	stage := newStage(t, deployment.Plan{})
	stage.writeRepository(t, "nvim/.config/nvim/init.lua", []byte("base"))
	stage.writeRepository(t, "nvim/_linux/.config/nvim/init.lua", []byte("linux"))
	stage.writeHome(t, ".config/nvim/init.lua", []byte("user file"))
	stage.activate(t, ".config/nvim/init.lua")

	result, err := stage.service.Forget(context.Background(), stage.request(".config/nvim", false, true))
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want both base and platform sources", result.Items)
	}
	stage.requireMissing(t, "nvim/.config/nvim/init.lua")
	stage.requireMissing(t, "nvim/_linux/.config/nvim/init.lua")
	stage.requireHome(t, ".config/nvim/init.lua", "user file")
	stage.requireRetired(t, ".config/nvim/init.lua")
}

func TestForgetDryRunAndConfirmationDoNotMutate(t *testing.T) {
	stage := newStage(t, deployment.Plan{})
	stage.writeRepository(t, "shell/.config/shell/env", []byte("source"))
	stage.writeHome(t, ".config/shell/env", []byte("target"))

	result, err := stage.service.Forget(context.Background(), stage.request(".config/shell", true, false))
	if err != nil || len(result.Items) != 1 || result.Items[0].Status != "planned" {
		t.Fatalf("dry run = %#v, %v", result, err)
	}
	stage.requireSource(t, "shell/.config/shell/env")

	_, err = stage.service.Forget(context.Background(), stage.request(".config/shell", false, false))
	if err == nil {
		t.Fatal("forget without --yes succeeded")
	}
	stage.requireSource(t, "shell/.config/shell/env")
}

func TestForgetRejectsAliasedDirectories(t *testing.T) {
	alias, err := deployment.NewAlias(deployment.Alias{Platform: "linux", AliasRelativePath: ".config/nvim/init.lua", CanonicalTargetRelativePath: ".config/editor/init.lua"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := deployment.NewPlan(deployment.PlanInput{RepositoryRoot: "repo", Platform: "linux", Aliases: []deployment.Alias{alias}})
	if err != nil {
		t.Fatal(err)
	}
	stage := newStage(t, plan)
	stage.writeRepository(t, "editor/.config/editor/init.lua", []byte("source"))

	_, err = stage.service.Forget(context.Background(), stage.request(".config/editor", false, true))
	if err == nil {
		t.Fatal("forget accepted an aliased directory")
	}
	stage.requireSource(t, "editor/.config/editor/init.lua")
}

type stage struct {
	service *Service
	repo    string
	home    string
	store   *state.Store
}

func newStage(t *testing.T, plan deployment.Plan) stage {
	t.Helper()
	fixture := testdb.New(t)
	repoRoot := t.TempDir()
	compiler := compiler{plan: plan, repo: repoRoot}
	service := NewService(Dependencies{
		RepositorySource: source{identity: repository.RepositoryIdentity{Root: repoRoot, Home: fixture.Home}},
		Compiler:         compiler,
		State:            fixture.Store,
		Retirements:      fixture.Store,
		Remover:          filesystem.NewReplacer(),
	})
	return stage{service: service, repo: repoRoot, home: fixture.Home, store: fixture.Store}
}

func (stage stage) request(directory string, dryRun, yes bool) Request {
	return Request{Repository: RepositoryInput{WorkingDir: stage.home}, Directory: directory, DryRun: dryRun, Yes: yes}
}

func (stage stage) writeRepository(t *testing.T, relative string, content []byte) {
	t.Helper()
	if err := testfs.New(stage.repo).File(relative, content, 0o600).Materialize(); err != nil {
		t.Fatal(err)
	}
}

func (stage stage) writeHome(t *testing.T, relative string, content []byte) {
	t.Helper()
	if err := testfs.New(stage.home).File(relative, content, 0o600).Materialize(); err != nil {
		t.Fatal(err)
	}
}

func (stage stage) activate(t *testing.T, target string) {
	t.Helper()
	baseline := state.FileBaseline{TargetPath: target, SourcePath: "source", SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.Ordinary([]byte("content")), BaselineSourceHash: deployment.Ordinary([]byte("source"))}
	if _, err := stage.store.UpsertFileBaseline(stage.repo, stage.home, baseline); err != nil {
		t.Fatal(err)
	}
}

func (stage stage) requireMissing(t *testing.T, relative string) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(stage.repo, relative))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source %s still exists: %v", relative, err)
	}
}

func (stage stage) requireSource(t *testing.T, relative string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(stage.repo, relative)); err != nil {
		t.Fatalf("source %s: %v", relative, err)
	}
}

func (stage stage) requireHome(t *testing.T, relative, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(stage.home, relative))
	if err != nil || string(content) != want {
		t.Fatalf("target %s = %q, %v; want %q", relative, content, err, want)
	}
}

func (stage stage) requireRetired(t *testing.T, target string) {
	t.Helper()
	baselines, err := stage.store.FileBaselines(stage.repo, stage.home)
	if err != nil || len(baselines) != 1 || baselines[0].Status != state.StatusRetired {
		t.Fatalf("baselines = %#v, %v; want one retired row", baselines, err)
	}
}

type source struct{ identity repository.RepositoryIdentity }

func (source source) Resolve(selection.RepositoryRequest) (repository.RepositoryIdentity, error) {
	return source.identity, nil
}

type compiler struct {
	plan deployment.Plan
	repo string
}

func (compiler compiler) Compile(input backend.CompileInput) (deployment.Plan, error) {
	if len(compiler.plan.Aliases()) == 0 {
		return deployment.NewPlan(deployment.PlanInput{RepositoryRoot: compiler.repo, Platform: string(input.Platform)})
	}
	return compiler.plan, nil
}

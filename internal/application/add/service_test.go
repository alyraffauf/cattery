package add

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
	testdb "github.com/alyraffauf/cattery/internal/testfixture/database"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func TestAddService(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"dry run plans without writing", testServiceDryRun},
		{"ordinary add completes", testServiceOrdinaryAdd},
		{"resolve error propagates", testServiceResolveError},
		{"compile error propagates", testServiceCompileError},
		{"target outside home rejected", testServiceTargetOutsideHome},
		{"cross-platform rejected", testServiceCrossPlatform},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testServiceDryRun(t *testing.T) {
	stage := newServiceStage(t)
	stage.withTarget(t, ".bashrc", []byte("shell"))
	request := stage.request()
	request.DryRun = true
	result, err := stage.service.Add(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Planned != 1 {
		t.Fatalf("planned = %d, want 1", result.Summary.Planned)
	}
	if _, err := os.Stat(filepath.Join(stage.repo, ".bashrc")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a source")
	}
}

func testServiceOrdinaryAdd(t *testing.T) {
	stage := newServiceStage(t)
	stage.withTarget(t, ".bashrc", []byte("shell"))
	result, err := stage.service.Add(context.Background(), stage.request())
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Completed != 1 {
		t.Fatalf("completed = %d, want 1", result.Summary.Completed)
	}
	if _, err := os.ReadFile(filepath.Join(stage.repo, ".bashrc")); err != nil {
		t.Fatal("ordinary source was not written")
	}
}

func testServiceResolveError(t *testing.T) {
	stage := newServiceStage(t)
	stage.source.err = errInjected
	if _, err := stage.service.Add(context.Background(), stage.request()); err == nil {
		t.Fatal("resolve error was swallowed")
	}
}

func testServiceCompileError(t *testing.T) {
	stage := newServiceStage(t)
	stage.compiler.err = errInjected
	if _, err := stage.service.Add(context.Background(), stage.request()); err == nil {
		t.Fatal("compile error was swallowed")
	}
}

func testServiceTargetOutsideHome(t *testing.T) {
	stage := newServiceStage(t)
	request := stage.request()
	request.Targets = []string{"../outside"}
	if _, err := stage.service.Add(context.Background(), request); err == nil {
		t.Fatal("target outside home was accepted")
	}
}

func testServiceCrossPlatform(t *testing.T) {
	stage := newServiceStage(t)
	stage.withTarget(t, ".bashrc", []byte("shell"))
	request := stage.request()
	request.PlatformSet = true
	request.Platform = "darwin"
	if _, err := stage.service.Add(context.Background(), request); err == nil {
		t.Fatal("cross-platform add was accepted")
	}
}

// errInjected is a sentinel cause for the fake failure ports.
var errInjected = newInjectedError()

func newInjectedError() error { return &injectedError{} }

type injectedError struct{}

func (*injectedError) Error() string { return "injected" }

// fakeSource satisfies RepositorySource with a fixed identity or error.
type fakeSource struct {
	identity RepositoryIdentity
	err      error
}

func (source *fakeSource) Resolve(selection.RepositoryRequest) (RepositoryIdentity, error) {
	return source.identity, source.err
}

// fakeCompiler satisfies Compiler with a fixed plan or error.
type fakeCompiler struct {
	plan deployment.Plan
	err  error
}

func (compiler *fakeCompiler) Compile(repository.CompileInput) (deployment.Plan, error) {
	return compiler.plan, compiler.err
}

// serviceStage wires the service against fakes and a real state fixture.
type serviceStage struct {
	service  *Service
	source   *fakeSource
	compiler *fakeCompiler
	fixture  *testdb.Fixture
	repo     string
	home     string
}

func newServiceStage(t *testing.T) serviceStage {
	t.Helper()
	fixture := testdb.New(t)
	repo := t.TempDir()
	source := &fakeSource{identity: RepositoryIdentity{Root: repo, Home: fixture.Home}}
	plan, err := deployment.NewPlan(deployment.PlanInput{RepositoryRoot: repo, Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	compiler := &fakeCompiler{plan: plan}
	deps := Dependencies{
		RepositorySource: source, Compiler: compiler,
		Writer: filesystem.NewReplacer(), Baselines: fixture.Store,
	}
	return serviceStage{
		service: NewService(deps), source: source, compiler: compiler,
		fixture: fixture, repo: repo, home: fixture.Home,
	}
}

// withTarget materializes one regular file beneath home.
func (stage serviceStage) withTarget(t *testing.T, relative string, content []byte) {
	t.Helper()
	if err := testfs.New(stage.home).File(relative, content, 0o600).Materialize(); err != nil {
		t.Fatal(err)
	}
}

// request builds a minimal add request rooted at the stage home.
func (stage serviceStage) request() Request {
	return Request{
		Repository: RepositoryInput{WorkingDir: stage.home},
		Targets:    []string{".bashrc"},
	}
}

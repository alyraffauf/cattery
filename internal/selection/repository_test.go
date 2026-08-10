package selection

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/database"
)

func TestRepositorySelection(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"explicit path wins", testSelectionExplicit},
		{"explicit relative path", testSelectionRelative},
		{"empty explicit blocks fallback", testSelectionEmptyExplicit},
		{"environment path", testSelectionEnvironment},
		{"empty environment blocks fallback", testSelectionEmptyEnvironment},
		{"unset environment falls through", testSelectionUnsetEnvironment},
		{"default for canonical home", testSelectionDefault},
		{"absent default fails", testSelectionAbsentDefault},
		{"two homes keep separate defaults", testSelectionTwoHomes},
		{"canonical results", testSelectionCanonical},
		{"canonical home", testSelectionCanonicalHome},
		{"no implicit registration", testSelectionNoRegistration},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func newFixtureResolver(t *testing.T) (*RepositoryResolver, *database.Fixture) {
	t.Helper()
	fixture := database.New(t)
	return NewRepositoryResolver(fixture.Home, fixture.Store), fixture
}

func resolveSelection(t *testing.T, resolver *RepositoryResolver) state.Repository {
	t.Helper()
	result, err := resolver.Resolve(RepositoryRequest{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve repository: %v", err)
	}
	return result
}

func testSelectionExplicit(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	other := filepath.Join(fixture.Root, "other")
	if _, err := fixture.Store.SetDefaultRepository(other, fixture.Home); err != nil {
		t.Fatalf("register default: %v", err)
	}
	repo := filepath.Join(fixture.Root, "main")
	result, err := resolver.Resolve(RepositoryRequest{RawExplicit: repo, ExplicitSet: true, WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve explicit path: %v", err)
	}
	if result.RootPath != repo || result.HomePath != fixture.Home {
		t.Fatal("explicit selection must return the canonical pair")
	}
}

func testSelectionRelative(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	working := filepath.Join(fixture.Root, "work")
	if err := os.MkdirAll(working, 0o700); err != nil {
		t.Fatalf("make working directory: %v", err)
	}
	result, err := resolver.Resolve(RepositoryRequest{RawExplicit: "repos/main", ExplicitSet: true, WorkingDir: working})
	if err != nil {
		t.Fatalf("resolve relative path: %v", err)
	}
	want := filepath.Join(working, "repos", "main")
	if result.RootPath != want {
		t.Fatalf("relative explicit root = %q, want %q", result.RootPath, want)
	}
}

func testSelectionEmptyExplicit(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	if _, err := fixture.Store.SetDefaultRepository(filepath.Join(fixture.Root, "default"), fixture.Home); err != nil {
		t.Fatalf("register default: %v", err)
	}
	if _, err := resolver.Resolve(RepositoryRequest{RawExplicit: "", ExplicitSet: true, WorkingDir: t.TempDir()}); err == nil {
		t.Fatal("an empty explicit path must block fallback")
	}
}

func testSelectionEnvironment(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	repo := filepath.Join(fixture.Root, "env-repo")
	result, err := resolver.Resolve(RepositoryRequest{RawEnv: repo, EnvSet: true, WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve environment path: %v", err)
	}
	if result.RootPath != repo {
		t.Fatalf("environment root = %q, want %q", result.RootPath, repo)
	}
}

func testSelectionEmptyEnvironment(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	if _, err := fixture.Store.SetDefaultRepository(filepath.Join(fixture.Root, "default"), fixture.Home); err != nil {
		t.Fatalf("register default: %v", err)
	}
	if _, err := resolver.Resolve(RepositoryRequest{RawEnv: "", EnvSet: true, WorkingDir: t.TempDir()}); err == nil {
		t.Fatal("an empty CATTERY_REPO must block fallback")
	}
}

func testSelectionUnsetEnvironment(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	repo := filepath.Join(fixture.Root, "default")
	if _, err := fixture.Store.SetDefaultRepository(repo, fixture.Home); err != nil {
		t.Fatalf("register default: %v", err)
	}
	result, err := resolver.Resolve(RepositoryRequest{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve unset environment: %v", err)
	}
	if result.RootPath != repo {
		t.Fatal("unset environment must fall through to the default")
	}
}

func testSelectionDefault(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	repo := filepath.Join(fixture.Root, "default")
	if _, err := fixture.Store.SetDefaultRepository(repo, fixture.Home); err != nil {
		t.Fatalf("register default: %v", err)
	}
	result, err := resolver.Resolve(RepositoryRequest{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if result.RootPath != repo || !result.IsDefault {
		t.Fatal("default selection must return the stored default row")
	}
}

func testSelectionAbsentDefault(t *testing.T) {
	resolver, _ := newFixtureResolver(t)
	if _, err := resolver.Resolve(RepositoryRequest{WorkingDir: t.TempDir()}); err == nil {
		t.Fatal("no default must fail with instructions")
	}
}

func testSelectionTwoHomes(t *testing.T) {
	fixture := database.New(t)
	homeB := filepath.Join(fixture.Root, "home-b")
	repoA := filepath.Join(fixture.Root, "repo-a")
	repoB := filepath.Join(fixture.Root, "repo-b")
	if _, err := fixture.Store.SetDefaultRepository(repoA, fixture.Home); err != nil {
		t.Fatalf("register home A default: %v", err)
	}
	if _, err := fixture.Store.SetDefaultRepository(repoB, homeB); err != nil {
		t.Fatalf("register home B default: %v", err)
	}
	resultA := resolveSelection(t, NewRepositoryResolver(fixture.Home, fixture.Store))
	resultB := resolveSelection(t, NewRepositoryResolver(homeB, fixture.Store))
	if resultA.RootPath != repoA || resultB.RootPath != repoB {
		t.Fatal("each home must resolve its own default")
	}
}

func testSelectionCanonical(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	real := filepath.Join(fixture.Root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatalf("make real directory: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(fixture.Root, "link")); err != nil {
		t.Fatalf("make symlink: %v", err)
	}
	result, err := resolver.Resolve(RepositoryRequest{RawExplicit: filepath.Join(fixture.Root, "link"), ExplicitSet: true, WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve symlinked path: %v", err)
	}
	if result.RootPath != real {
		t.Fatalf("canonical root = %q, want %q", result.RootPath, real)
	}
}

func testSelectionCanonicalHome(t *testing.T) {
	fixture := database.New(t)
	realHome := filepath.Join(fixture.Root, "real-home")
	if err := os.Mkdir(realHome, 0o700); err != nil {
		t.Fatalf("make real home: %v", err)
	}
	linkedHome := filepath.Join(fixture.Root, "linked-home")
	if err := os.Symlink(realHome, linkedHome); err != nil {
		t.Fatalf("make linked home: %v", err)
	}
	repository := filepath.Join(fixture.Root, "repository")
	resolver := NewRepositoryResolver(linkedHome, fixture.Store)
	result, err := resolver.Resolve(RepositoryRequest{RawExplicit: repository, ExplicitSet: true, WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("resolve linked home: %v", err)
	}
	if result.HomePath != realHome {
		t.Fatalf("home = %q, want %q", result.HomePath, realHome)
	}
}

func testSelectionNoRegistration(t *testing.T) {
	resolver, fixture := newFixtureResolver(t)
	repo := filepath.Join(fixture.Root, "repo")
	if _, err := resolver.Resolve(RepositoryRequest{RawExplicit: repo, ExplicitSet: true, WorkingDir: t.TempDir()}); err != nil {
		t.Fatalf("resolve explicit path: %v", err)
	}
	rows, err := fixture.Store.Repositories()
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}
	if len(rows) != 0 {
		t.Fatal("explicit selection must never register a repository")
	}
}

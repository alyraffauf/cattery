package initialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/database"
)

func TestInitializeService(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"defaults to the working directory", testServiceWorkingDirectoryDefault},
		{"rejects a non-directory entry", testServiceRejectsNonDirectory},
		{"creates and registers a missing directory", testServiceCreatesMissingDirectory},
		{"re-canonicalizes through a symlinked ancestor", testServiceRecheckAfterCreation},
		{"clears previous defaults per home", testServicePromotesSoleDefault},
		{"creates no scaffolding", testServiceNoScaffolding},
		{"rejects home overlap", testServiceRejectsHomeOverlap},
		{"rejects state overlap", testServiceRejectsStateOverlap},
		{"rejects portable case-equivalent overlap", testServiceRejectsPortableOverlap},
		{"acquires the store lazily", testServiceAcquiresStoreLazily},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testServiceWorkingDirectoryDefault(t *testing.T) {
	fixture, service := newService(t)
	working := filepath.Join(fixture.Home, "work")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(working); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previous) }()
	result, err := service.Initialize(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Initialize zero request: %v", err)
	}
	if result.Repository.RootPath != working {
		t.Fatalf("root = %q, want the working directory %q", result.Repository.RootPath, working)
	}
}

func testServiceRejectsNonDirectory(t *testing.T) {
	fixture, service := newService(t)
	target := filepath.Join(fixture.Root, "plain")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.Initialize(context.Background(), Request{Path: target})
	if kind, matched := failure.HasKind(err); !matched || kind != failure.InvalidInput {
		t.Fatalf("error = %v, want InvalidInput", err)
	}
	if rows := registeredRepositories(t, fixture); len(rows) != 0 {
		t.Fatalf("non-directory registered %v", rows)
	}
}

func testServiceCreatesMissingDirectory(t *testing.T) {
	fixture, service := newService(t)
	target := filepath.Join(fixture.Home, "repos", "dotfiles")
	result, err := service.Initialize(context.Background(), Request{Path: target})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if result.Repository.RootPath != target {
		t.Fatalf("root = %q, want %q", result.Repository.RootPath, target)
	}
	if !result.Repository.IsDefault {
		t.Fatal("first repository is not the default")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("repository directory missing: %v", err)
	}
}

func testServiceRecheckAfterCreation(t *testing.T) {
	fixture, service := newService(t)
	real := filepath.Join(fixture.Root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.Root, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	result, err := service.Initialize(context.Background(), Request{Path: filepath.Join(link, "dotfiles")})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	want := filepath.Join(real, "dotfiles")
	if result.Repository.RootPath != want {
		t.Fatalf("root = %q, want canonical %q", result.Repository.RootPath, want)
	}
	if info, err := os.Lstat(want); err != nil || !info.IsDir() {
		t.Fatalf("canonical directory missing: %v", err)
	}
}

func testServicePromotesSoleDefault(t *testing.T) {
	fixture := database.New(t)
	first := registerPath(t, fixture, filepath.Join(fixture.Home, "first"))
	second := registerPath(t, fixture, filepath.Join(fixture.Home, "second"))
	current, err := fixture.Store.LookupRepository(first.RootPath, fixture.Home)
	if err != nil {
		t.Fatalf("LookupRepository: %v", err)
	}
	if current.IsDefault {
		t.Fatal("first repository remained default")
	}
	if !second.IsDefault {
		t.Fatal("second repository is not the default")
	}
	defaulted, err := fixture.Store.DefaultRepository(fixture.Home)
	if err != nil {
		t.Fatalf("DefaultRepository: %v", err)
	}
	if defaulted.RootPath != second.RootPath {
		t.Fatalf("default = %q, want %q", defaulted.RootPath, second.RootPath)
	}
}

func testServiceNoScaffolding(t *testing.T) {
	fixture, service := newService(t)
	target := filepath.Join(fixture.Home, "dotfiles")
	if _, err := service.Initialize(context.Background(), Request{Path: target}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("scaffolding created: %v", entries)
	}
}

func testServiceRejectsHomeOverlap(t *testing.T) {
	fixture, service := newService(t)
	for _, target := range []string{fixture.Home, fixture.Root} {
		_, err := service.Initialize(context.Background(), Request{Path: target})
		if kind, matched := failure.HasKind(err); !matched || kind != failure.InvalidInput {
			t.Fatalf("Initialize(%q) error = %v, want InvalidInput", target, err)
		}
	}
	if rows := registeredRepositories(t, fixture); len(rows) != 0 {
		t.Fatalf("overlapping root registered %v", rows)
	}
}

func testServiceRejectsStateOverlap(t *testing.T) {
	fixture, service := newService(t)
	_, err := service.Initialize(context.Background(), Request{Path: fixture.Directory()})
	if kind, matched := failure.HasKind(err); !matched || kind != failure.InvalidInput {
		t.Fatalf("error = %v, want InvalidInput", err)
	}
	if rows := registeredRepositories(t, fixture); len(rows) != 0 {
		t.Fatalf("state-overlapping root registered %v", rows)
	}
}

func testServiceRejectsPortableOverlap(t *testing.T) {
	fixture, service := newService(t)
	upper := filepath.Join(filepath.Dir(fixture.Home), strings.ToUpper(filepath.Base(fixture.Home)))
	_, err := service.Initialize(context.Background(), Request{Path: upper})
	if kind, matched := failure.HasKind(err); !matched || kind != failure.InvalidInput {
		t.Fatalf("error = %v, want InvalidInput", err)
	}
	if _, err := os.Stat(upper); !os.IsNotExist(err) {
		t.Fatal("rejected case-variant root was created")
	}
}

func testServiceAcquiresStoreLazily(t *testing.T) {
	store := state.NewStore(state.Dependencies{StateHome: t.TempDir()})
	defer store.Close()
	service := NewService(Dependencies{Home: t.TempDir(), Store: store})
	if _, err := service.Initialize(context.Background(), Request{Path: t.TempDir()}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
}

func newService(t *testing.T) (*database.Fixture, *Service) {
	t.Helper()
	fixture := database.New(t)
	service := NewService(Dependencies{Home: fixture.Home, Store: fixture.Store})
	return fixture, service
}

func registerPath(t *testing.T, fixture *database.Fixture, path string) RegisteredRepository {
	t.Helper()
	service := NewService(Dependencies{Home: fixture.Home, Store: fixture.Store})
	result, err := service.Initialize(context.Background(), Request{Path: path})
	if err != nil {
		t.Fatalf("Initialize(%q): %v", path, err)
	}
	return result.Repository
}

func registeredRepositories(t *testing.T, fixture *database.Fixture) []state.Repository {
	t.Helper()
	rows, err := fixture.Store.Repositories()
	if err != nil {
		t.Fatalf("Repositories: %v", err)
	}
	return rows
}

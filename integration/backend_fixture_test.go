// Package integration proves the frozen Cattery contracts through real
// backends: isolated repositories, HOME trees, state stores, application
// services, and the built executable (PLAN.md Section 15). No production
// file is patched here.
package integration

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/bootstrap"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/state"
)

// BackendFixture bundles one isolated backend environment: a real
// repository tree, a real HOME, a state store, and the application
// services over a deterministic clock.
type BackendFixture struct {
	Repository   string
	Home         string
	StateHome    string
	Store        *state.Store
	Applications bootstrap.Applications
	Platform     deployment.Layer
}

// NewBackendFixture builds one isolated environment with real directories
// and a deterministic clock. Nothing is registered and no repository or
// managed row exists until a test explicitly registers one.
func NewBackendFixture(t *testing.T) BackendFixture {
	t.Helper()
	repository := t.TempDir()
	home := t.TempDir()
	stateHome := filepath.Join(t.TempDir(), "state")
	adapters := bootstrap.NewAdapters(stateHome, func() time.Time { return fixedClock() })
	applications := bootstrap.BuildApplications(bootstrap.ApplicationsInput{
		Adapters:   adapters,
		Home:       home,
		Platform:   currentPlatform(),
		Protected:  []string{stateHome},
		Stdin:      strings.NewReader(""),
		Stderr:     io.Discard,
		IsTerminal: func(fd int) bool { return false },
	})
	return BackendFixture{
		Repository:   repository,
		Home:         home,
		StateHome:    stateHome,
		Store:        adapters.Store,
		Applications: applications,
		Platform:     currentPlatform(),
	}
}

// Acquire opens the state store and closes it at cleanup.
func (fixture BackendFixture) Acquire(t *testing.T) {
	t.Helper()
	if err := fixture.Store.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire store: %v", err)
	}
	t.Cleanup(func() {
		if err := fixture.Store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
}

// RegisterRepository registers the fixture repository as the home default.
func (fixture BackendFixture) RegisterRepository(t *testing.T) {
	t.Helper()
	if _, err := fixture.Store.SetDefaultRepository(fixture.Repository, fixture.Home); err != nil {
		t.Fatalf("register repository: %v", err)
	}
}

// RepositoryPath joins one repository-relative source path.
func (fixture BackendFixture) RepositoryPath(relative string) string {
	return filepath.Join(fixture.Repository, filepath.FromSlash(relative))
}

// TargetPath joins one HOME-relative target path.
func (fixture BackendFixture) TargetPath(relative string) string {
	return filepath.Join(fixture.Home, filepath.FromSlash(relative))
}

// WriteRepository writes one source file into the repository.
func (fixture BackendFixture) WriteRepository(t *testing.T, relative string, content []byte) {
	t.Helper()
	writeFile(t, fixture.RepositoryPath(relative), content)
}

// WriteTarget writes one file into the HOME tree.
func (fixture BackendFixture) WriteTarget(t *testing.T, relative string, content []byte) {
	t.Helper()
	writeFile(t, fixture.TargetPath(relative), content)
}

// writeFile writes one 0600 file after creating its parent directories.
func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixedClock returns one deterministic instant.
func fixedClock() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// currentPlatform derives the deployment layer from the runtime GOOS.
func currentPlatform() deployment.Layer {
	platform, err := deployment.ParseLayer(runtime.GOOS)
	if err != nil {
		return ""
	}
	return platform
}

func TestBackendFixture(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"isolation", testFixtureIsolation},
		{"deterministic clock", testFixtureClock},
		{"no hidden registration", testFixtureNoRegistration},
		{"cleanup", testFixtureCleanup},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testFixtureIsolation(t *testing.T) {
	first := NewBackendFixture(t)
	second := NewBackendFixture(t)
	if first.Repository == second.Repository || first.Home == second.Home || first.StateHome == second.StateHome {
		t.Fatal("two fixtures must share no path")
	}
	if first.Store == second.Store || first.Applications.Apply == second.Applications.Apply {
		t.Fatal("two fixtures must share no store or service")
	}
}

func testFixtureClock(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	repository, err := fixture.Store.DefaultRepository(fixture.Home)
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !repository.CreatedAt.Equal(want) {
		t.Fatalf("created = %v, want the fixed instant", repository.CreatedAt)
	}
}

func testFixtureNoRegistration(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	if _, err := fixture.Store.DefaultRepository(fixture.Home); err == nil {
		t.Fatal("a fresh fixture must register no repository")
	}
}

func testFixtureCleanup(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.WriteRepository(t, "files/a", []byte("a"))
	fixture.WriteTarget(t, "a", []byte("a"))
	if _, err := os.Stat(fixture.RepositoryPath("files/a")); err != nil {
		t.Fatalf("fixture files must exist during the test: %v", err)
	}
}

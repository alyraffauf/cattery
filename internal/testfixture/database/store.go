// Package database provides a test-only fixture that materializes an isolated
// Cattery state tree: a fresh HOME, a private state home, and an opened,
// migrated state store with a deterministic clock. Production code must not
// import this package.
package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/state"
)

// Directory and file names mirror PLAN.md Section 8.1. The fixture duplicates
// them so tests can reach the concrete state paths without widening state's
// public surface.
const (
	catteryDirectory = "cattery"
)

// fixtureOrigin returns the fixed instant every fixture clock starts at, so
// timestamps across fixtures are deterministic.
func fixtureOrigin() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// Clock is a deterministic clock pinned to a fixed instant. Advance moves the
// instant so tests can produce stable, ordered timestamps.
type Clock struct {
	now time.Time
}

// NewClock returns a Clock pinned at at.
func NewClock(at time.Time) *Clock {
	return &Clock{now: at}
}

// Now returns the pinned instant.
func (clock *Clock) Now() time.Time {
	return clock.now
}

// Advance moves the pinned instant forward by delta.
func (clock *Clock) Advance(delta time.Duration) {
	clock.now = clock.now.Add(delta)
}

// Fixture owns one fully isolated state tree. Root is the private container of
// the fixture HOME (Home) and the state home (StateHome); Cleanup removes it
// wholesale.
type Fixture struct {
	Root      string
	Home      string
	StateHome string
	Clock     *Clock
	Store     *state.Store
}

// New builds a fixture beneath a fresh temporary root, opens and migrates the
// state store, and registers cleanup that closes the store and removes every
// created path. Two fixtures share no path, lock, connection, key, or clock
// state.
func New(t *testing.T) *Fixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("fixture home: %v", err)
	}
	stateHome := filepath.Join(root, "state")
	clock := NewClock(fixtureOrigin())
	store := state.NewStore(state.Dependencies{StateHome: stateHome, Now: clock.Now})
	if err := store.Acquire(context.Background()); err != nil {
		t.Fatalf("fixture store acquire: %v", err)
	}
	fixture := &Fixture{Root: root, Home: home, StateHome: stateHome, Clock: clock, Store: store}
	t.Cleanup(func() { _ = fixture.cleanup() })
	return fixture
}

// Directory returns the canonical cattery state directory.
func (fixture *Fixture) Directory() string {
	return filepath.Join(fixture.StateHome, catteryDirectory)
}

func (fixture *Fixture) cleanup() error {
	if fixture.Store != nil {
		if err := fixture.Store.Close(); err != nil {
			return err
		}
		fixture.Store = nil
	}
	return os.RemoveAll(fixture.Root)
}

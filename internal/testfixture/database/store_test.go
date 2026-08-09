package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/state"
)

func TestDatabaseFixture(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"two fixtures share no paths", testFixturesShareNoPaths},
		{"store is open and migrated", testFixtureStoreOpen},
		{"connections are independent", testFixtureConnectionsIndependent},
		{"locks are independent", testFixtureLocksIndependent},
		{"keys are distinct and deferred", testFixtureKeysDistinct},
		{"clocks are deterministic and distinct", testFixtureClocksDistinct},
		{"cleanup removes every created path", testFixtureCleanupRemovesPaths},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testFixturesShareNoPaths(t *testing.T) {
	first := New(t)
	second := New(t)
	firstPaths := fixturePaths(first)
	secondPaths := fixturePaths(second)
	for index, firstPath := range firstPaths {
		if firstPath == secondPaths[index] {
			t.Fatalf("fixtures share path %q", firstPath)
		}
	}
}

func testFixtureStoreOpen(t *testing.T) {
	fixture := New(t)
	if fixture.Store.Database() == nil {
		t.Fatal("store database nil after New")
	}
	if fixture.Store.Clock() != fixture.Clock {
		t.Fatal("store does not use the fixture clock")
	}
	assertMode(t, fixture.Directory(), 0o700)
	assertMode(t, fixture.DatabasePath(), 0o600)
	assertMode(t, fixture.LockPath(), 0o600)
}

func testFixtureConnectionsIndependent(t *testing.T) {
	first := New(t)
	second := New(t)
	if first.Store.Database() == second.Store.Database() {
		t.Fatal("fixtures share a database handle")
	}
	if err := first.Store.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	reopen := state.NewStore(state.Dependencies{StateHome: first.StateHome, Clock: first.Clock})
	if err := reopen.Acquire(context.Background()); err != nil {
		t.Fatalf("first state home not reopenable: %v", err)
	}
	if err := reopen.Close(); err != nil {
		t.Fatalf("reopen close: %v", err)
	}
	if err := second.Store.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func testFixtureLocksIndependent(t *testing.T) {
	first := New(t)
	second := New(t)
	assertLocked(t, first.LockPath())
	assertLocked(t, second.LockPath())
	if err := first.Store.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	assertFree(t, first.LockPath())
	assertLocked(t, second.LockPath())
	if err := second.Store.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func assertLocked(t *testing.T, path string) {
	t.Helper()
	if err := state.NewLock(path).Acquire(); err == nil {
		t.Fatalf("lock %q is free but must be held", path)
	}
}

func assertFree(t *testing.T, path string) {
	t.Helper()
	probe := state.NewLock(path)
	if err := probe.Acquire(); err != nil {
		t.Fatalf("lock %q is held: %v", path, err)
	}
	_ = probe.Release()
}

func testFixtureKeysDistinct(t *testing.T) {
	first := New(t)
	second := New(t)
	if first.KeyPath() == second.KeyPath() {
		t.Fatal("fixtures share a key path")
	}
	if _, err := os.Lstat(first.KeyPath()); !os.IsNotExist(err) {
		t.Fatal("key file exists before a secret baseline requires it")
	}
}

func testFixtureClocksDistinct(t *testing.T) {
	first := New(t)
	second := New(t)
	if first.Clock == second.Clock {
		t.Fatal("fixtures share a clock instance")
	}
	before := first.Clock.Now()
	if !second.Clock.Now().Equal(before) {
		t.Fatal("fixture clocks do not start at the same pinned instant")
	}
	first.Clock.Advance(time.Second)
	if first.Clock.Now().Equal(second.Clock.Now()) {
		t.Fatal("advancing one clock moved the other")
	}
	if first.Clock.Now().Before(before) {
		t.Fatal("advanced clock moved backwards")
	}
}

func testFixtureCleanupRemovesPaths(t *testing.T) {
	fixture := New(t)
	paths := fixturePaths(fixture)
	if err := fixture.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("path %q still exists after cleanup: %v", path, err)
		}
	}
	if err := fixture.Cleanup(); err != nil {
		t.Fatalf("second Cleanup: %v", err)
	}
}

func fixturePaths(fixture *Fixture) []string {
	return []string{
		fixture.Root,
		fixture.Home,
		fixture.StateHome,
		fixture.Directory(),
		fixture.DatabasePath(),
		fixture.LockPath(),
		fixture.KeyPath(),
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat %q: %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode of %q = %o, want %o", path, info.Mode().Perm(), want)
	}
}

package state

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestRepositoryRows(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"registers canonical pair with timestamps", testRepositoryRegisters},
		{"re-registration preserves identity", testRepositoryReRegistration},
		{"lookup never registers", testRepositoryLookupDoesNotRegister},
		{"default replacement keeps one per home", testRepositoryDefaultReplacement},
		{"defaults across two homes", testRepositoryTwoHomes},
		{"deleted database loses rows and re-registers", testRepositoryDeletedDatabase},
		{"snapshot ordering is deterministic", testRepositorySnapshotOrdering},
		{"rollback leaves no partial change", testRepositoryRollback},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRepositoryRegisters(t *testing.T) {
	clock := &pinnedClock{now: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	store := openStore(t, Dependencies{StateHome: t.TempDir(), Clock: clock})
	root := t.TempDir()
	home := t.TempDir()
	first, err := store.RegisterRepository(root, home)
	if err != nil {
		t.Fatalf("RegisterRepository: %v", err)
	}
	assertRepositoryPair(t, first, Repository{RootPath: root, HomePath: home})
	if first.IsDefault {
		t.Fatal("registration created a default")
	}
	if !first.CreatedAt.Equal(clock.now) || !first.LastSeenAt.Equal(clock.now) {
		t.Fatalf("timestamps = %v/%v, want %v", first.CreatedAt, first.LastSeenAt, clock.now)
	}
}

func testRepositoryReRegistration(t *testing.T) {
	clock := &pinnedClock{now: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	store := openStore(t, Dependencies{StateHome: t.TempDir(), Clock: clock})
	root := t.TempDir()
	home := t.TempDir()
	first, err := store.RegisterRepository(root, home)
	if err != nil {
		t.Fatalf("RegisterRepository: %v", err)
	}
	clock.now = clock.now.Add(time.Hour)
	second, err := store.RegisterRepository(root, home)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-registration changed row id %d -> %d", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.LastSeenAt.Equal(clock.now) {
		t.Fatal("re-registration changed created_at or last_seen_at")
	}
}

func testRepositoryLookupDoesNotRegister(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	if _, err := store.LookupRepository(root, home); err == nil {
		t.Fatal("lookup found an unregistered pair")
	}
	if count := rowCount(t, store.Database().conn, "repositories"); count != 0 {
		t.Fatalf("lookup registered %d rows", count)
	}
	if _, err := store.RegisterRepository(root, home); err != nil {
		t.Fatalf("RegisterRepository: %v", err)
	}
	found, err := store.LookupRepository(root, home)
	if err != nil {
		t.Fatalf("lookup after registration: %v", err)
	}
	assertRepositoryPair(t, found, Repository{RootPath: root, HomePath: home})
}

func testRepositoryDefaultReplacement(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	home := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	if _, err := store.SetDefaultRepository(first, home); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if count := defaultCount(t, store, home); count != 1 {
		t.Fatalf("default rows = %d, want 1", count)
	}
	if _, err := store.SetDefaultRepository(second, home); err != nil {
		t.Fatalf("replace default: %v", err)
	}
	defaulted, err := store.DefaultRepository(home)
	if err != nil {
		t.Fatalf("DefaultRepository after replace: %v", err)
	}
	if defaulted.RootPath != second || defaulted.HomePath != home {
		t.Fatalf("default = (%q, %q), want (%q, %q)",
			defaulted.RootPath, defaulted.HomePath, second, home)
	}
}

func testRepositoryTwoHomes(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	homeA := t.TempDir()
	homeB := t.TempDir()
	rootA := t.TempDir()
	rootB := t.TempDir()
	if _, err := store.SetDefaultRepository(rootA, homeA); err != nil {
		t.Fatalf("set default home A: %v", err)
	}
	if _, err := store.SetDefaultRepository(rootB, homeB); err != nil {
		t.Fatalf("set default home B: %v", err)
	}
	gotA, err := store.DefaultRepository(homeA)
	if err != nil {
		t.Fatalf("default A: %v", err)
	}
	assertRepositoryPair(t, gotA, Repository{RootPath: rootA, HomePath: homeA})
	gotB, err := store.DefaultRepository(homeB)
	if err != nil {
		t.Fatalf("default B: %v", err)
	}
	assertRepositoryPair(t, gotB, Repository{RootPath: rootB, HomePath: homeB})
}

func testRepositoryDeletedDatabase(t *testing.T) {
	deps := tempDependencies(t)
	store := openStore(t, deps)
	root := t.TempDir()
	home := t.TempDir()
	if _, err := store.SetDefaultRepository(root, home); err != nil {
		t.Fatalf("SetDefaultRepository: %v", err)
	}
	_ = store.Close()
	deleteStateFiles(t, deps)
	reopened := openStore(t, deps)
	if _, err := reopened.LookupRepository(root, home); err == nil {
		t.Fatal("lookup found rows after database deletion")
	}
	if _, err := reopened.DefaultRepository(home); err == nil {
		t.Fatal("default survived database deletion")
	}
	if _, err := reopened.SetDefaultRepository(root, home); err != nil {
		t.Fatalf("re-register: %v", err)
	}
}

func testRepositorySnapshotOrdering(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	home := t.TempDir()
	roots := []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	for _, root := range roots {
		if _, err := store.RegisterRepository(root, home); err != nil {
			t.Fatalf("register %q: %v", root, err)
		}
	}
	snapshot, err := store.Repositories()
	if err != nil {
		t.Fatalf("Repositories: %v", err)
	}
	if len(snapshot) != len(roots) {
		t.Fatalf("snapshot rows = %d, want %d", len(snapshot), len(roots))
	}
	sort.Strings(roots)
	for index, root := range roots {
		if snapshot[index].RootPath != root {
			t.Fatalf("root %d = %q, want %q", index, snapshot[index].RootPath, root)
		}
	}
}

func testRepositoryRollback(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	home := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	if _, err := store.SetDefaultRepository(first, home); err != nil {
		t.Fatalf("set default: %v", err)
	}
	execOn(t, store.Database().conn, "CREATE TRIGGER repositories_abort AFTER UPDATE ON repositories BEGIN SELECT RAISE(ABORT, 'boom'); END")
	if _, err := store.SetDefaultRepository(second, home); err == nil {
		t.Fatal("SetDefaultRepository succeeded against an aborting trigger")
	}
	defaulted, err := store.DefaultRepository(home)
	if err != nil {
		t.Fatalf("DefaultRepository after rollback: %v", err)
	}
	assertRepositoryPair(t, defaulted, Repository{RootPath: first, HomePath: home})
}

func assertRepositoryPair(t *testing.T, got Repository, want Repository) {
	t.Helper()
	if got.RootPath != want.RootPath || got.HomePath != want.HomePath {
		t.Fatalf("repository = (%q, %q), want (%q, %q)",
			got.RootPath, got.HomePath, want.RootPath, want.HomePath)
	}
}

func defaultCount(t *testing.T, store *Store, home string) int64 {
	t.Helper()
	var count int64
	query := "SELECT COUNT(*) FROM repositories WHERE home_path = ? AND is_default = 1"
	if err := store.Database().conn.QueryRow(query, home).Scan(&count); err != nil {
		t.Fatalf("default count: %v", err)
	}
	return count
}

func openStore(t *testing.T, deps Dependencies) *Store {
	t.Helper()
	store := NewStore(deps)
	if err := store.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func deleteStateFiles(t *testing.T, deps Dependencies) {
	t.Helper()
	for _, name := range []string{stateDatabaseFileName, stateDatabaseFileName + "-wal", stateDatabaseFileName + "-shm"} {
		path := filepath.Join(catteryDirFor(t, deps), name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %q: %v", path, err)
		}
	}
}

type pinnedClock struct {
	now time.Time
}

func (clock *pinnedClock) Now() time.Time {
	return clock.now
}

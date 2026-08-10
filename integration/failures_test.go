package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExecutableFailures(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"pre-rename keeps the old target", testFailuresPreRename},
		{"later item preserves earlier", testFailuresLaterItem},
		{"retry recovers by equality", testFailuresRecovery},
		{"locked state store fails before mutation", testFailuresStateLock},
		{"asynchronous apply converges", testFailuresAsyncApply},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testFailuresPreRename(t *testing.T) {
	race := NewRaceFixture(t)
	race.initRepository(t)
	race.source(t, ".config/app", "v1")
	if result := race.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	race.source(t, ".config/app", "v2")
	race.blockTargetParent(t, ".config/app")
	result := race.run(t, nil, "apply")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a blocked rename", result.Code)
	}
	if string(race.target(t, ".config/app")) != "v1" {
		t.Fatal("a pre-rename failure must keep the old destination")
	}
}

func testFailuresLaterItem(t *testing.T) {
	race := NewRaceFixture(t)
	race.initRepository(t)
	race.source(t, ".config/a", "a")
	race.source(t, "x/bin/b", "b")
	if result := race.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	race.source(t, ".config/a", "a2")
	race.source(t, "x/bin/b", "b2")
	if err := os.Chmod(filepath.Join(race.home, "bin"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(race.home, "bin"), 0o700) })
	result := race.run(t, nil, "apply")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a later-item failure", result.Code)
	}
	if string(race.target(t, ".config/a")) != "a2" {
		t.Fatal("the earlier item must remain accurate")
	}
}

func testFailuresRecovery(t *testing.T) {
	race := NewRaceFixture(t)
	race.initRepository(t)
	race.source(t, ".config/app", "v1")
	if result := race.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	race.source(t, ".config/app", "v2")
	race.blockTargetParent(t, ".config/app")
	if result := race.run(t, nil, "apply"); result.Code != 1 {
		t.Fatalf("blocked apply: %+v", result)
	}
	if err := os.Chmod(filepath.Join(race.home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := race.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("recovery apply: %+v", result)
	}
	if string(race.target(t, ".config/app")) != "v2" {
		t.Fatal("the retry must converge the target")
	}
}

// testFailuresStateLock proves that a concurrently held state-store lock
// is an operational failure before any target mutation. The open-file
// description locks are Linux-only.
func testFailuresStateLock(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("open-file description locks are Linux-only")
	}
	race := NewRaceFixture(t)
	race.initRepository(t)
	race.source(t, ".config/app", "v1")
	if result := race.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	race.source(t, ".config/app", "v2")
	race.lockStateWrites(t)
	result := race.run(t, nil, "apply")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a locked state store", result.Code)
	}
	if string(race.target(t, ".config/app")) != "v1" {
		t.Fatal("a locked state store must not touch the target")
	}
}

// testFailuresAsyncApply proves the fixture's asynchronous launch,
// content polling, and completion helpers against a real apply.
func testFailuresAsyncApply(t *testing.T) {
	race := NewRaceFixture(t)
	race.initRepository(t)
	race.source(t, ".config/a", "a")
	race.source(t, "x/bin/b", "b")
	handle := race.start(t, "apply")
	race.awaitTarget(t, ".config/a", "a")
	race.awaitTarget(t, "bin/b", "b")
	result := handle.finish(t)
	if result.Code != 0 {
		t.Fatalf("async apply: %+v", result)
	}
}

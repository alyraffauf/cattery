package integration

import (
	"os"
	"path/filepath"
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

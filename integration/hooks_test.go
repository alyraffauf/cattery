package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableHooks(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordering", testHooksOrder},
		{"before failure writes nothing", testHooksBeforeFailure},
		{"after failure keeps writes", testHooksAfterFailure},
		{"result environment", testHooksResultEnv},
		{"no-op suppression", testHooksNoop},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// hookScript writes one executable hook that records its phase and result.
func hookScript(phase string) []byte {
	return []byte("#!/bin/sh\nprintf \"" + phase + " $CATTERY_RESULT\\n\" >> $CATTERY_HOME/hook-order\n")
}

// installHooks writes before and after hooks into the repository.
func installHooks(t *testing.T, env execEnv) {
	t.Helper()
	before := filepath.Join(env.repo, "_hooks", "before", "before.sh")
	after := filepath.Join(env.repo, "_hooks", "after", "after.sh")
	writeFile(t, before, hookScript("before"))
	writeFile(t, after, hookScript("after"))
	if err := os.Chmod(before, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(after, 0o755); err != nil {
		t.Fatal(err)
	}
}

func testHooksOrder(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	installHooks(t, env)
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: %+v", result)
	}
	order, err := os.ReadFile(filepath.Join(env.home, "hook-order"))
	if err != nil {
		t.Fatal(err)
	}
	if string(order) != "before pending\nafter success\n" {
		t.Fatalf("hook order = %q, want before pending then after success", order)
	}
}

func testHooksBeforeFailure(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	before := filepath.Join(env.repo, "_hooks", "before", "before.sh")
	writeFile(t, before, []byte("#!/bin/sh\nexit 3\n"))
	if err := os.Chmod(before, 0o755); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 3 {
		t.Fatalf("code = %d, want 3 for a before-hook failure", result.Code)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".config", "app")); !os.IsNotExist(err) {
		t.Fatal("a before-hook failure must cause zero writes")
	}
}

func testHooksAfterFailure(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	after := filepath.Join(env.repo, "_hooks", "after", "after.sh")
	writeFile(t, after, []byte("#!/bin/sh\nexit 3\n"))
	if err := os.Chmod(after, 0o755); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 3 {
		t.Fatalf("code = %d, want 3 for an after-hook failure", result.Code)
	}
	if _, err := os.Stat(filepath.Join(env.home, ".config", "app")); err != nil {
		t.Fatal("an after-hook failure must keep the completed writes")
	}
}

func testHooksResultEnv(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	installHooks(t, env)
	writeFile(t, filepath.Join(env.home, ".config", "app"), []byte("drifted"))
	result := env.runPty(t, []string{"skip"}, "apply")
	if result.Code != 2 {
		t.Fatalf("code = %d, want 2 after a skip", result.Code)
	}
	order, err := os.ReadFile(filepath.Join(env.home, "hook-order"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(order), "after partial") {
		t.Fatalf("hook order = %q, want after partial after a skip", order)
	}
}

func testHooksNoop(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	installHooks(t, env)
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("second apply: %+v", result)
	}
	order, err := os.ReadFile(filepath.Join(env.home, "hook-order"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(order), "before") != 2 {
		t.Fatalf("hook order = %q, a converged apply must still run hooks", order)
	}
}

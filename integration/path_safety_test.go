package integration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutablePathSafety(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"symlinked parent rejected", testPathSymlinkParent},
		{"blocking ancestor rejected", testPathBlockingAncestor},
		{"case collision rejected", testPathCaseCollision},
		{"parent child collision", testPathParentChild},
		{"source target identity", testPathIdentity},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testPathSymlinkParent(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if err := os.MkdirAll(filepath.Join(env.home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(env.home, ".config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.repo, ".config"), filepath.Join(env.home, ".config")); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a symlinked target parent", result.Code)
	}
}

func testPathBlockingAncestor(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, "x/a/b", "v1")
	writeFile(t, filepath.Join(env.home, "a"), []byte("a file"))
	result := env.run(t, nil, "apply")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a blocking ancestor", result.Code)
	}
}

func testPathCaseCollision(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	env.source(t, ".config/App", "v2")
	result := env.run(t, nil, "validate")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a case-colliding repository", result.Code)
	}
}

func testPathParentChild(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, "a", "v1")
	env.source(t, "x/a/b", "v2")
	result := env.run(t, nil, "validate")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a parent-child collision", result.Code)
	}
}

func testPathIdentity(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	result := env.run(t, nil, "init", env.home)
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a repository identical to HOME", result.Code)
	}
}

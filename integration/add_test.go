package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableAdd(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary adoption", testExecAddOrdinary},
		{"raw argument order", testExecAddOrder},
		{"explicit presence", testExecAddPresence},
		{"dry run", testExecAddDryRun},
		{"conflict rejected", testExecAddConflict},
		{"partial output", testExecAddPartial},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testExecAddOrdinary(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.home, "a.conf"), []byte("content"))
	result := env.run(t, nil, "add", "a.conf")
	if result.Code != 0 {
		t.Fatalf("add: code=%d stderr=%q", result.Code, result.Stderr)
	}
	content, err := os.ReadFile(filepath.Join(env.repo, "a.conf"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if string(content) != "content" {
		t.Fatal("the repository must carry the exact target bytes")
	}
	if !strings.Contains(result.Stdout, "completed") {
		t.Fatalf("stdout = %q, want the completed verb", result.Stdout)
	}
}

func testExecAddOrder(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.home, "b.conf"), []byte("b"))
	writeFile(t, filepath.Join(env.home, "a.conf"), []byte("a"))
	result := env.run(t, nil, "add", "b.conf", "a.conf")
	if result.Code != 0 {
		t.Fatalf("add: code=%d stderr=%q", result.Code, result.Stderr)
	}
	for _, name := range []string{"a.conf", "b.conf"} {
		if _, err := os.Stat(filepath.Join(env.repo, name)); err != nil {
			t.Fatalf("source %s: %v", name, err)
		}
	}
	if !strings.Contains(result.Stdout, "completed") {
		t.Fatalf("stdout = %q, want the completed verbs", result.Stdout)
	}
}

func testExecAddPresence(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.home, "app.conf"), []byte("content"))
	result := env.run(t, nil, "add", "--group", "apps", "app.conf")
	if result.Code != 0 {
		t.Fatalf("add: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if _, err := os.Stat(filepath.Join(env.repo, "apps", "app.conf")); err != nil {
		t.Fatalf("group source: %v", err)
	}
}

func testExecAddDryRun(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.home, "a.conf"), []byte("content"))
	result := env.run(t, nil, "add", "--dry-run", "a.conf")
	if result.Code != 2 {
		t.Fatalf("code = %d, want 2 for a dry run with pending changes", result.Code)
	}
	if _, err := os.Stat(filepath.Join(env.repo, "a.conf")); !os.IsNotExist(err) {
		t.Fatal("a dry run must not write the source")
	}
	if !strings.Contains(result.Stdout, "planned") {
		t.Fatalf("stdout = %q, want the planned verb", result.Stdout)
	}
}

func testExecAddConflict(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.home, "a.conf"), []byte("content"))
	if result := env.run(t, nil, "add", "a.conf"); result.Code != 0 {
		t.Fatalf("first add: %+v", result)
	}
	writeFile(t, filepath.Join(env.home, "a.conf"), []byte("changed"))
	result := env.run(t, nil, "add", "--secret", "a.conf")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a storage-class conflict", result.Code)
	}
}

func testExecAddPartial(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.home, "a.conf"), []byte("a"))
	writeFile(t, filepath.Join(env.home, "b.conf"), []byte("b"))
	if err := os.MkdirAll(filepath.Join(env.repo, "b.conf"), 0o700); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "add", "a.conf", "b.conf")
	if result.Code != 1 {
		t.Fatalf("code = %d, want 1 for a failed batch", result.Code)
	}
	if strings.Contains(result.Stdout, "a.conf") && !strings.Contains(result.Stdout, "partial") {
		t.Fatalf("stdout = %q, want the partial facts", result.Stdout)
	}
}

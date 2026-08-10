package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutableApply(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"first apply", testExecApplyFirst},
		{"source update", testExecApplyUpdate},
		{"drift overwrite", testExecApplyOverwrite},
		{"drift skip", testExecApplySkip},
		{"dry run", testExecApplyDryRun},
		{"status convergence", testExecApplyStatus},
		{"database loss", testExecApplyDatabaseLoss},
		{"source removal", testExecApplyRemoval},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// execEnv bundles the executable fixture and its directories.
type execEnv struct {
	fixture ProcessFixture
	repo    string
	home    string
}

// newExecEnv builds the binary and the isolated repository and HOME.
func newExecEnv(t *testing.T) execEnv {
	t.Helper()
	return execEnv{
		fixture: NewProcessFixture(t),
		repo:    t.TempDir(),
		home:    t.TempDir(),
	}
}

// initRepository runs cattery init over the environment home.
func (env execEnv) initRepository(t *testing.T) {
	t.Helper()
	result := env.run(t, nil, "init", env.repo)
	if result.Code != 0 {
		t.Fatalf("init: code=%d stderr=%q", result.Code, result.Stderr)
	}
}

// run executes the binary with the given stdin and arguments.
func (env execEnv) run(t *testing.T, stdin []string, args ...string) ProcessResult {
	t.Helper()
	return env.runInput(t, ProcessInput{Args: args, Home: env.home, Stdin: joined(stdin), Timeout: 30 * time.Second})
}

// runPty executes the binary under a pseudo-terminal.
func (env execEnv) runPty(t *testing.T, stdin []string, args ...string) ProcessResult {
	t.Helper()
	return env.runInput(t, ProcessInput{Args: args, Home: env.home, Stdin: joined(stdin), Timeout: 30 * time.Second, Pty: true})
}

// source writes one repository source file.
func (env execEnv) source(t *testing.T, relative string, content string) {
	t.Helper()
	writeFile(t, filepath.Join(env.repo, filepath.FromSlash(relative)), []byte(content))
}

// target reads one HOME-relative target file.
func (env execEnv) target(t *testing.T, relative string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(env.home, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read target %s: %v", relative, err)
	}
	return string(content)
}

// targetExists reports whether one HOME-relative target exists.
func (env execEnv) targetExists(t *testing.T, relative string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(env.home, filepath.FromSlash(relative)))
	return err == nil
}

func testExecApplyFirst(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "content")
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if env.target(t, ".config/app") != "content" {
		t.Fatal("the target must carry the source bytes")
	}
	if !strings.Contains(result.Stdout, "completed") {
		t.Fatalf("stdout = %q, want the completed verb", result.Stdout)
	}
}

func testExecApplyUpdate(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	env.source(t, ".config/app", "v2")
	result := env.run(t, nil, "apply")
	if result.Code != 0 || env.target(t, ".config/app") != "v2" {
		t.Fatalf("result = %+v, want the updated target", result)
	}
}

func testExecApplyOverwrite(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	writeFile(t, filepath.Join(env.home, ".config", "app"), []byte("drifted"))
	result := env.runPty(t, []string{"overwrite"}, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if env.target(t, ".config/app") != "v1" {
		t.Fatal("an overwrite must restore the source bytes")
	}
}

func testExecApplySkip(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("first apply: %+v", result)
	}
	writeFile(t, filepath.Join(env.home, ".config", "app"), []byte("drifted"))
	result := env.runPty(t, []string{"skip"}, "apply")
	if result.Code != 2 {
		t.Fatalf("code = %d, want 2 after a skip", result.Code)
	}
	if env.target(t, ".config/app") != "drifted" {
		t.Fatal("a skip must keep the drifted target")
	}
}

func testExecApplyDryRun(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	result := env.run(t, nil, "apply", "--dry-run")
	if result.Code != 2 {
		t.Fatalf("code = %d, want 2 for a dry run with pending changes", result.Code)
	}
	if env.targetExists(t, ".config/app") {
		t.Fatal("a dry run must not write targets")
	}
	if !strings.Contains(result.Stdout, "planned") {
		t.Fatalf("stdout = %q, want the planned verb", result.Stdout)
	}
}

func testExecApplyStatus(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("apply: %+v", result)
	}
	if result := env.run(t, nil, "status"); result.Code != 0 {
		t.Fatalf("converged status: %+v", result)
	}
	if result := env.run(t, nil, "diff"); result.Code != 0 {
		t.Fatalf("converged diff: %+v", result)
	}
}

func testExecApplyDatabaseLoss(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("apply: %+v", result)
	}
	database := filepath.Join(env.home, ".local", "state", "cattery", "state.db")
	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "--repo", env.repo, "apply")
	if result.Code != 0 {
		t.Fatalf("apply after database loss: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if env.target(t, ".config/app") != "v1" {
		t.Fatal("database loss must never change targets")
	}
}

func testExecApplyRemoval(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	env.source(t, ".config/app", "v1")
	if result := env.run(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("apply: %+v", result)
	}
	if err := os.Remove(filepath.Join(env.repo, ".config", "app")); err != nil {
		t.Fatal(err)
	}
	result := env.run(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("apply after removal: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if !env.targetExists(t, ".config/app") {
		t.Fatal("source removal must never delete the target")
	}
}

// runInput executes the binary under one full process input.
func (env execEnv) runInput(t *testing.T, input ProcessInput) ProcessResult {
	t.Helper()
	return env.fixture.Run(t, input)
}

// joined joins one answer list into scripted stdin.
func joined(stdin []string) string {
	if len(stdin) == 0 {
		return ""
	}
	return strings.Join(stdin, "\n") + "\n"
}

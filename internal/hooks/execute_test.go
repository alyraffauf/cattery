// This file exercises the execution contract of Execute:
// suppression for empty sequences, dry-run, and no-hooks, inherited streams
// and environment, before-stop versus after-aggregate failures, and the
// order the comparators produce.
package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestHookExecution(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"empty sequence is a no-op", testExecuteEmptySequence},
		{"dry-run and no-hooks suppress hooks", testExecuteSuppression},
		{"hooks inherit streams and environment", testExecuteEnvironment},
		{"cattery variables override the caller environment", testExecuteEnvironmentPrecedence},
		{"before failure stops the sequence", testExecuteBeforeStops},
		{"after failure aggregates and continues", testExecuteAfterAggregates},
		{"execution follows the before comparators", testExecuteBeforeOrder},
		{"execution follows the after comparators", testExecuteAfterOrder},
		{"missing executable is a failure", testExecuteMissingExecutable},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testExecuteEmptySequence(t *testing.T) {
	input := executeInput(t.TempDir(), deployment.HookBefore, "pending")
	if err := Execute(context.Background(), input, nil); err != nil {
		t.Fatalf("Execute with no hooks: %v", err)
	}
}

func testExecuteSuppression(t *testing.T) {
	dir := t.TempDir()
	hook := writeTestHook(t, dir, hookSpec{name: "touch.sh", phase: deployment.HookBefore, body: "touch ran.txt"})
	for _, suppressed := range []ExecuteInput{{DryRun: true}, {NoHooks: true}} {
		input := executeInput(dir, deployment.HookBefore, "pending")
		input.DryRun, input.NoHooks = suppressed.DryRun, suppressed.NoHooks
		if err := Execute(context.Background(), input, []deployment.Hook{hook}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if fileExists(filepath.Join(dir, "ran.txt")) {
			t.Fatal("suppressed execution must not run hooks")
		}
	}
}

func testExecuteEnvironment(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(t.TempDir(), "home")
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	var stdout bytes.Buffer
	hook := writeTestHook(t, dir, hookSpec{
		name: "env.sh", phase: deployment.HookBefore,
		body: `printf '%s\n' "$PWD" "$CATTERY_REPO" "$CATTERY_HOME" "$CATTERY_PLATFORM" "$CATTERY_PHASE" "$CATTERY_GROUP" "$CATTERY_RESULT"`,
	})
	input := ExecuteInput{
		RepositoryRoot: root, HomePath: home, Platform: "linux",
		Phase: deployment.HookBefore, Result: "pending", Stdout: &stdout,
	}
	if err := Execute(context.Background(), input, []deployment.Hook{hook}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := strings.Join([]string{root, root, home, "linux", "before", "", "pending"}, "\n") + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testExecuteEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATTERY_REPO", "caller-value")
	var stdout bytes.Buffer
	hook := writeTestHook(t, dir, hookSpec{
		name: "repo.sh", phase: deployment.HookBefore,
		body: `printf '%s' "$CATTERY_REPO"`,
	})
	input := ExecuteInput{
		RepositoryRoot: dir, HomePath: dir, Platform: "linux",
		Phase: deployment.HookBefore, Result: "pending", Stdout: &stdout,
	}
	if err := Execute(context.Background(), input, []deployment.Hook{hook}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stdout.String() != dir {
		t.Fatalf("CATTERY_REPO = %q, want the canonical value", stdout.String())
	}
}

func testExecuteBeforeStops(t *testing.T) {
	dir := t.TempDir()
	bad := writeTestHook(t, dir, hookSpec{name: "bad.sh", phase: deployment.HookBefore, body: "exit 7"})
	marker := writeTestHook(t, dir, hookSpec{name: "marker.sh", phase: deployment.HookBefore, body: "touch ran.txt"})
	input := executeInput(dir, deployment.HookBefore, "pending")
	err := Execute(context.Background(), input, []deployment.Hook{bad, marker})
	if err == nil {
		t.Fatal("before failure must stop execution")
	}
	if !strings.Contains(err.Error(), "status 7") {
		t.Fatalf("error = %v, want exit status 7", err)
	}
	if fileExists(filepath.Join(dir, "ran.txt")) {
		t.Fatal("remaining before hooks must not run after a failure")
	}
}

func testExecuteAfterAggregates(t *testing.T) {
	dir := t.TempDir()
	first := writeTestHook(t, dir, hookSpec{name: "first.sh", phase: deployment.HookAfter, body: "exit 7"})
	marker := writeTestHook(t, dir, hookSpec{name: "marker.sh", phase: deployment.HookAfter, body: "touch ran.txt"})
	last := writeTestHook(t, dir, hookSpec{name: "last.sh", phase: deployment.HookAfter, body: "exit 9"})
	input := executeInput(dir, deployment.HookAfter, "success")
	err := Execute(context.Background(), input, []deployment.Hook{first, marker, last})
	if err == nil {
		t.Fatal("after failures must report an error")
	}
	if !strings.Contains(err.Error(), "status 7") || !strings.Contains(err.Error(), "status 9") {
		t.Fatalf("error = %v, want both failures reported", err)
	}
	if !fileExists(filepath.Join(dir, "ran.txt")) {
		t.Fatal("remaining after hooks must still run")
	}
}

func testExecuteBeforeOrder(t *testing.T) {
	dir := t.TempDir()
	if err := Execute(context.Background(), executeInput(dir, deployment.HookBefore, "pending"), sequenceHooks(t, dir, deployment.HookBefore)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertExecutionOrder(t, dir, []string{"repo.sh", "apps-a.sh", "apps-b.sh", "ghost.sh", "zsh-a.sh", "zsh-b.sh"})
}

func testExecuteAfterOrder(t *testing.T) {
	dir := t.TempDir()
	if err := Execute(context.Background(), executeInput(dir, deployment.HookAfter, "success"), sequenceHooks(t, dir, deployment.HookAfter)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertExecutionOrder(t, dir, []string{"apps-a.sh", "apps-b.sh", "ghost.sh", "zsh-a.sh", "zsh-b.sh", "repo.sh"})
}

func testExecuteMissingExecutable(t *testing.T) {
	dir := t.TempDir()
	hook := deployment.Hook{Scope: deployment.NewScope(""), Phase: deployment.HookBefore, Name: "missing.sh", AbsolutePath: filepath.Join(dir, "missing.sh"), RepositoryPath: "missing.sh"}
	err := Execute(context.Background(), executeInput(dir, deployment.HookBefore, "pending"), []deployment.Hook{hook})
	if err == nil {
		t.Fatal("missing executable must fail")
	}
	if !strings.Contains(err.Error(), "missing.sh") {
		t.Fatalf("error = %v, want the hook path", err)
	}
}

type hookSpec struct {
	name  string
	group string
	phase deployment.HookPhase
	body  string
}

func writeTestHook(t *testing.T, dir string, spec hookSpec) deployment.Hook {
	t.Helper()
	path := filepath.Join(dir, spec.name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+spec.body+"\n"), 0o755); err != nil {
		t.Fatalf("write hook %s: %v", spec.name, err)
	}
	return deployment.Hook{
		Scope: deployment.NewScope(spec.group), Phase: spec.phase, Name: spec.name,
		AbsolutePath: path, RepositoryPath: spec.name,
	}
}

func recordingHook(t *testing.T, dir string, spec hookSpec) deployment.Hook {
	spec.body = fmt.Sprintf("echo %s >> order.txt", spec.name)
	return writeTestHook(t, dir, spec)
}

func sequenceHooks(t *testing.T, dir string, phase deployment.HookPhase) []deployment.Hook {
	specs := []hookSpec{
		{name: "zsh-b.sh", group: "zsh"},
		{name: "apps-a.sh", group: "apps"},
		{name: "repo.sh", group: ""},
		{name: "apps-b.sh", group: "apps"},
		{name: "ghost.sh", group: "ghostty"},
		{name: "zsh-a.sh", group: "zsh"},
	}
	ordered := make([]deployment.Hook, 0, len(specs))
	for _, spec := range specs {
		spec.phase = phase
		ordered = append(ordered, recordingHook(t, dir, spec))
	}
	if phase == deployment.HookBefore {
		SortBefore(ordered)
	} else {
		SortAfter(ordered)
	}
	return ordered
}

func executeInput(root string, phase deployment.HookPhase, result string) ExecuteInput {
	return ExecuteInput{
		RepositoryRoot: root,
		HomePath:       root,
		Platform:       "linux",
		Phase:          phase,
		Result:         result,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func assertExecutionOrder(t *testing.T, dir string, want []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "order.txt"))
	if err != nil {
		t.Fatalf("read order: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if !slices.Equal(got, want) {
		t.Fatalf("execution order = %v, want %v", got, want)
	}
}

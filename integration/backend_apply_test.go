package integration

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/bootstrap"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestBackendApply(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"first apply creates the target", testApplyFirst},
		{"source update applies automatically", testApplySourceUpdate},
		{"target drift decides", testApplyDrift},
		{"target drift skips", testApplySkip},
		{"source removal retires", testApplyRetirement},
		{"hooks run around the phase", testApplyHooks},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// scriptedApply builds one apply service over the fixture adapters with a
// scripted interactive prompt.
func scriptedApply(t *testing.T, fixture BackendFixture, answers []string) *apply.Service {
	t.Helper()
	input := ""
	if len(answers) > 0 {
		input = strings.Join(answers, "\n") + "\n"
	}
	return bootstrap.BuildApplications(bootstrap.ApplicationsInput{
		Adapters:   fixture.Adapters,
		Home:       fixture.Home,
		Platform:   fixture.Platform,
		Protected:  []string{fixture.StateHome},
		Stdin:      strings.NewReader(input),
		Stderr:     io.Discard,
		IsTerminal: func(fd int) bool { return true },
	}).Apply
}

// applyRequest freezes one apply over the fixture default repository.
func applyRequest(fixture BackendFixture) apply.Request {
	return apply.Request{Repository: apply.RepositoryInput{WorkingDir: fixture.Home}}
}

// applyOutcome names one expected summary.
type applyOutcome struct {
	completed int
	partial   int
}

// assertApply asserts one apply outcome.
func assertApply(t *testing.T, result apply.Result, want applyOutcome) {
	t.Helper()
	if result.Summary.Completed != want.completed || result.Summary.Partial != want.partial {
		t.Fatalf("summary = %+v, want completed=%d partial=%d", result.Summary, want.completed, want.partial)
	}
}

// readTarget reads one HOME-relative target.
func readTarget(t *testing.T, fixture BackendFixture, relative string) []byte {
	t.Helper()
	content, err := os.ReadFile(fixture.TargetPath(relative))
	if err != nil {
		t.Fatalf("read target %s: %v", relative, err)
	}
	return content
}

// fileRow reads the persisted file baseline of one target.
func fileRow(t *testing.T, fixture BackendFixture, target string) state.FileBaseline {
	t.Helper()
	row, err := fixture.Store.FileBaseline(fixture.Repository, fixture.Home, target)
	if err != nil {
		t.Fatalf("file row %s: %v", target, err)
	}
	return row
}

func testApplyFirst(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, ".config/app", []byte("content"))
	service := scriptedApply(t, fixture, nil)
	result, err := service.Apply(context.Background(), applyRequest(fixture))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertApply(t, result, applyOutcome{completed: 1})
	if string(readTarget(t, fixture, ".config/app")) != "content" {
		t.Fatal("the target must carry the source bytes")
	}
	if row := fileRow(t, fixture, ".config/app"); row.Status != state.StatusActive {
		t.Fatalf("row = %+v, want an active baseline", row)
	}
}

func testApplySourceUpdate(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, ".config/app", []byte("v1"))
	service := scriptedApply(t, fixture, nil)
	if _, err := service.Apply(context.Background(), applyRequest(fixture)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	fixture.WriteRepository(t, ".config/app", []byte("v2"))
	result, err := service.Apply(context.Background(), applyRequest(fixture))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	assertApply(t, result, applyOutcome{completed: 1})
	if string(readTarget(t, fixture, ".config/app")) != "v2" {
		t.Fatal("a source-only change must apply automatically")
	}
}

func testApplyDrift(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, ".config/app", []byte("v1"))
	service := scriptedApply(t, fixture, nil)
	if _, err := service.Apply(context.Background(), applyRequest(fixture)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	fixture.WriteTarget(t, ".config/app", []byte("drifted"))
	service = scriptedApply(t, fixture, []string{"overwrite"})
	result, err := service.Apply(context.Background(), applyRequest(fixture))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertApply(t, result, applyOutcome{completed: 1})
	if string(readTarget(t, fixture, ".config/app")) != "v1" {
		t.Fatal("an overwrite decision must restore the source bytes")
	}
}

func testApplySkip(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, ".config/app", []byte("v1"))
	service := scriptedApply(t, fixture, nil)
	if _, err := service.Apply(context.Background(), applyRequest(fixture)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	fixture.WriteTarget(t, ".config/app", []byte("drifted"))
	service = scriptedApply(t, fixture, []string{"skip"})
	result, err := service.Apply(context.Background(), applyRequest(fixture))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Summary.Planned != 1 {
		t.Fatalf("summary = %+v, want one planned skip", result.Summary)
	}
	if string(readTarget(t, fixture, ".config/app")) != "drifted" {
		t.Fatal("a skip must leave the drifted target")
	}
}

func testApplyRetirement(t *testing.T) {
	fixture, service := appliedFixture(t, ".config/app", "v1")
	if err := os.Remove(fixture.RepositoryPath(".config/app")); err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), applyRequest(fixture))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	assertApply(t, result, applyOutcome{completed: 1})
	if _, err := os.Stat(fixture.TargetPath(".config/app")); err != nil {
		t.Fatal("retirement must never delete the deployed target")
	}
	if row := fileRow(t, fixture, ".config/app"); row.Status != state.StatusRetired {
		t.Fatalf("row = %+v, want a retired row", row)
	}
}

func testApplyHooks(t *testing.T) {
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, ".config/app", []byte("v1"))
	writeHook(t, fixture, hookSpec{relative: "_hooks/before/before.sh", content: []byte("#!/bin/sh\nprintf before >> $CATTERY_HOME/hook-order\n")})
	writeHook(t, fixture, hookSpec{relative: "_hooks/after/after.sh", content: []byte("#!/bin/sh\nprintf after >> $CATTERY_HOME/hook-order\n")})
	service := scriptedApply(t, fixture, nil)
	if _, err := service.Apply(context.Background(), applyRequest(fixture)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if string(readTarget(t, fixture, "hook-order")) != "beforeafter" {
		t.Fatalf("hook order = %q, want before then after", string(readTarget(t, fixture, "hook-order")))
	}
}

// hookSpec names one executable hook.
type hookSpec struct {
	relative string
	content  []byte
}

// writeHook writes and chmods one executable repository hook.
func writeHook(t *testing.T, fixture BackendFixture, spec hookSpec) {
	t.Helper()
	path := fixture.RepositoryPath(spec.relative)
	writeFile(t, path, spec.content)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// appliedFixture builds a registered fixture with one applied source.
func appliedFixture(t *testing.T, relative, content string) (BackendFixture, *apply.Service) {
	t.Helper()
	fixture := NewBackendFixture(t)
	fixture.Acquire(t)
	fixture.RegisterRepository(t)
	fixture.WriteRepository(t, relative, []byte(content))
	service := scriptedApply(t, fixture, nil)
	if _, err := service.Apply(context.Background(), applyRequest(fixture)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	return fixture, service
}

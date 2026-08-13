package cli

import (
	"bytes"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/apply"
)

func TestApplyRenderer(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"dry run verbs", testRenderApplyDryRun},
		{"partial items", testRenderApplyPartial},
		{"summary counts", testRenderApplySummary},
		{"escaping", testRenderApplyEscaping},
		{"writer failure", testRenderApplyWriter},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRenderApplyDryRun(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := apply.Result{Items: []apply.ItemResult{
		{TargetPath: "a.conf", Status: apply.StatusPlanned, Kind: apply.ActionKindWriteSource},
	}, Summary: apply.Summary{Planned: 1}}
	if err := renderApply(stdout, result, true); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Ready to apply — 1 change\n\n  Update   ~/a.conf\n           This change has not been applied.\n\nNo files were changed.\nNext: run `cattery apply` to make these changes.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderApplyPartial(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := apply.Result{Items: []apply.ItemResult{
		{TargetPath: "a.conf", Status: apply.StatusCompleted, Kind: apply.ActionKindWriteSource},
		{TargetPath: "b.conf", Status: apply.StatusPartial, Kind: apply.ActionKindRealizeAlias},
	}, Summary: apply.Summary{Completed: 1, Partial: 1}}
	if err := renderApply(stdout, result, false); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Applied with unresolved items — 1 change\n\n  Update   ~/a.conf\n\n  Skipped  ~/b.conf\n           This item was not changed.\n\n1 change applied; 1 item need attention.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderApplySummary(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := apply.Result{Summary: apply.Summary{Planned: 3, Completed: 2, Partial: 1}}
	if err := renderApply(stdout, result, false); err != nil {
		t.Fatalf("render: %v", err)
	}
	if stdout.String() != "\nNothing to apply.\n" {
		t.Fatalf("stdout = %q, want the empty outcome", stdout.String())
	}
}

func testRenderApplyEscaping(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := apply.Result{Items: []apply.ItemResult{
		{TargetPath: "dir/bad\nname", Status: apply.StatusCompleted, Kind: apply.ActionKindWriteSource},
	}}
	if err := renderApply(stdout, result, false); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("\nname")) {
		t.Fatalf("stdout = %q, a control character must not inject a line", stdout.String())
	}
}

func testRenderApplyWriter(t *testing.T) {
	if err := renderApply(failingWriter{}, apply.Result{}, false); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

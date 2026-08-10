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
	if err := renderApply(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "$HOME/a.conf planned write-source\nsummary planned=1 completed=0 partial=0\n"
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
	if err := renderApply(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "$HOME/a.conf completed write-source\n$HOME/b.conf partial realize-alias\n" +
		"summary planned=0 completed=1 partial=1\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderApplySummary(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := apply.Result{Summary: apply.Summary{Planned: 3, Completed: 2, Partial: 1}}
	if err := renderApply(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if stdout.String() != "summary planned=3 completed=2 partial=1\n" {
		t.Fatalf("stdout = %q, want the summary line", stdout.String())
	}
}

func testRenderApplyEscaping(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := apply.Result{Items: []apply.ItemResult{
		{TargetPath: "dir/bad\nname", Status: apply.StatusCompleted, Kind: apply.ActionKindWriteSource},
	}}
	if err := renderApply(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("\nname")) {
		t.Fatalf("stdout = %q, a control character must not inject a line", stdout.String())
	}
}

func testRenderApplyWriter(t *testing.T) {
	if err := renderApply(failingWriter{}, apply.Result{}); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

package cli

import (
	"bytes"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/add"
)

func TestAddRenderer(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"item lines", testRenderAddItems},
		{"summary counts", testRenderAddSummary},
		{"dry run verbs", testRenderAddDryRun},
		{"escaping", testRenderAddEscaping},
		{"writer failure", testRenderAddWriter},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRenderAddItems(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := add.Result{Items: []add.ItemResult{
		{Target: "a.conf", Source: "a.conf", Status: add.StatusCompleted},
		{Target: "token", Source: "apps/token", Status: add.StatusCompleted},
	}, Summary: add.Summary{Completed: 2}}
	if err := renderAdd(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Added — 2 changes\n\n  Added    ~/a.conf\n           from a.conf\n\n  Added    ~/token\n           from apps/token\n\n2 changes added.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderAddSummary(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := add.Result{Items: []add.ItemResult{
		{Target: "a", Status: add.StatusPlanned},
		{Target: "b", Status: add.StatusCompleted},
		{Target: "c", Status: add.StatusPartial},
	}, Summary: add.Summary{Planned: 1, Completed: 1, Partial: 1}}
	if err := renderAdd(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Ready to add — 1 change")) || !bytes.Contains(stdout.Bytes(), []byte("Skipped  ~/c")) {
		t.Fatalf("stdout = %q, want the human-readable plan", stdout.String())
	}
}

func testRenderAddDryRun(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := add.Result{Items: []add.ItemResult{
		{Target: "a.conf", Source: "a.conf", Status: add.StatusPlanned},
	}, Summary: add.Summary{Planned: 1}}
	if err := renderAdd(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Add      ~/a.conf")) {
		t.Fatalf("stdout = %q, want the planned action", stdout.String())
	}
}

func testRenderAddEscaping(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := add.Result{Items: []add.ItemResult{
		{Target: "dir/bad\nname", Source: "dir/bad\nname", Status: add.StatusCompleted},
	}}
	if err := renderAdd(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("\nname")) {
		t.Fatalf("stdout = %q, a control character must not inject a line", stdout.String())
	}
}

func testRenderAddWriter(t *testing.T) {
	if err := renderAdd(failingWriter{}, add.Result{}); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

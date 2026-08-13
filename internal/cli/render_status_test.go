package cli

import (
	"bytes"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/inspect"
)

// frozenResult freezes one status result over the given records.
func frozenResult(records []inspect.StatusRecord, converged bool) inspect.StatusResult {
	return inspect.NewStatusResult(records, converged)
}

func TestStatusRenderer(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"pending records", testRenderStatusRecords},
		{"retired records", testRenderStatusRetired},
		{"summary line", testRenderStatusSummary},
		{"escaping", testRenderStatusEscaping},
		{"writer failure", testRenderStatusWriter},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRenderStatusRecords(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := frozenResult([]inspect.StatusRecord{
		statusRecord("a.conf", inspect.StatusKindFile, "write-source"),
		statusRecord("bin/tool", inspect.StatusKindAlias, "realize-alias"),
	}, false)
	if err := renderStatus(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Changes needed — 2 changes\n\n  Update   ~/a.conf\n\n  Link     ~/bin/tool\n\nNo files were changed.\nNext: run `cattery apply` to make these changes.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderStatusRetired(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := frozenResult([]inspect.StatusRecord{
		statusRecord("gone.conf", inspect.StatusKindRetired, "retire-file"),
	}, true)
	if err := renderStatus(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "\nEverything is up to date.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderStatusSummary(t *testing.T) {
	stdout := &bytes.Buffer{}
	records := []inspect.StatusRecord{
		statusRecord("a", inspect.StatusKindFile, "write-source"),
		statusRecord("b", inspect.StatusKindFile, "write-source"),
		statusRecord("c", inspect.StatusKindAlias, "realize-alias"),
		statusRecord("d", inspect.StatusKindAlias, "realize-alias"),
		statusRecord("e", inspect.StatusKindAlias, "realize-alias"),
		statusRecord("f", inspect.StatusKindRetired, "retire-file"),
		statusRecord("g", inspect.StatusKindRetired, "retire-file"),
		statusRecord("h", inspect.StatusKindRetired, "retire-file"),
		statusRecord("i", inspect.StatusKindRetired, "retire-file"),
	}
	result := frozenResult(records, true)
	if err := renderStatus(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if stdout.String() != "\nEverything is up to date.\n" {
		t.Fatalf("stdout = %q, want the converged outcome", stdout.String())
	}
}

func testRenderStatusEscaping(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := frozenResult([]inspect.StatusRecord{
		statusRecord("dir/weird\nname", inspect.StatusKindFile, "write-source"),
	}, false)
	if err := renderStatus(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("\nname")) {
		t.Fatalf("stdout = %q, a control character must not inject a line", stdout.String())
	}
}

func testRenderStatusWriter(t *testing.T) {
	result := frozenResult([]inspect.StatusRecord{
		statusRecord("a.conf", inspect.StatusKindFile, "write-source"),
	}, false)
	if err := renderStatus(failingWriter{}, result); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

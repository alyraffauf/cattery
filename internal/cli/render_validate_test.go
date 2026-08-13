package cli

import (
	"bytes"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/validate"
)

func TestValidateRenderer(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"two count lines", testRenderTwoLines},
		{"deterministic order", testRenderOrder},
		{"writer failure", testRenderWriterFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testRenderTwoLines(t *testing.T) {
	stdout := &bytes.Buffer{}
	if err := renderValidate(stdout, validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "linux", Files: 2, Secrets: 1, Aliases: 0, Groups: 3},
	}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if stdout.String() != "Repository is valid.\n\n  linux\n    Files: 2  Secrets: 1  Links: 0  Groups: 3\n" {
		t.Fatalf("stdout = %q, want the validation summary", stdout.String())
	}
}

func testRenderOrder(t *testing.T) {
	stdout := &bytes.Buffer{}
	if err := renderValidate(stdout, validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "linux", Files: 1},
		{Platform: "darwin", Files: 2},
	}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := stdout.String()
	want := "Repository is valid.\n\n  linux\n    Files: 1  Secrets: 0  Links: 0  Groups: 0\n\n  darwin\n    Files: 2  Secrets: 0  Links: 0  Groups: 0\n"
	if got != want {
		t.Fatalf("stdout = %q, want the given sorted order", got)
	}
}

func testRenderWriterFailure(t *testing.T) {
	if err := renderValidate(failingWriter{}, validate.Result{Platforms: []validate.PlatformCount{
		{Platform: "linux"},
	}}); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

package cli

import (
	"bytes"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/inspect"
)

func TestDiffRenderer(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"every record tag", testRenderDiffTags},
		{"text payload", testRenderDiffText},
		{"binary payload", testRenderDiffBinary},
		{"secret marker only", testRenderDiffSecret},
		{"aliases and retirement", testRenderDiffAliases},
		{"escaping", testRenderDiffEscaping},
		{"writer failure", testRenderDiffWriter},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// diffResult freezes one unconverged diff result over the given records.
func diffResult(records ...inspect.DiffRecord) inspect.DiffResult {
	return inspect.NewDiffResult(records, false)
}

// diffSpec names one frozen diff record.
type diffSpec struct {
	target string
	kind   inspect.StatusKind
	tag    string
	action string
}

// diffRecord freezes one diff record over the spec.
func diffRecord(spec diffSpec) inspect.DiffRecord {
	return inspect.NewDiffRecord(inspect.DiffRecordInput{
		TargetPath: spec.target, Kind: spec.kind, Tag: spec.tag, Action: spec.action,
	})
}

func testRenderDiffTags(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := diffResult(
		diffRecord(diffSpec{target: "a.conf", kind: inspect.StatusKindFile, tag: "none", action: "write-source"}),
		diffRecord(diffSpec{target: "b.conf", kind: inspect.StatusKindFile, tag: "binary", action: "write-source"}),
		diffRecord(diffSpec{target: "c.conf", kind: inspect.StatusKindFile, tag: "secret", action: "write-source"}),
	)
	if err := renderDiff(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Changes to review — 3 changes\n\n  Update   ~/a.conf\n\n  Update   ~/b.conf\n           Binary content differs (0 bytes in repository, 0 bytes locally).\n\n  Update   ~/c.conf\n           Encrypted secret content differs; its plaintext is not shown.\n\nNext: run `cattery apply` to make these changes.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderDiffText(t *testing.T) {
	stdout := &bytes.Buffer{}
	record := inspect.NewDiffRecord(inspect.DiffRecordInput{
		TargetPath: "a.conf", Kind: inspect.StatusKindFile, Tag: "text", Action: "write-source",
		SourceLabel: "repo/a.conf", TargetLabel: "$HOME/a.conf", Lines: "-old\n+new\n",
	})
	if err := renderDiff(stdout, diffResult(record)); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Changes to review — 1 change\n\n  Update   ~/a.conf\n\n-old\n+new\n\nNext: run `cattery apply` to make these changes.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderDiffBinary(t *testing.T) {
	stdout := &bytes.Buffer{}
	record := inspect.NewDiffRecord(inspect.DiffRecordInput{
		TargetPath: "b.bin", Kind: inspect.StatusKindFile, Tag: "binary", Action: "write-source",
		SourceSize: 3, TargetSize: 5,
	})
	if err := renderDiff(stdout, diffResult(record)); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Binary content differs (3 bytes in repository, 5 bytes locally).")) {
		t.Fatalf("stdout = %q, want the binary sizes", stdout.String())
	}
}

func testRenderDiffSecret(t *testing.T) {
	stdout := &bytes.Buffer{}
	record := inspect.NewDiffRecord(inspect.DiffRecordInput{
		TargetPath: "token", Kind: inspect.StatusKindFile, Tag: "secret", Action: "write-source",
		SourceLabel: "secret-leak", Lines: "secret-leak", SourceSize: 99, TargetSize: 99,
	})
	if err := renderDiff(stdout, diffResult(record)); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("secret-leak")) {
		t.Fatalf("stdout = %q, a secret record must render zero payload", stdout.String())
	}
}

func testRenderDiffAliases(t *testing.T) {
	stdout := &bytes.Buffer{}
	result := diffResult(
		diffRecord(diffSpec{target: "bin/tool", kind: inspect.StatusKindAlias, tag: "none", action: "realize-alias"}),
		diffRecord(diffSpec{target: "gone.conf", kind: inspect.StatusKindRetired, tag: "none", action: "retire-file"}),
	)
	if err := renderDiff(stdout, result); err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "Changes to review — 2 changes\n\n  Link     ~/bin/tool\n\n  Forget   ~/gone.conf\n           Its repository source is gone; the file in your home directory is left untouched.\n\nNext: run `cattery apply` to make these changes.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func testRenderDiffEscaping(t *testing.T) {
	stdout := &bytes.Buffer{}
	record := diffRecord(diffSpec{target: "dir/bad\nname", kind: inspect.StatusKindFile, tag: "none", action: "write-source"})
	if err := renderDiff(stdout, diffResult(record)); err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(stdout.Bytes(), []byte("\nname")) {
		t.Fatalf("stdout = %q, a control character must not inject a line", stdout.String())
	}
}

func testRenderDiffWriter(t *testing.T) {
	if err := renderDiff(failingWriter{}, diffResult()); err == nil {
		t.Fatal("a writer failure must surface")
	}
}

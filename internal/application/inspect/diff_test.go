package inspect

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/diff"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestDiffService(t *testing.T) {
	t.Run("diff and status records agree", testDiffStatusParity)
	t.Run("text content renders a safe diff", testDiffText)
	t.Run("binary content renders facts only", testDiffBinary)
	t.Run("secret records carry no payload", testDiffSecret)
	t.Run("alias-only drift fails the difference", testDiffAliasOnly)
	t.Run("converged state returns no records", testDiffConvergence)
	t.Run("partial evaluation errors propagate", testDiffErrors)
}

// singleDiffRecord requires exactly one diff record and returns it.
func singleDiffRecord(t *testing.T, result DiffResult) DiffRecord {
	t.Helper()
	records := result.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	return records[0]
}

func testDiffStatusParity(t *testing.T) {
	t.Run("alias drift matches status", testDiffAliasParity)
	t.Run("retirement drift matches status", testDiffRetirementParity)
	t.Run("records stay path sorted", testDiffSorting)
}

func testDiffAliasParity(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{
		"g1/g1-file.conf": "source g1\n",
		"g1/_routes.toml": routesTOM("g1-file.conf", "bin/tool"),
	})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
	}}
	status, statusErr := fx.service.Status(context.Background(), Request{})
	diffResult, diffErr := fx.service.Diff(context.Background(), Request{})
	assertKind(t, statusErr, failure.Difference)
	assertKind(t, diffErr, failure.Difference)
	statusRecord := singleRecord(t, status)
	diffRecord := singleDiffRecord(t, diffResult)
	if diffRecord.TargetPath() != statusRecord.TargetPath() || diffRecord.Kind() != statusRecord.Kind() ||
		diffRecord.Action() != statusRecord.Action() || diffRecord.Reason() != statusRecord.Reason() ||
		diffRecord.Converged() != statusRecord.Converged() {
		t.Fatalf("diff record %+v differs from status record %+v", diffRecord, statusRecord)
	}
}

func testDiffRetirementParity(t *testing.T) {
	fx := newFixture(t)
	useRepository(t, fx, []string{"g1"})
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "old.conf", group: "g2", source: []byte("stale\n"), content: []byte("stale\n")}, nil),
	}}
	status, statusErr := fx.service.Status(context.Background(), Request{Groups: []string{"g2"}})
	diffResult, diffErr := fx.service.Diff(context.Background(), Request{Groups: []string{"g2"}})
	assertKind(t, statusErr, failure.Difference)
	assertKind(t, diffErr, failure.Difference)
	statusRecord := singleRecord(t, status)
	diffRecord := singleDiffRecord(t, diffResult)
	if diffRecord.Kind() != StatusKindRetired || diffRecord.Action() != "retire-state" ||
		diffRecord.Reason() != statusRecord.Reason() || diffRecord.Converged() {
		t.Fatalf("record = %+v", diffRecord)
	}
}

func testDiffSorting(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{
		"g1/b.conf": "source b\n",
		"g1/a.conf": "source a\n",
	})
	writeFile(t, filepath.Join(fx.home, "a.conf"), "drifted a\n")
	writeFile(t, filepath.Join(fx.home, "b.conf"), "drifted b\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "a.conf", group: "g1", source: []byte("source a\n"), content: []byte("older a\n")}, nil),
		fileRow(baselineInput{target: "b.conf", group: "g1", source: []byte("source b\n"), content: []byte("older b\n")}, nil),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	records := result.Records()
	if len(records) != 2 || records[0].TargetPath() != "a.conf" || records[1].TargetPath() != "b.conf" {
		t.Fatalf("records = %v, want a.conf then b.conf", records)
	}
}

func testDiffText(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "alpha\nbeta\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "alpha\ngamma\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("alpha\nbeta\n"), content: []byte("alpha\nbeta\n")}, nil),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleDiffRecord(t, result)
	if record.Tag() != diff.TagText || record.Kind() != StatusKindFile {
		t.Fatalf("record = %+v", record)
	}
	for _, fragment := range []string{"--- repo/g1/g1-file.conf", "+++ $HOME/g1-file.conf", "-beta", "+gamma"} {
		if !strings.Contains(record.Lines(), fragment) {
			t.Fatalf("lines lack %q:\n%s", fragment, record.Lines())
		}
	}
}

func testDiffBinary(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "alpha\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "alpha\x1bbeta\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("alpha\n"), content: []byte("alpha\n")}, nil),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleDiffRecord(t, result)
	if record.Tag() != diff.TagBinary || record.Lines() != "" {
		t.Fatalf("record = %+v", record)
	}
	if record.SourceSize() != int64(len("alpha\n")) || record.TargetSize() != int64(len("alpha\x1bbeta\n")) {
		t.Fatalf("sizes = %d/%d", record.SourceSize(), record.TargetSize())
	}
	if record.SourceHash() != deployment.Ordinary([]byte("alpha\n")) ||
		record.TargetHash() != deployment.Ordinary([]byte("alpha\x1bbeta\n")) {
		t.Fatalf("binary hashes mismatch")
	}
}

func testDiffSecret(t *testing.T) {
	fx := newFixture(t)
	secretRepository(t, fx)
	writeFile(t, filepath.Join(fx.home, ".bashrc"), "export X=1\n")
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	writePrivateFile(t, filepath.Join(fx.home, "token"), "other\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: ".bashrc", source: []byte("export X=1\n"), content: []byte("export X=1\n")}, nil),
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
		fileRow(baselineInput{target: "token", group: "g1", source: secretFixtureJSON(), content: []byte("plain\n")}, &fx.rows.key),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleDiffRecord(t, result)
	if record.Tag() != diff.TagSecret || record.Action() != "needs-decision" {
		t.Fatalf("record = %+v", record)
	}
	if record.Lines() != "" || record.SourceSize() != 0 || record.TargetSize() != 0 ||
		record.SourceHash() != (deployment.Digest{}) || record.TargetHash() != (deployment.Digest{}) {
		t.Fatalf("secret record leaked content or facts")
	}
}

func testDiffAliasOnly(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{
		"g1/g1-file.conf": "source g1\n",
		"g1/_routes.toml": routesTOM("g1-file.conf", "bin/tool"),
	})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleDiffRecord(t, result)
	if record.Kind() != StatusKindAlias || record.Action() != "create-alias" || record.Converged() {
		t.Fatalf("record = %+v", record)
	}
	if result.Files() != 0 || result.Aliases() != 1 || result.Retired() != 0 || result.Converged() {
		t.Fatalf("counts = %d/%d/%d converged=%v", result.Files(), result.Aliases(), result.Retired(), result.Converged())
	}
}

func testDiffConvergence(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(result.Records()) != 0 || !result.Converged() {
		t.Fatalf("records = %d converged=%v, want no records", len(result.Records()), result.Converged())
	}
}

func testDiffErrors(t *testing.T) {
	t.Run("unknown group is invalid input", testDiffUnknownGroup)
	t.Run("state read failure is operational", testDiffStateRead)
	t.Run("drift returns partial records with the difference", testDiffPartialResult)
}

func testDiffUnknownGroup(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	_, err := fx.service.Diff(context.Background(), Request{Groups: []string{"ghost"}})
	assertKind(t, err, failure.InvalidInput)
}

func testDiffStateRead(t *testing.T) {
	fx := newFixture(t)
	fx.rows.fail = errors.New("database closed")
	_, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Operational)
}

func testDiffPartialResult(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "drifted\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("applied\n")}, nil),
	}}
	result, err := fx.service.Diff(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	if len(result.Records()) != 1 {
		t.Fatalf("partial records = %d, want 1", len(result.Records()))
	}
}

package inspect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestStatusService(t *testing.T) {
	t.Run("files report drift and convergence", testStatusFiles)
	t.Run("aliases report drift and convergence", testStatusAliases)
	t.Run("retirement-only drift stays pending", testStatusRetirement)
	t.Run("converged state returns no records", testStatusConvergence)
	t.Run("partial evaluation errors propagate", testStatusErrors)
}

// useFileRepository points the fixture at a repository containing exactly
// the given group files.
func useFileRepository(t *testing.T, fx *fixture, files map[string]string) {
	t.Helper()
	fx.root = t.TempDir()
	for path, content := range files {
		writeFile(t, filepath.Join(fx.root, path), content)
	}
	fx.source.identity.Root = fx.root
}

// routesTOM builds the routes manifest declaring one alias.
func routesTOM(canonical, alias string) string {
	return "version = 1\n\n[symlinks.all]\n\"" + canonical + "\" = [\"" + alias + "\"]\n"
}

// aliasRow builds an active baselined alias row.
func aliasRow(alias, canonical, group string) state.AliasBaseline {
	return state.AliasBaseline{
		AliasPath: alias, CanonicalTargetPath: canonical,
		GroupName: group, Layer: state.LayerAll, Status: state.StatusActive,
	}
}

// fixtureLink creates the symlink at path with the given payload.
func fixtureLink(t *testing.T, path, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, path); err != nil {
		t.Fatal(err)
	}
}

// singleRecord requires exactly one status record and returns it.
func singleRecord(t *testing.T, result StatusResult) StatusRecord {
	t.Helper()
	records := result.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	return records[0]
}

func testStatusFiles(t *testing.T) {
	t.Run("target drift needs a decision", testStatusTargetDrift)
	t.Run("source change applies automatically", testStatusSourceChange)
	t.Run("no-op file produces no record", testStatusNoOpFile)
}

func testStatusTargetDrift(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "drifted\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("applied\n")}, nil),
	}}
	result, err := fx.service.Status(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleRecord(t, result)
	if record.TargetPath() != "g1-file.conf" || record.Kind() != StatusKindFile ||
		record.Action() != "needs-decision" {
		t.Fatalf("record = %+v", record)
	}
	if result.Files() != 1 || result.Aliases() != 0 || result.Retired() != 0 || result.Converged() {
		t.Fatalf("counts = %d/%d/%d converged=%v", result.Files(), result.Aliases(), result.Retired(), result.Converged())
	}
}

func testStatusSourceChange(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("older source\n"), content: []byte("source g1\n")}, nil),
	}}
	result, err := fx.service.Status(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleRecord(t, result)
	if record.Action() != "write-source-to-target" {
		t.Fatalf("record = %+v", record)
	}
}

func testStatusNoOpFile(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
	}}
	result, err := fx.service.Status(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Records()) != 0 || !result.Converged() {
		t.Fatalf("records = %d converged=%v, want no records", len(result.Records()), result.Converged())
	}
}

func testStatusAliases(t *testing.T) {
	t.Run("missing alias is created", testStatusAliasCreate)
	t.Run("exact alias converges", testStatusAliasExact)
}

func testStatusAliasCreate(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{
		"g1/g1-file.conf": "source g1\n",
		"g1/_routes.toml": routesTOM("g1-file.conf", "bin/tool"),
	})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
	}}
	result, err := fx.service.Status(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	record := singleRecord(t, result)
	if record.Kind() != StatusKindAlias || record.Action() != "create-alias" {
		t.Fatalf("record = %+v", record)
	}
	if result.Aliases() != 1 || result.Converged() {
		t.Fatalf("counts = %d/%d/%d converged=%v", result.Files(), result.Aliases(), result.Retired(), result.Converged())
	}
}

func testStatusAliasExact(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{
		"g1/g1-file.conf": "source g1\n",
		"g1/_routes.toml": routesTOM("g1-file.conf", "bin/tool"),
	})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fixtureLink(t, filepath.Join(fx.home, "bin", "tool"), "../g1-file.conf")
	fx.rows.rows = stateRows{
		files: []state.FileBaseline{
			fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
		},
		aliases: []state.AliasBaseline{aliasRow("bin/tool", "g1-file.conf", "g1")},
	}
	result, err := fx.service.Status(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Records()) != 0 || !result.Converged() {
		t.Fatalf("records = %d converged=%v, want converged", len(result.Records()), result.Converged())
	}
}

func testStatusRetirement(t *testing.T) {
	t.Run("state-only row retires tracking", testStatusRetirePending)
	t.Run("already retired rows are reported converged", testStatusRetireDone)
}

func testStatusRetirePending(t *testing.T) {
	fx := newFixture(t)
	useRepository(t, fx, []string{"g1"})
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "old.conf", group: "g2", source: []byte("stale\n"), content: []byte("stale\n")}, nil),
	}}
	result, err := fx.service.Status(context.Background(), Request{Groups: []string{"g2"}})
	assertKind(t, err, failure.Difference)
	record := singleRecord(t, result)
	if record.Kind() != StatusKindRetired || record.Action() != "retire-state" {
		t.Fatalf("record = %+v", record)
	}
	if result.Retired() != 1 || result.Converged() {
		t.Fatalf("counts = %d/%d/%d converged=%v", result.Files(), result.Aliases(), result.Retired(), result.Converged())
	}
}

func testStatusRetireDone(t *testing.T) {
	fx := newFixture(t)
	useRepository(t, fx, []string{"g1"})
	row := fileRow(baselineInput{target: "old.conf", group: "g2", source: []byte("stale\n"), content: []byte("stale\n")}, nil)
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	row.Status = state.StatusRetired
	row.RetiredAt = &when
	fx.rows.rows = stateRows{files: []state.FileBaseline{row}}
	result, err := fx.service.Status(context.Background(), Request{Groups: []string{"g2"}})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	record := singleRecord(t, result)
	if record.Kind() != StatusKindRetired || record.Action() != "no-op" {
		t.Fatalf("record = %+v", record)
	}
	if !result.Converged() {
		t.Fatalf("converged = false, want true")
	}
}

func testStatusConvergence(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{
		"g1/g1-file.conf": "source g1\n",
		"g1/_routes.toml": routesTOM("g1-file.conf", "bin/tool"),
	})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "source g1\n")
	fixtureLink(t, filepath.Join(fx.home, "bin", "tool"), "../g1-file.conf")
	fx.rows.rows = stateRows{
		files: []state.FileBaseline{
			fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("source g1\n")}, nil),
		},
		aliases: []state.AliasBaseline{aliasRow("bin/tool", "g1-file.conf", "g1")},
	}
	result, err := fx.service.Status(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(result.Records()) != 0 || !result.Converged() {
		t.Fatalf("records = %d converged=%v, want converged", len(result.Records()), result.Converged())
	}
}

func testStatusErrors(t *testing.T) {
	t.Run("unknown group is invalid input", testStatusUnknownGroup)
	t.Run("state read failure is operational", testStatusStateRead)
	t.Run("drift returns partial records with the difference", testStatusPartialResult)
}

func testStatusUnknownGroup(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	_, err := fx.service.Status(context.Background(), Request{Groups: []string{"ghost"}})
	assertKind(t, err, failure.InvalidInput)
}

func testStatusStateRead(t *testing.T) {
	fx := newFixture(t)
	fx.rows.fail = errors.New("database closed")
	_, err := fx.service.Status(context.Background(), Request{})
	assertKind(t, err, failure.Operational)
}

func testStatusPartialResult(t *testing.T) {
	fx := newFixture(t)
	useFileRepository(t, fx, map[string]string{"g1/g1-file.conf": "source g1\n"})
	writeFile(t, filepath.Join(fx.home, "g1-file.conf"), "drifted\n")
	fx.rows.rows = stateRows{files: []state.FileBaseline{
		fileRow(baselineInput{target: "g1-file.conf", group: "g1", source: []byte("source g1\n"), content: []byte("applied\n")}, nil),
	}}
	result, err := fx.service.Status(context.Background(), Request{})
	assertKind(t, err, failure.Difference)
	if len(result.Records()) != 1 {
		t.Fatalf("partial records = %d, want 1", len(result.Records()))
	}
}

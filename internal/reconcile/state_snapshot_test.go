package reconcile

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/database"
)

func TestPersistedStateSnapshot(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"file rows round-trip", testStateSnapshotFileRows},
		{"alias rows", testStateSnapshotAliasRows},
		{"inactive platform rows", testStateSnapshotInactivePlatform},
		{"state-only group rows", testStateSnapshotStateOnly},
		{"dual-active corruption rejected", testStateSnapshotDualActive},
		{"invalid rows rejected", testStateSnapshotInvalidRows},
		{"records copy defensively", testStateSnapshotDefensiveCopy},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func convertRows(t *testing.T, rows StateRows) StateSnapshot {
	t.Helper()
	snapshot, err := NewStateSnapshot(rows)
	if err != nil {
		t.Fatalf("convert state rows: %v", err)
	}
	return snapshot
}

func fileRow(target, group, source string) state.FileBaseline {
	return state.FileBaseline{
		TargetPath:          target,
		GroupName:           group,
		SourcePath:          source,
		SourceKind:          deployment.FileOrdinary,
		Layer:               deployment.LayerBase,
		BaselineContentHash: deployment.Ordinary([]byte("baseline-" + target)),
		BaselineSourceHash:  deployment.Ordinary([]byte("storage-" + target)),
		Status:              state.StatusActive,
	}
}

func aliasRow(alias, canonical, group string) state.AliasBaseline {
	return state.AliasBaseline{
		AliasPath:           alias,
		CanonicalTargetPath: canonical,
		GroupName:           group,
		Layer:               state.LayerAll,
		Status:              state.StatusActive,
	}
}

func testStateSnapshotFileRows(t *testing.T) {
	fixture := database.New(t)
	root := filepath.Join(fixture.Root, "repo")
	if _, err := fixture.Store.UpsertFileBaseline(root, fixture.Home, fileRow(".config/app", "apps", "files/app")); err != nil {
		t.Fatalf("upsert file row: %v", err)
	}
	files, err := fixture.Store.FileBaselines(root, fixture.Home)
	if err != nil {
		t.Fatalf("read file rows: %v", err)
	}
	snapshot := convertRows(t, StateRows{RepositoryRoot: root, HomePath: fixture.Home, Files: files})
	records := snapshot.AllFiles()
	if len(records) != 1 || !records[0].Active() || records[0].RetiredAt() != nil {
		t.Fatal("store row must convert active with no retirement time")
	}
	record := records[0]
	if record.TargetPath() != ".config/app" || record.GroupName() != "apps" || record.SourcePath() != "files/app" {
		t.Fatal("record must echo target, group, and source paths")
	}
	if record.SourceKind() != deployment.FileOrdinary || record.Layer() != deployment.LayerBase {
		t.Fatal("record must echo kind and layer")
	}
}

func testStateSnapshotAliasRows(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	retired := aliasRow("bin/old", "files/old", "")
	retired.Status = state.StatusRetired
	retired.RetiredAt = &when
	rows := StateRows{
		RepositoryRoot: "/repo",
		HomePath:       "/home",
		Aliases:        []state.AliasBaseline{aliasRow("bin/cat", "files/cat", "apps"), retired},
	}
	aliases := convertRows(t, rows).AllAliases()
	if len(aliases) != 2 {
		t.Fatalf("alias records = %d, want 2", len(aliases))
	}
	if aliases[0].AliasPath() != "bin/cat" || aliases[0].CanonicalTargetPath() != "files/cat" ||
		aliases[0].GroupName() != "apps" || !aliases[0].Active() {
		t.Fatal("active alias record must echo path, payload, group, and status")
	}
	if aliases[0].Layer() != state.LayerAll {
		t.Fatal("active alias record must echo its layer")
	}
	if aliases[1].Active() || aliases[1].RetiredAt() == nil {
		t.Fatal("retired alias row must convert inactive with a retirement time")
	}
}

func testStateSnapshotInactivePlatform(t *testing.T) {
	file := fileRow(".config/darwin", "apps", "files/darwin")
	file.Layer = deployment.LayerDarwin
	alias := aliasRow("bin/darwin", "files/darwin", "")
	alias.Layer = state.LayerDarwin
	snapshot := convertRows(t, StateRows{
		RepositoryRoot: "/repo",
		HomePath:       "/home",
		Files:          []state.FileBaseline{file},
		Aliases:        []state.AliasBaseline{alias},
	})
	files := snapshot.AllFiles()
	aliases := snapshot.AllAliases()
	if len(files) != 1 || len(aliases) != 1 {
		t.Fatalf("records = %d files %d aliases, want one of each", len(files), len(aliases))
	}
	if files[0].Layer() != deployment.LayerDarwin || aliases[0].Layer() != state.LayerDarwin {
		t.Fatal("inactive-platform rows must convert with their layer intact")
	}
}

func testStateSnapshotStateOnly(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	file := fileRow(".config/gone", "gone", "files/gone")
	file.Status = state.StatusRetired
	file.RetiredAt = &when
	snapshot := convertRows(t, StateRows{
		RepositoryRoot: "/repo",
		HomePath:       "/home",
		Files:          []state.FileBaseline{file},
	})
	files := snapshot.AllFiles()
	if len(files) != 1 || files[0].Active() {
		t.Fatalf("state-only file records = %d active %v, want one inactive", len(files), files[0].Active())
	}
	if files[0].GroupName() != "gone" {
		t.Fatal("deleted-scope rows must stay visible with their group intact")
	}
	if files[0].BaselineContent() != deployment.Ordinary([]byte("baseline-.config/gone")) {
		t.Fatal("state-only records must retain their baseline digest")
	}
	if snapshot.RepositoryRoot() != "/repo" || snapshot.HomePath() != "/home" {
		t.Fatal("snapshot must carry the canonical repository pair")
	}
}

func testStateSnapshotDualActive(t *testing.T) {
	rows := StateRows{
		RepositoryRoot: "/repo",
		HomePath:       "/home",
		Files:          []state.FileBaseline{fileRow(".both", "", "files/both")},
		Aliases:        []state.AliasBaseline{aliasRow(".both", "files/both", "")},
	}
	if _, err := NewStateSnapshot(rows); err == nil {
		t.Fatal("snapshot must reject a path active in both representations")
	}
	rows.Aliases[0].Status = state.StatusRetired
	retiredAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rows.Aliases[0].RetiredAt = &retiredAt
	if _, err := NewStateSnapshot(rows); err != nil {
		t.Fatalf("retired alias alongside active file must convert: %v", err)
	}
}

func mustReject(t *testing.T, rows StateRows, name string) {
	t.Helper()
	if _, err := NewStateSnapshot(rows); err == nil {
		t.Fatalf("%s must be rejected", name)
	}
}

func testStateSnapshotInvalidRows(t *testing.T) {
	for _, row := range invalidFileVariants() {
		mustReject(t, StateRows{RepositoryRoot: "/repo", HomePath: "/home", Files: []state.FileBaseline{row}}, row.TargetPath)
	}
	for _, row := range invalidAliasVariants() {
		mustReject(t, StateRows{RepositoryRoot: "/repo", HomePath: "/home", Aliases: []state.AliasBaseline{row}}, row.AliasPath)
	}
}

func testStateSnapshotDefensiveCopy(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	file := fileRow(".config/app", "apps", "files/app")
	file.Status = state.StatusRetired
	file.RetiredAt = &when
	snapshot := convertRows(t, StateRows{
		RepositoryRoot: "/repo",
		HomePath:       "/home",
		Files:          []state.FileBaseline{file},
	})
	record := snapshot.AllFiles()[0]
	if !record.RetiredAt().Equal(when) {
		t.Fatal("converted record must clone the retirement time")
	}
	*file.RetiredAt = time.Time{}
	if record.RetiredAt().IsZero() {
		t.Fatal("mutating the source row must not reach the converted record")
	}
	retiredAt := record.RetiredAt()
	*retiredAt = time.Time{}
	if snapshot.AllFiles()[0].RetiredAt().IsZero() {
		t.Fatal("mutating a read copy must not reach the snapshot")
	}
}

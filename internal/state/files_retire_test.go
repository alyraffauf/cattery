package state

import (
	"fmt"
	"os"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func testFileSecretBaseline(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	baseline := secretBaseline(".secret", "secrets", 0x22)
	stored, err := store.UpsertFileBaseline(root, home, baseline)
	requireNoError(t, err)
	if stored.SourceKind != deployment.FileSecret || stored.ExecutableBits != 0o600 {
		t.Fatal("secret row lost kind or mode")
	}
	_, err = os.Lstat(keyPathFor(t, storeDependenciesFor(store)))
	requireNoError(t, err)
	_, err = store.HashKeyID()
	requireNoError(t, err)
	if count := rowCount(t, store.Database().conn, "metadata"); count != 1 {
		t.Fatalf("metadata rows = %d, want 1", count)
	}
}

func testFileSelectedRetirement(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".keep", fill: 0x31})
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".drop", fill: 0x32})
	if _, err := store.RetireFileBaseline(root, home, ".drop"); err != nil {
		t.Fatalf("RetireFileBaseline: %v", err)
	}
	all, err := store.FileBaselines(root, home)
	if err != nil {
		t.Fatalf("FileBaselines: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("rows = %d, want 2", len(all))
	}
	if all[0].Status != StatusRetired || all[0].TargetPath != ".drop" {
		t.Fatal("first row is not the retired .drop")
	}
	if all[1].Status != StatusActive || all[1].TargetPath != ".keep" {
		t.Fatal("second row is not the active .keep")
	}
}

func testFileRetirementRequiresRow(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	if _, err := store.RetireFileBaseline(root, home, ".missing"); err == nil {
		t.Fatal("retired a row that does not exist")
	}
	if count := rowCount(t, store.Database().conn, "files"); count != 0 {
		t.Fatalf("retirement created %d rows", count)
	}
}

func testFileReactivation(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".once", fill: 0x41})
	if _, err := store.RetireFileBaseline(root, home, ".once"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	restored, err := store.ReactivateFileBaseline(root, home, ".once")
	if err != nil {
		t.Fatalf("ReactivateFileBaseline: %v", err)
	}
	if restored.Status != StatusActive || restored.RetiredAt != nil {
		t.Fatal("reactivation left status or timestamp behind")
	}
}

func testFileDeterministicReads(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".z", fill: 0x91})
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".a", group: "zeta", fill: 0x92})
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".m", group: "alpha", fill: 0x93})
	if _, err := store.RetireFileBaseline(root, home, ".a"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if groups, err := store.FileGroups(root, home); err != nil || len(groups) != 3 || groups[0] != "" || groups[1] != "alpha" || groups[2] != "zeta" {
		t.Fatalf("FileGroups = %v (%v), want ['', alpha, zeta]", groups, err)
	}
	if active, err := store.ActiveFileGroups(root, home); err != nil || len(active) != 2 || active[0] != "" || active[1] != "alpha" {
		t.Fatalf("ActiveFileGroups = %v (%v), want ['', alpha]", active, err)
	}
}

func testFileRollback(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	execOn(t, store.Database().conn, "CREATE TRIGGER files_abort AFTER INSERT ON files BEGIN SELECT RAISE(ABORT, 'boom'); END")
	if _, err := store.UpsertFileBaseline(root, home, ordinaryBaseline(".x", "", 0x51)); err == nil {
		t.Fatal("upsert succeeded against an aborting trigger")
	}
	if count := rowCount(t, store.Database().conn, "files"); count != 0 {
		t.Fatalf("trigger left %d file rows", count)
	}
	if count := rowCount(t, store.Database().conn, "repositories"); count != 0 {
		t.Fatalf("trigger left %d repository rows", count)
	}
}

func testFileDualActiveCorruption(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".both", fill: 0x61})
	execOn(t, store.Database().conn, fmt.Sprintf(
		"INSERT INTO aliases (repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at) SELECT id, '.both', '.both', '', 'all', 'active', '2026-01-02T03:04:05Z' FROM repositories WHERE root_path = '%s' AND home_path = '%s'",
		root, home))
	if _, err := store.FileBaselines(root, home); err == nil {
		t.Fatal("snapshot accepted a dual-active path")
	}
	if _, err := store.ActiveFileBaselines(root, home); err == nil {
		t.Fatal("active snapshot accepted a dual-active path")
	}
}

func testFileNoPlaintextColumn(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	var count int64
	if err := store.Database().conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('files')").Scan(&count); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 12 {
		t.Fatalf("files table has %d columns, want 12", count)
	}
}

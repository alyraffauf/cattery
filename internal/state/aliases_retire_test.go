package state

import (
	"fmt"
	"testing"
)

func testAliasSelectedRetirement(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".keep", canonical: ".config/keep"})
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".drop", canonical: ".config/drop"})
	if _, err := store.RetireAliasBaseline(root, home, ".drop"); err != nil {
		t.Fatalf("RetireAliasBaseline: %v", err)
	}
	all, err := store.AliasBaselines(root, home)
	requireNoError(t, err)
	if len(all) != 2 {
		t.Fatalf("rows = %d, want 2", len(all))
	}
	if all[0].Status != StatusRetired || all[0].AliasPath != ".drop" {
		t.Fatal("first row is not the retired .drop")
	}
	if all[0].RetiredAt == nil {
		t.Fatal("retired row must carry a retirement timestamp")
	}
	if all[1].Status != StatusActive || all[1].AliasPath != ".keep" {
		t.Fatal("second row is not the active .keep")
	}
}

func testAliasRetirementRequiresRow(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	if _, err := store.RetireAliasBaseline(root, home, ".missing"); err == nil {
		t.Fatal("retired a row that does not exist")
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 0 {
		t.Fatalf("retirement created %d rows", count)
	}
}

func testAliasRollback(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	execOn(t, store.Database().conn, "CREATE TRIGGER aliases_abort AFTER INSERT ON aliases BEGIN SELECT RAISE(ABORT, 'boom'); END")
	if _, err := store.UpsertAliasBaseline(root, home, aliasBaseline(".x", "canonical/.x", "")); err == nil {
		t.Fatal("upsert succeeded against an aborting trigger")
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 0 {
		t.Fatalf("trigger left %d alias rows", count)
	}
	if count := rowCount(t, store.Database().conn, "repositories"); count != 0 {
		t.Fatalf("trigger left %d repository rows", count)
	}
}

func testAliasDualActiveCorruption(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".both", canonical: ".config/both"})
	execOn(t, store.Database().conn, fmt.Sprintf(
		"INSERT INTO files (repository_id, target_path, source_path, source_kind, layer, baseline_content_hash, baseline_source_hash, executable_bits, status, applied_at) SELECT id, '.both', 'files/.both', 'ordinary', 'base', zeroblob(32), zeroblob(32), 0, 'active', '2026-01-02T03:04:05Z' FROM repositories WHERE root_path = '%s' AND home_path = '%s'",
		root, home))
	if _, err := store.AliasBaselines(root, home); err == nil {
		t.Fatal("alias snapshot accepted a dual-active path")
	}
}

func testAliasColumnCount(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	var count int64
	if err := store.Database().conn.QueryRow("SELECT COUNT(*) FROM pragma_table_info('aliases')").Scan(&count); err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 8 {
		t.Fatalf("aliases table has %d columns, want 8", count)
	}
}

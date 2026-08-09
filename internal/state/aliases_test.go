package state

import (
	"fmt"
	"testing"
	"time"
)

func TestAliasRows(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"realization stores the exact payload", testAliasRealizes},
		{"realization registers the repository pair", testAliasRegistersPair},
		{"realization rejects malformed rows", testAliasValidationRejects},
		{"realization at the same path updates the row", testAliasUpdatesRow},
		{"retirement keeps the payload for diagnostics", testAliasRetirement},
		{"retirement requires an existing row", testAliasRetirementRequiresRow},
		{"reactivation restores a retired row", testAliasReactivation},
		{"reads are deterministic and scopes are state-only", testAliasDeterministicReads},
		{"rollback discards the whole transaction", testAliasRollback},
		{"dual-active paths fail the snapshot", testAliasDualActiveCorruption},
		{"no duplicate active alias paths", testAliasNoDuplicateActives},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// aliasBaseline builds a valid realized alias row.
func aliasBaseline(aliasPath, canonical, group string) AliasBaseline {
	return AliasBaseline{
		AliasPath:           aliasPath,
		CanonicalTargetPath: canonical,
		GroupName:           group,
		Layer:               LayerAll,
	}
}

// aliasSpec names the arguments of one alias seed operation.
type aliasSpec struct {
	root, home, alias, canonical, group string
}

// seedAlias realizes one alias for the pair.
func seedAlias(t *testing.T, store *Store, spec aliasSpec) {
	t.Helper()
	if _, err := store.UpsertAliasBaseline(spec.root, spec.home, aliasBaseline(spec.alias, spec.canonical, spec.group)); err != nil {
		t.Fatalf("seed alias %q: %v", spec.alias, err)
	}
}

func testAliasRealizes(t *testing.T) {
	clock := &pinnedClock{now: time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)}
	store := openStore(t, Dependencies{StateHome: t.TempDir(), Clock: clock})
	root := t.TempDir()
	home := t.TempDir()
	stored, err := store.UpsertAliasBaseline(root, home, aliasBaseline(".app", ".config/app", "apps"))
	if err != nil {
		t.Fatalf("UpsertAliasBaseline: %v", err)
	}
	if stored.AliasPath != ".app" || stored.CanonicalTargetPath != ".config/app" || stored.GroupName != "apps" || stored.Layer != LayerAll {
		t.Fatalf("stored = %+v, want the exact realized payload", stored)
	}
	if stored.Status != StatusActive {
		t.Fatalf("status = %q, want active", stored.Status)
	}
	if !stored.AppliedAt.Equal(clock.now) {
		t.Fatalf("applied_at = %v, want %v", stored.AppliedAt, clock.now)
	}
}

func testAliasRegistersPair(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".app", canonical: ".config/app", group: "apps"})
	if _, err := store.LookupRepository(root, home); err != nil {
		t.Fatalf("pair not registered: %v", err)
	}
}

func testAliasValidationRejects(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	aliases := []AliasBaseline{
		{AliasPath: ""},
		{AliasPath: "/abs"},
		{AliasPath: `a\b`},
		{AliasPath: ".a", CanonicalTargetPath: "/abs"},
		{AliasPath: ".a", CanonicalTargetPath: ".a"},
		{AliasPath: ".a", CanonicalTargetPath: ".b", Layer: "nope"},
	}
	for _, alias := range aliases {
		if _, err := store.UpsertAliasBaseline(root, home, alias); err == nil {
			t.Fatalf("accepted invalid alias %+v", alias)
		}
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 0 {
		t.Fatalf("validation stored %d rows", count)
	}
}

func testAliasUpdatesRow(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".app", canonical: ".config/app", group: "apps"})
	if _, err := store.UpsertAliasBaseline(root, home, aliasBaseline(".app", ".config/app-new", "apps")); err != nil {
		t.Fatalf("second realization: %v", err)
	}
	rows, err := store.AliasBaselines(root, home)
	if err != nil {
		t.Fatalf("AliasBaselines: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].CanonicalTargetPath != ".config/app-new" || rows[0].Status != StatusActive {
		t.Fatal("row did not keep the new payload as a single active alias")
	}
}

func testAliasRetirement(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".app", canonical: ".config/app", group: "apps"})
	retired, err := store.RetireAliasBaseline(root, home, ".app")
	if err != nil {
		t.Fatalf("RetireAliasBaseline: %v", err)
	}
	if retired.Status != StatusRetired || retired.RetiredAt == nil {
		t.Fatal("retired row lost status or timestamp")
	}
	if retired.CanonicalTargetPath != ".config/app" {
		t.Fatal("retirement dropped the retained payload")
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

func testAliasReactivation(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".once", canonical: ".config/once", group: "apps"})
	if _, err := store.RetireAliasBaseline(root, home, ".once"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	restored, err := store.ReactivateAliasBaseline(root, home, ".once")
	if err != nil {
		t.Fatalf("ReactivateAliasBaseline: %v", err)
	}
	if restored.Status != StatusActive || restored.RetiredAt != nil {
		t.Fatal("reactivation left status or timestamp behind")
	}
}

func testAliasDeterministicReads(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".z", canonical: ".config/z", group: ""})
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".a", canonical: ".config/a", group: "zeta"})
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".m", canonical: ".config/m", group: "alpha"})
	if _, err := store.RetireAliasBaseline(root, home, ".a"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if groups, err := store.AliasGroups(root, home); err != nil || len(groups) != 3 || groups[0] != "" || groups[1] != "alpha" || groups[2] != "zeta" {
		t.Fatalf("AliasGroups = %v (%v), want ['', alpha, zeta]", groups, err)
	}
	if active, err := store.ActiveAliasGroups(root, home); err != nil || len(active) != 2 || active[0] != "" || active[1] != "alpha" {
		t.Fatalf("ActiveAliasGroups = %v (%v), want ['', alpha]", active, err)
	}
}

func testAliasRollback(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	execOn(t, store.Database().conn, "CREATE TRIGGER aliases_abort AFTER INSERT ON aliases BEGIN SELECT RAISE(ABORT, 'boom'); END")
	if _, err := store.UpsertAliasBaseline(root, home, aliasBaseline(".x", ".config/x", "")); err == nil {
		t.Fatal("realization succeeded against an aborting trigger")
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
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".both", canonical: ".config/both", group: ""})
	execOn(t, store.Database().conn, fmt.Sprintf(
		"INSERT INTO files (repository_id, target_path, group_name, source_path, source_kind, layer, baseline_content_hash, baseline_source_hash, executable_bits, status, applied_at) SELECT id, '.both', '', 'files/.both', 'ordinary', 'base', X'0101010101010101010101010101010101010101010101010101010101010101', X'0202020202020202020202020202020202020202020202020202020202020202', 420, 'active', '2026-01-02T03:04:05Z' FROM repositories WHERE root_path = '%s' AND home_path = '%s'",
		root, home))
	if _, err := store.AliasBaselines(root, home); err == nil {
		t.Fatal("snapshot accepted a dual-active path")
	}
	if _, err := store.FileBaselines(root, home); err == nil {
		t.Fatal("file snapshot accepted a dual-active path")
	}
}

func testAliasNoDuplicateActives(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".app", canonical: ".config/app", group: "apps"})
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".app", canonical: ".config/app", group: "apps"})
	if count := rowCount(t, store.Database().conn, "aliases"); count != 1 {
		t.Fatalf("aliases rows = %d, want 1", count)
	}
	active, err := store.ActiveAliasBaselines(root, home)
	if err != nil {
		t.Fatalf("ActiveAliasBaselines: %v", err)
	}
	if len(active) != 1 || active[0].AliasPath != ".app" {
		t.Fatal("duplicate realizations produced more than one active row")
	}
}

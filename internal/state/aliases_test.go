package state

import (
	"testing"
	"time"
)

func TestAliasRows(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"upsert creates an active alias row", testAliasUpsertCreatesRow},
		{"exact payloads round-trip", testAliasExactPayload},
		{"upsert registers the repository pair", testAliasUpsertRegistersPair},
		{"cross-scope ownership keeps one row", testAliasCrossScopeOwnership},
		{"baseline validation rejects malformed rows", testAliasValidationRejects},
		{"selected retirement marks only the chosen row", testAliasSelectedRetirement},
		{"retirement requires an existing row", testAliasRetirementRequiresRow},
		{"reactivation restores a retired row", testAliasReactivation},
		{"reads are deterministic and scopes are state-only", testAliasDeterministicReads},
		{"rollback discards the whole transaction", testAliasRollback},
		{"dual-active corruption fails the snapshot", testAliasDualActiveCorruption},
		{"no plaintext column exists", testAliasColumnCount},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// aliasBaseline builds a valid alias baseline with the exact payload.
func aliasBaseline(aliasPath, canonicalTargetPath, group string) AliasBaseline {
	return AliasBaseline{
		AliasPath:           aliasPath,
		CanonicalTargetPath: canonicalTargetPath,
		GroupName:           group,
		Layer:               LayerAll,
	}
}

// aliasSpec names the files an alias seed operation baselines for a pair.
type aliasSpec struct {
	root, home, alias, canonical, group string
}

// seedAlias baselines one alias for the pair.
func seedAlias(t *testing.T, store *Store, spec aliasSpec) {
	t.Helper()
	if _, err := store.UpsertAliasBaseline(spec.root, spec.home, aliasBaseline(spec.alias, spec.canonical, spec.group)); err != nil {
		t.Fatalf("seed %q: %v", spec.alias, err)
	}
}

func testAliasUpsertCreatesRow(t *testing.T) {
	clock := &pinnedClock{now: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	store := openStore(t, Dependencies{StateHome: t.TempDir(), Now: clock.Now})
	root := t.TempDir()
	home := t.TempDir()
	baseline := aliasBaseline(".config/app", "canonical/.config/app", "conf")
	stored, err := store.UpsertAliasBaseline(root, home, baseline)
	requireNoError(t, err)
	if stored.AliasPath != baseline.AliasPath || stored.CanonicalTargetPath != baseline.CanonicalTargetPath {
		t.Fatalf("stored = %q/%q, want %q/%q", stored.AliasPath, stored.CanonicalTargetPath, baseline.AliasPath, baseline.CanonicalTargetPath)
	}
	if stored.Status != StatusActive || stored.Layer != LayerAll {
		t.Fatalf("status = %q, layer = %q", stored.Status, stored.Layer)
	}
	if !stored.AppliedAt.Equal(clock.now) {
		t.Fatalf("applied_at = %v, want %v", stored.AppliedAt, clock.now)
	}
	if stored.RetiredAt != nil {
		t.Fatal("active row must carry a nil retirement timestamp")
	}
}

func testAliasExactPayload(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	baseline := aliasBaseline(".linked", "canonical/.linked", "platform")
	baseline.Layer = LayerDarwin
	stored, err := store.UpsertAliasBaseline(root, home, baseline)
	requireNoError(t, err)
	if stored.CanonicalTargetPath != "canonical/.linked" || stored.GroupName != "platform" || stored.Layer != LayerDarwin {
		t.Fatalf("payload = %+v, want exact", stored)
	}
	read, err := store.AliasBaseline(root, home, ".linked")
	requireNoError(t, err)
	if read != stored {
		t.Fatalf("round trip changed the row: %+v", read)
	}
}

func testAliasUpsertRegistersPair(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".x", canonical: ".config/x"})
	if _, err := store.LookupRepository(root, home); err != nil {
		t.Fatalf("pair not registered: %v", err)
	}
}

func testAliasCrossScopeOwnership(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	first := aliasBaseline(".x", "canonical/.x", "old")
	if _, err := store.UpsertAliasBaseline(root, home, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := aliasBaseline(".x", "canonical/.moved", "new")
	if _, err := store.UpsertAliasBaseline(root, home, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	rows, err := store.AliasBaselines(root, home)
	requireNoError(t, err)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].GroupName != "new" || rows[0].CanonicalTargetPath != "canonical/.moved" {
		t.Fatal("row did not move scope with new payload")
	}
}

func testAliasValidationRejects(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	baselines := []AliasBaseline{
		{AliasPath: ""},
		{AliasPath: "/abs"},
		{AliasPath: `a\b`},
		{AliasPath: ".x", CanonicalTargetPath: "/abs"},
		{AliasPath: ".x", CanonicalTargetPath: "t", GroupName: "a/b"},
		{AliasPath: ".x", CanonicalTargetPath: "t", GroupName: "_private"},
		{AliasPath: ".x", CanonicalTargetPath: "t", Layer: "nope"},
	}
	for _, baseline := range baselines {
		if _, err := store.UpsertAliasBaseline(root, home, baseline); err == nil {
			t.Fatalf("accepted invalid baseline %+v", baseline)
		}
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 0 {
		t.Fatalf("validation stored %d rows", count)
	}
}

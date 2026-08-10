package state

import (
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestFileRows(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"upsert creates an active baseline row", testFileUpsertCreatesRow},
		{"upsert registers the repository pair", testFileUpsertRegistersPair},
		{"cross-scope ownership keeps one row", testFileCrossScopeOwnership},
		{"baseline validation rejects malformed rows", testFileValidationRejects},
		{"secret baselines create the key and commit its identifier", testFileSecretBaseline},
		{"selected retirement marks only the chosen row", testFileSelectedRetirement},
		{"retirement requires an existing row", testFileRetirementRequiresRow},
		{"rollback discards the whole transaction", testFileRollback},
		{"dual-active corruption fails the snapshot", testFileDualActiveCorruption},
		{"no plaintext column exists", testFileNoPlaintextColumn},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// sampleDigest returns a deterministic 32-byte digest filled with fill.
func sampleDigest(fill byte) deployment.Digest {
	var digest deployment.Digest
	for index := range digest {
		digest[index] = fill
	}
	return digest
}

// ordinaryBaseline builds a valid ordinary baseline for the target.
func ordinaryBaseline(target, group string, fill byte) FileBaseline {
	return FileBaseline{
		TargetPath:          target,
		GroupName:           group,
		SourcePath:          "files/" + target,
		SourceKind:          deployment.FileOrdinary,
		Layer:               deployment.LayerBase,
		BaselineContentHash: sampleDigest(fill),
		BaselineSourceHash:  sampleDigest(fill + 1),
		ExecutableBits:      0o755,
	}
}

// secretBaseline builds a valid secret baseline with the exact secret mode.
func secretBaseline(target, group string, fill byte) FileBaseline {
	baseline := ordinaryBaseline(target, group, fill)
	baseline.SourceKind = deployment.FileSecret
	baseline.ExecutableBits = 0o600
	return baseline
}

// seedSpec names the files a seed operation baselines for a pair.
type seedSpec struct {
	root, home, target, group string
	fill                      byte
}

// seedOrdinary baselines one ordinary file for the pair.
func seedOrdinary(t *testing.T, store *Store, spec seedSpec) {
	t.Helper()
	if _, err := store.UpsertFileBaseline(spec.root, spec.home, ordinaryBaseline(spec.target, spec.group, spec.fill)); err != nil {
		t.Fatalf("seed %q: %v", spec.target, err)
	}
}

func testFileUpsertCreatesRow(t *testing.T) {
	clock := &pinnedClock{now: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	store := openStore(t, Dependencies{StateHome: t.TempDir(), Now: clock.Now})
	root := t.TempDir()
	home := t.TempDir()
	baseline := ordinaryBaseline(".config/app/config", "", 0x11)
	stored, err := store.UpsertFileBaseline(root, home, baseline)
	if err != nil {
		t.Fatalf("UpsertFileBaseline: %v", err)
	}
	if stored.TargetPath != baseline.TargetPath || stored.SourcePath != baseline.SourcePath {
		t.Fatalf("stored = %q/%q, want %q/%q", stored.TargetPath, stored.SourcePath, baseline.TargetPath, baseline.SourcePath)
	}
	if stored.Status != StatusActive {
		t.Fatalf("status = %q, want active", stored.Status)
	}
	if !stored.AppliedAt.Equal(clock.now) {
		t.Fatalf("applied_at = %v, want %v", stored.AppliedAt, clock.now)
	}
	if stored.BaselineContentHash != baseline.BaselineContentHash || stored.BaselineSourceHash != baseline.BaselineSourceHash {
		t.Fatal("digests changed in storage")
	}
}

func testFileUpsertRegistersPair(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".x", fill: 0x71})
	if _, err := store.LookupRepository(root, home); err != nil {
		t.Fatalf("pair not registered: %v", err)
	}
}

func testFileCrossScopeOwnership(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	if _, err := store.UpsertFileBaseline(root, home, ordinaryBaseline(".x", "old", 0x81)); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if _, err := store.UpsertFileBaseline(root, home, ordinaryBaseline(".x", "new", 0x82)); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	rows, err := store.FileBaselines(root, home)
	if err != nil {
		t.Fatalf("FileBaselines: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].GroupName != "new" || rows[0].BaselineContentHash != ordinaryBaseline(".x", "new", 0x82).BaselineContentHash {
		t.Fatal("row did not move scope with new content")
	}
}

func testFileValidationRejects(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	baselines := []FileBaseline{
		{TargetPath: ""},
		{TargetPath: "/abs"},
		{TargetPath: `a\b`},
		{TargetPath: ".x", SourcePath: "/abs"},
		{TargetPath: ".x", SourcePath: "s", SourceKind: "nope"},
		{TargetPath: ".x", SourcePath: "s", SourceKind: deployment.FileOrdinary, Layer: "nope"},
		{TargetPath: ".x", SourcePath: "s", SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase},
		{TargetPath: ".x", SourcePath: "s", SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase, BaselineContentHash: sampleDigest(1)},
		{TargetPath: ".x", SourcePath: "s", SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase, BaselineContentHash: sampleDigest(1), BaselineSourceHash: sampleDigest(2), ExecutableBits: 0o1000},
		{TargetPath: ".x", SourcePath: "s", GroupName: "_private", SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase, BaselineContentHash: sampleDigest(1), BaselineSourceHash: sampleDigest(2)},
		{TargetPath: ".x", SourcePath: "s", GroupName: "a/b", SourceKind: deployment.FileOrdinary, Layer: deployment.LayerBase, BaselineContentHash: sampleDigest(1), BaselineSourceHash: sampleDigest(2)},
	}
	for _, baseline := range baselines {
		if _, err := store.UpsertFileBaseline(root, home, baseline); err == nil {
			t.Fatalf("accepted invalid baseline %+v", baseline)
		}
	}
	if count := rowCount(t, store.Database().conn, "files"); count != 0 {
		t.Fatalf("validation stored %d rows", count)
	}
}

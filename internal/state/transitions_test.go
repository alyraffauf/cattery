package state

import (
	"fmt"
	"os"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestRepresentationTransition(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"file-to-alias activates the alias and retires the file", testTransitionToAlias},
		{"alias-to-file activates the file and retires the alias", testTransitionToFile},
		{"rollback preserves the prior active row", testTransitionRollback},
		{"transition with no active old row is skipped cleanly", testTransitionSkipped},
		{"pre-existing dual-active corruption fails", testTransitionDualActive},
		{"secret files transition with their identifier", testTransitionSecretFile},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testTransitionToAlias(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".x", fill: 0x11})
	aliasRow, err := store.TransitionToAlias(root, home, aliasBaseline(".x", ".config/x", "apps"))
	if err != nil {
		t.Fatalf("TransitionToAlias: %v", err)
	}
	if aliasRow.Status != StatusActive || aliasRow.CanonicalTargetPath != ".config/x" {
		t.Fatal("alias row is not active with the new payload")
	}
	fileRows, err := store.FileBaselines(root, home)
	if err != nil {
		t.Fatalf("FileBaselines: %v", err)
	}
	if len(fileRows) != 1 || fileRows[0].Status != StatusRetired {
		t.Fatal("file row did not retire")
	}
}

func testTransitionToFile(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".x", canonical: ".config/x", group: "apps"})
	fileRow, err := store.TransitionToFile(root, home, ordinaryBaseline(".x", "apps", 0x22))
	if err != nil {
		t.Fatalf("TransitionToFile: %v", err)
	}
	if fileRow.Status != StatusActive || fileRow.BaselineContentHash != ordinaryBaseline(".x", "apps", 0x22).BaselineContentHash {
		t.Fatal("file row is not active with the new baseline")
	}
	aliasRows, err := store.AliasBaselines(root, home)
	if err != nil {
		t.Fatalf("AliasBaselines: %v", err)
	}
	if len(aliasRows) != 1 || aliasRows[0].Status != StatusRetired {
		t.Fatal("alias row did not retire")
	}
}

func testTransitionRollback(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".x", fill: 0x11})
	execOn(t, store.Database().conn, "CREATE TRIGGER aliases_abort AFTER INSERT ON aliases BEGIN SELECT RAISE(ABORT, 'boom'); END")
	if _, err := store.TransitionToAlias(root, home, aliasBaseline(".x", ".config/x", "apps")); err == nil {
		t.Fatal("transition succeeded against an aborting trigger")
	}
	rows, err := store.FileBaselines(root, home)
	if err != nil {
		t.Fatalf("FileBaselines: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != StatusActive {
		t.Fatal("prior file row was not preserved")
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 0 {
		t.Fatalf("trigger left %d alias rows", count)
	}
}

func testTransitionSkipped(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".x", fill: 0x11})
	if _, err := store.RetireFileBaseline(root, home, ".x"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if _, err := store.TransitionToAlias(root, home, aliasBaseline(".x", ".config/x", "apps")); err == nil {
		t.Fatal("transition skipped on a retired file row")
	}
	rows, err := store.FileBaselines(root, home)
	if err != nil {
		t.Fatalf("FileBaselines: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != StatusRetired {
		t.Fatal("skipped transition changed the file row")
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 0 {
		t.Fatalf("skipped transition created %d alias rows", count)
	}
}

func testTransitionDualActive(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedOrdinary(t, store, seedSpec{root: root, home: home, target: ".both", fill: 0x61})
	execOn(t, store.Database().conn, fmt.Sprintf(
		"INSERT INTO aliases (repository_id, alias_path, canonical_target_path, group_name, layer, status, applied_at) SELECT id, '.both', '.config/both', '', 'all', 'active', '2026-01-02T03:04:05Z' FROM repositories WHERE root_path = '%s' AND home_path = '%s'",
		root, home))
	if _, err := store.TransitionToAlias(root, home, aliasBaseline(".both", ".config/both", "")); err == nil {
		t.Fatal("transition accepted a dual-active path")
	}
	if count := rowCount(t, store.Database().conn, "aliases"); count != 1 {
		t.Fatalf("aliases rows = %d, want 1", count)
	}
	stored, err := store.FileBaseline(root, home, ".both")
	if err != nil {
		t.Fatalf("FileBaseline: %v", err)
	}
	if stored.Status != StatusActive {
		t.Fatal("dual-active failure changed the file row")
	}
}

func testTransitionSecretFile(t *testing.T) {
	store := openStore(t, tempDependencies(t))
	root := t.TempDir()
	home := t.TempDir()
	seedAlias(t, store, aliasSpec{root: root, home: home, alias: ".key", canonical: ".config/key", group: "secrets"})
	fileRow, err := store.TransitionToFile(root, home, secretBaseline(".key", "secrets", 0x33))
	if err != nil {
		t.Fatalf("TransitionToFile: %v", err)
	}
	if fileRow.SourceKind != deployment.FileSecret || fileRow.ExecutableBits != 0o600 {
		t.Fatal("secret row lost kind or mode")
	}
	if _, err := os.Lstat(keyPathFor(t, storeDependenciesFor(store))); err != nil {
		t.Fatalf("transition did not create hash.key: %v", err)
	}
	if _, err := store.HashKeyID(); err != nil {
		t.Fatalf("transition did not commit the identifier: %v", err)
	}
}

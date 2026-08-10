package add

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func TestAddOrdinaryWrite(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"writes new source with content", testWriteNewSource},
		{"preserves existing source mode", testWritePreservesMode},
		{"records executable bits", testWriteExecutableBits},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testWriteNewSource(t *testing.T) {
	state := newWriteStage(t, ordinaryTarget{relative: ".bashrc", content: []byte("shell"), mode: 0o600})
	outcome, err := state.service.writeOrdinary(context.Background(), state.identity, state.item)
	if err != nil {
		t.Fatal(err)
	}
	assertSourceContent(t, state.repoSource(), []byte("shell"))
	if outcome.result.Renamed != true {
		t.Fatal("write did not publish the source")
	}
}

func testWritePreservesMode(t *testing.T) {
	target := ordinaryTarget{relative: ".bashrc", content: []byte("new"), mode: 0o600}
	state := newWriteStage(t, target)
	if err := os.WriteFile(state.repoSource(), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := state.service.writeOrdinary(context.Background(), state.identity, state.item); err != nil {
		t.Fatal(err)
	}
	assertSourceMode(t, state.repoSource(), 0o600)
}

func testWriteExecutableBits(t *testing.T) {
	target := ordinaryTarget{relative: "bin/tool", content: []byte("tool"), mode: 0o755}
	state := newWriteStage(t, target)
	if _, err := state.service.writeOrdinary(context.Background(), state.identity, state.item); err != nil {
		t.Fatal(err)
	}
	assertSourceMode(t, state.repoSource(), 0o755)
}

// ordinaryTarget describes the target file materialized beneath home.
type ordinaryTarget struct {
	relative string
	content  []byte
	mode     os.FileMode
}

// writeStage bundles the service, identity, item, and source path of one
// ordinary-write test.
type writeStage struct {
	service  *Service
	identity RepositoryIdentity
	item     ItemPlan
	repo     string
}

func newWriteStage(t *testing.T, target ordinaryTarget) writeStage {
	t.Helper()
	home := t.TempDir()
	repo := t.TempDir()
	if err := testfs.New(home).File(target.relative, target.content, target.mode).Materialize(); err != nil {
		t.Fatal(err)
	}
	item, err := NewItemPlan(ItemPlanInput{
		Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		TargetAbsolutePath:   filepath.Join(home, target.relative),
		TargetRelativePath:   target.relative,
		SourceRepositoryPath: target.relative,
		SourceAbsolutePath:   filepath.Join(repo, target.relative),
		ExecutableBits:       target.mode & deployment.ExecutableBitMask,
	})
	if err != nil {
		t.Fatal(err)
	}
	return writeStage{
		service:  NewService(Dependencies{Writer: filesystem.NewReplacer()}),
		identity: RepositoryIdentity{Root: repo, Home: home},
		item:     item, repo: repo,
	}
}

func (stage writeStage) repoSource() string {
	return filepath.Join(stage.identity.Root, stage.item.SourceRepositoryPath())
}

func assertSourceContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("source content = %q, want %q", got, want)
	}
}

func assertSourceMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("source mode = %v, want %v", info.Mode().Perm(), want)
	}
}

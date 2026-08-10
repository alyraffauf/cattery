package add

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
)

func TestAddBatchPreflight(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"resolves relative argument", testResolveRelative},
		{"expands directory argument", testResolveDirectory},
		{"rejects target outside home", testResolveOutsideHome},
		{"rejects home itself", testResolveHomeItself},
		{"rejects duplicate canonical path", testResolveDuplicate},
		{"fills executable bits", testPreflightExecutableBits},
		{"rejects directory target", testPreflightRejectsDirectory},
		{"rejects same object via hard link", testPreflightRejectsSameObject},
		{"rejects plan-owned source", testPreflightRejectsPlanCollision},
		{"rejects shared batch source", testPreflightRejectsBatchCollision},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testResolveRelative(t *testing.T) {
	home := materializeHome(t, dotFile)
	canonical, err := resolveTargets(home, home, []string{".bashrc"})
	if err != nil {
		t.Fatal(err)
	}
	if canonical[0] != filepath.Join(home, ".bashrc") {
		t.Fatalf("canonical = %q, want %q", canonical[0], filepath.Join(home, ".bashrc"))
	}
}

func testResolveDirectory(t *testing.T) {
	home := materializeHome(t, directoryEntry)
	targets, err := resolveTargets(home, home, []string{"subdir"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(home, "subdir", "inside")}
	if len(targets) != len(want) || targets[0] != want[0] {
		t.Fatalf("targets = %q, want %q", targets, want)
	}
}

func testResolveOutsideHome(t *testing.T) {
	home := materializeHome(t, dotFile)
	if _, err := resolveTargets(home, home, []string{filepath.Join(home, "..", "escape")}); err == nil {
		t.Fatal("target outside home was accepted")
	}
}

func testResolveHomeItself(t *testing.T) {
	home := materializeHome(t, dotFile)
	if _, err := resolveTargets(home, home, []string{"."}); err == nil {
		t.Fatal("home itself was accepted")
	}
}

func testResolveDuplicate(t *testing.T) {
	home := materializeHome(t, dotFile)
	duplicate := filepath.Join(home, ".bashrc")
	if _, err := resolveTargets(home, home, []string{duplicate, duplicate}); err == nil {
		t.Fatal("duplicate canonical target was accepted")
	}
}

func testPreflightExecutableBits(t *testing.T) {
	home := materializeHome(t, executableFile)
	context := preflightContext{identity: RepositoryIdentity{Root: t.TempDir(), Home: home}, plan: planWith(t, nil, nil)}
	items := []ItemPlanInput{preflightItem(home, "bin/tool")}
	validated, err := Preflight(context, items)
	if err != nil {
		t.Fatal(err)
	}
	if validated[0].ExecutableBits != deployment.ExecutableBitMask {
		t.Fatalf("executable bits = %v, want %v", validated[0].ExecutableBits, deployment.ExecutableBitMask)
	}
}

func testPreflightRejectsDirectory(t *testing.T) {
	home := materializeHome(t, directoryEntry)
	context := preflightContext{identity: RepositoryIdentity{Root: t.TempDir(), Home: home}, plan: planWith(t, nil, nil)}
	items := []ItemPlanInput{preflightItem(home, "subdir")}
	if _, err := Preflight(context, items); err == nil {
		t.Fatal("directory target was accepted")
	}
}

func testPreflightRejectsSameObject(t *testing.T) {
	home := materializeHome(t, hardLinkedFiles)
	context := preflightContext{identity: RepositoryIdentity{Root: t.TempDir(), Home: home}, plan: planWith(t, nil, nil)}
	items := []ItemPlanInput{
		preflightItem(home, "original.txt"),
		{Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
			TargetAbsolutePath: filepath.Join(home, "link.txt"), TargetRelativePath: "link.txt",
			SourceRepositoryPath: "link.txt", SourceAbsolutePath: "/repo/link.txt"},
	}
	if _, err := Preflight(context, items); err == nil {
		t.Fatal("two arguments naming one object were accepted")
	}
}

func testPreflightRejectsPlanCollision(t *testing.T) {
	home := materializeHome(t, dotFile)
	owner := deployment.NewScope("")
	owned, err := deployment.NewManagedFile(deployment.ManagedFile{
		Scope: owner, Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		SourceAbsolutePath: "/repo/owned.txt", SourceRepositoryPath: "owned.txt",
		TargetRelativePath: "owned.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	item := preflightItem(home, ".bashrc")
	item.SourceRepositoryPath = "owned.txt"
	context := preflightContext{identity: RepositoryIdentity{Root: t.TempDir(), Home: home}, plan: planWith(t, []deployment.ManagedFile{owned}, nil)}
	if _, err := Preflight(context, []ItemPlanInput{item}); err == nil {
		t.Fatal("plan-owned source was accepted")
	}
}

func testPreflightRejectsBatchCollision(t *testing.T) {
	home := materializeHome(t, twoDotFiles)
	context := preflightContext{identity: RepositoryIdentity{Root: t.TempDir(), Home: home}, plan: planWith(t, nil, nil)}
	first := preflightItem(home, ".bashrc")
	second := preflightItem(home, ".vimrc")
	second.SourceRepositoryPath = first.SourceRepositoryPath
	if _, err := Preflight(context, []ItemPlanInput{first, second}); err == nil {
		t.Fatal("shared batch source was accepted")
	}
}

// preflightItem builds a minimal root-base-ordinary item for one target.
func preflightItem(home, relative string) ItemPlanInput {
	return ItemPlanInput{
		Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		TargetAbsolutePath:   filepath.Join(home, relative),
		TargetRelativePath:   relative,
		SourceRepositoryPath: relative,
		SourceAbsolutePath:   filepath.Join("/repo", relative),
	}
}

// fixtureEntry materializes one entry beneath a fresh home tree.
type fixtureEntry struct {
	path    string
	content []byte
	mode    uint32
}

var (
	dotFile         = fixtureEntry{path: ".bashrc", content: []byte("shell"), mode: 0o600}
	executableFile  = fixtureEntry{path: "bin/tool", content: []byte("tool"), mode: 0o755}
	directoryEntry  = fixtureEntry{path: "subdir/inside", content: []byte("x"), mode: 0o600}
	hardLinkedFiles = fixtureEntry{path: "original.txt", content: []byte("shared"), mode: 0o600}
	twoDotFiles     = fixtureEntry{path: ".vimrc", content: []byte("vim"), mode: 0o600}
)

// materializeHome creates a fresh home directory with one fixture entry and
// returns its path.
func materializeHome(t *testing.T, entry fixtureEntry) string {
	t.Helper()
	home := t.TempDir()
	builder := testfs.New(home).File(entry.path, entry.content, os.FileMode(entry.mode))
	if entry.path == "original.txt" {
		builder = builder.HardLink("link.txt", "original.txt")
	}
	if err := builder.Materialize(); err != nil {
		t.Fatal(err)
	}
	return home
}

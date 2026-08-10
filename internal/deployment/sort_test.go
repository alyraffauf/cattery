package deployment

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestDeploymentOrdering(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"empty inputs are safe", testSortEmpty},
		{"managed file total order", testManagedFileOrder},
		{"managed file tie breakers", testManagedFileTieBreakers},
		{"alias bytewise order", testAliasOrder},
		{"both hook phases", testHookPhaseOrder},
		{"file permutations converge", testFilePermutations},
		{"alias permutations converge", testAliasPermutations},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSortEmpty(t *testing.T) {
	SortFiles(nil)
	SortAliases(nil)
	SortHooks(nil)
}

func testManagedFileOrder(t *testing.T) {
	files := []ManagedFile{
		{TargetRelativePath: ".config/zsh/.zshrc", Layer: LayerBase, Kind: FileOrdinary},
		{TargetRelativePath: ".config/atuin/config.toml", Layer: LayerLinux, Kind: FileOrdinary},
		{TargetRelativePath: ".config/atuin/config.toml", Layer: LayerBase, Kind: FileSecret},
		{TargetRelativePath: ".config/atuin/config.toml", Layer: LayerBase, Kind: FileOrdinary},
	}
	SortFiles(files)
	if files[0].Kind != FileOrdinary || files[0].Layer != LayerBase {
		t.Fatalf("first must be atuin base ordinary: %v/%v", files[0].Layer, files[0].Kind)
	}
	if files[1].Kind != FileSecret || files[1].Layer != LayerBase {
		t.Fatalf("second must be atuin base secret: %v/%v", files[1].Layer, files[1].Kind)
	}
	if files[2].Layer != LayerLinux {
		t.Fatalf("third must be atuin linux: %v", files[2].Layer)
	}
	if files[3].TargetRelativePath != ".config/zsh/.zshrc" {
		t.Fatalf("last must be zshrc: %q", files[3].TargetRelativePath)
	}
}

func testManagedFileTieBreakers(t *testing.T) {
	files := []ManagedFile{
		{TargetRelativePath: "same", Layer: LayerBase, Kind: FileOrdinary, Scope: NewScope("z"), SourceRepositoryPath: "z", SourceAbsolutePath: "/z"},
		{TargetRelativePath: "same", Layer: LayerBase, Kind: FileOrdinary, Scope: NewScope("a"), SourceRepositoryPath: "a", SourceAbsolutePath: "/a"},
		{TargetRelativePath: "same", Layer: LayerBase, Kind: FileOrdinary, Scope: NewScope("a"), SourceRepositoryPath: "b", SourceAbsolutePath: "/b"},
		{TargetRelativePath: "same", Layer: LayerBase, Kind: FileOrdinary, Scope: NewScope("a"), SourceRepositoryPath: "b", SourceAbsolutePath: "/a", SourceExecutableBits: 0o111},
	}
	SortFiles(files)
	if files[0].Scope.Group != "a" || files[0].SourceRepositoryPath != "a" {
		t.Fatalf("scope and repository path tie breakers failed: %+v", files)
	}
	if files[1].SourceAbsolutePath != "/a" || files[1].SourceExecutableBits != 0o111 {
		t.Fatalf("executable bits tie breaker failed: %+v", files)
	}
	if files[2].Scope.Group != "a" || files[2].SourceRepositoryPath != "b" || files[2].SourceAbsolutePath != "/b" {
		t.Fatalf("absolute path tie breaker failed: %+v", files)
	}
	if files[3].Scope.Group != "z" {
		t.Fatalf("scope must be the first tie breaker: %+v", files)
	}
}

func testAliasOrder(t *testing.T) {
	aliases := []Alias{
		{AliasRelativePath: ".config/zzz/alias"},
		{AliasRelativePath: ".config/aaa/alias"},
		{AliasRelativePath: ".config/aaa/early"},
	}
	SortAliases(aliases)
	if aliases[0].AliasRelativePath != ".config/aaa/alias" {
		t.Fatalf("first = %q", aliases[0].AliasRelativePath)
	}
	if aliases[1].AliasRelativePath != ".config/aaa/early" {
		t.Fatalf("second = %q", aliases[1].AliasRelativePath)
	}
	if aliases[2].AliasRelativePath != ".config/zzz/alias" {
		t.Fatalf("last = %q", aliases[2].AliasRelativePath)
	}
}

func testHookPhaseOrder(t *testing.T) {
	hooks := []Hook{
		{Phase: HookBefore, Name: "z", Scope: NewScope("")},
		{Phase: HookAfter, Name: "a", Scope: NewScope("")},
		{Phase: HookBefore, Name: "a", Scope: NewScope("")},
	}
	SortHooks(hooks)
	if hooks[0].Phase != HookAfter {
		t.Fatalf("'after' sorts before 'before' bytewise: %v", hooks[0].Phase)
	}
	if hooks[1].Phase != HookBefore || hooks[1].Scope.Group != "" {
		t.Fatalf("before/root second: phase=%v group=%q", hooks[1].Phase, hooks[1].Scope.Group)
	}
	if hooks[2].Name != "z" {
		t.Fatalf("'z' must sort last: %q", hooks[2].Name)
	}
}

func testFilePermutations(t *testing.T) {
	base := []ManagedFile{
		{TargetRelativePath: "a", Layer: LayerBase, Kind: FileOrdinary},
		{TargetRelativePath: "b", Layer: LayerBase, Kind: FileOrdinary},
		{TargetRelativePath: "c", Layer: LayerLinux, Kind: FileSecret},
		{TargetRelativePath: "d", Layer: LayerDarwin, Kind: FileOrdinary},
		{TargetRelativePath: "e", Layer: LayerBase, Kind: FileSecret},
	}
	canonical := copyFiles(base)
	SortFiles(canonical)
	rng := rand.New(rand.NewPCG(7, 99))
	for trial := 0; trial < 50; trial++ {
		shuffled := copyFiles(base)
		rng.Shuffle(len(shuffled), swapFiles(shuffled))
		SortFiles(shuffled)
		if !slices.Equal(shuffled, canonical) {
			t.Fatalf("trial %d drifted from canonical order", trial)
		}
	}
}

func testAliasPermutations(t *testing.T) {
	base := []Alias{
		{AliasRelativePath: "alpha"},
		{AliasRelativePath: "beta"},
		{AliasRelativePath: "gamma"},
		{AliasRelativePath: "delta"},
	}
	canonical := copyAliases(base)
	SortAliases(canonical)
	rng := rand.New(rand.NewPCG(11, 13))
	for trial := 0; trial < 50; trial++ {
		shuffled := copyAliases(base)
		rng.Shuffle(len(shuffled), swapAliases(shuffled))
		SortAliases(shuffled)
		if !slices.Equal(shuffled, canonical) {
			t.Fatalf("trial %d drifted from canonical order", trial)
		}
	}
}

func swapFiles(items []ManagedFile) func(int, int) {
	return func(i, j int) {
		items[i], items[j] = items[j], items[i]
	}
}

func swapAliases(items []Alias) func(int, int) {
	return func(i, j int) {
		items[i], items[j] = items[j], items[i]
	}
}

package reconcile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/state"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSnapshotAssembly(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"deterministic joins", testSnapshotDeterministicJoins},
		{"missing producers", testSnapshotMissingProducers},
		{"representation pairs", testSnapshotRepresentationPairs},
		{"defensive copies", testSnapshotDefensiveCopies},
		{"secret plaintext cleared", testSnapshotSecretPlaintext},
		{"invalid plans rejected", testSnapshotRejected},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSnapshotDeterministicJoins(t *testing.T) {
	repo, home := fixtureDir(t)
	files := []deployment.ManagedFile{planFile(t, repo, "a.conf"), planFile(t, repo, "c")}
	aliases := []deployment.Alias{{Platform: "linux", AliasRelativePath: "bin/z", CanonicalTargetRelativePath: "files/z"}}
	mustTargetFile(t, filepath.Join(home, "a.conf"), []byte("current a"))
	fixtureLink(t, filepath.Join(home, "bin", "z"), "files/z")
	state := sampleState(t, repo, StateRows{Files: []state.FileBaseline{fileRow("a.conf", "apps", "a.conf"), fileRow("c", "apps", "c")},
		Aliases: []state.AliasBaseline{aliasRow("bin/z", "files/z", "apps")}})
	records := mustAssemble(t, samplePlan(repo, []deployment.ManagedFile{files[1], files[0]}, aliases),
		StateSnapshot{repositoryRoot: state.RepositoryRoot(), homePath: state.HomePath(),
			files: []FileState{state.AllFiles()[1], state.AllFiles()[0]}, aliases: state.AllAliases()}).All()
	if len(records) != 3 || records[0].TargetPath > records[1].TargetPath || records[1].TargetPath > records[2].TargetPath {
		t.Fatal("records must be bytewise sorted in path order")
	}
	requireFileJoin(t, findRecord(t, records, "a.conf"), "a.conf")
}
func testSnapshotMissingProducers(t *testing.T) {
	repo, home := fixtureDir(t)
	mustTargetFile(t, filepath.Join(home, "gone"), []byte("stale target"))
	retired := fileRow("gone", "apps", "files/gone")
	retired.Status = state.StatusRetired
	retired.RetiredAt = ptrTimestamp()
	alias := aliasRow("bin/old", "files/old", "apps")
	alias.Status = state.StatusRetired
	alias.RetiredAt = ptrTimestamp()
	state := sampleState(t, repo, StateRows{Files: []state.FileBaseline{retired}, Aliases: []state.AliasBaseline{alias}})
	records := mustAssemble(t, samplePlan(repo, nil, nil), state).All()
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	gone := findRecord(t, records, "gone")
	requireRetiredJoin(t, gone)
	requireRetiredJoin(t, findRecord(t, records, "bin/old"))
	if gone.Target.Kind() != KindFile {
		t.Fatal("state-only file must join its current target")
	}
}
func testSnapshotRepresentationPairs(t *testing.T) {
	repo, home := fixtureDir(t)
	fileToAlias := fileRow("bin/tool", "apps", "files/tool")
	fileToAliasPair := aliasRow("bin/tool", "files/tool", "apps")
	fileToAliasPair.Status = state.StatusRetired
	fileToAliasPair.RetiredAt = ptrTimestamp()
	aliasToFile := fileRow("conf/app", "apps", "files/app")
	aliasToFile.Status = state.StatusRetired
	aliasToFile.RetiredAt = ptrTimestamp()
	aliasToFilePair := aliasRow("conf/app", "files/app", "apps")
	plan := samplePlan(repo, []deployment.ManagedFile{planFile(t, repo, "conf/app")},
		[]deployment.Alias{{Platform: "linux", AliasRelativePath: "bin/tool", CanonicalTargetRelativePath: "files/tool"}})
	if err := os.MkdirAll(filepath.Join(home, "conf"), 0o700); err != nil {
		t.Fatalf("mkdir conf: %v", err)
	}
	mustTargetFile(t, filepath.Join(home, "conf", "app"), []byte("current app"))
	fixtureLink(t, filepath.Join(home, "bin", "tool"), "files/tool")
	state := sampleState(t, repo, StateRows{Files: []state.FileBaseline{fileToAlias, aliasToFile},
		Aliases: []state.AliasBaseline{fileToAliasPair, aliasToFilePair}})
	records := mustAssemble(t, plan, state).All()
	requireTransition(t, findRecord(t, records, "bin/tool"), "files/tool")
	requireTransition(t, findRecord(t, records, "conf/app"), "conf/app")
}
func testSnapshotDefensiveCopies(t *testing.T) {
	repo, home := fixtureDir(t)
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mustTargetFile(t, filepath.Join(home, "a.conf"), []byte("current a"))
	row := fileRow("a.conf", "apps", "a.conf")
	row.Status = state.StatusRetired
	row.RetiredAt = &when
	plan := samplePlan(repo, []deployment.ManagedFile{planFile(t, repo, "a.conf")}, nil)
	state := sampleState(t, repo, StateRows{Files: []state.FileBaseline{row}})
	snapshot := mustAssemble(t, plan, state)
	files := plan.Files()
	files[0].TargetRelativePath = "mutated"
	stateFiles := state.AllFiles()
	stateFiles[0].baselineContent = deployment.Digest{}
	record := findRecord(t, snapshot.All(), "a.conf")
	retiredAt := record.FileState.RetiredAt()
	*retiredAt = time.Time{}
	*row.RetiredAt = time.Time{}
	fresh := findRecord(t, snapshot.All(), "a.conf")
	if fresh.FileState.RetiredAt() == nil || fresh.FileState.RetiredAt().IsZero() || fresh.FileState.BaselineContent() == (deployment.Digest{}) {
		t.Fatal("mutating inputs or read copies must not reach the snapshot")
	}
}
func testSnapshotSecretPlaintext(t *testing.T) {
	repo, _ := fixtureDir(t)
	path := filepath.Join(repo, "app", "token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	writeSource(t, path, secretJSON())
	client, record := fakeClient(t, sops.Behavior{Stdout: []byte("plaintext")})
	plan := samplePlan(repo, []deployment.ManagedFile{managedSource(path, "app/token", deployment.FileSecret)}, nil)
	snapshot, err := Assemble(plan, sampleState(t, repo, StateRows{}), client)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if recordRun(record) {
		t.Fatal("assembly must never decrypt secret sources")
	}
	token := findRecord(t, snapshot.All(), "target")
	if token.Entry != PlanEntryFile || token.Target.Kind() != KindAbsent || string(token.Source.Bytes()) != string(secretJSON()) {
		t.Fatal("the snapshot must retain ciphertext only, never plaintext")
	}
}

func testSnapshotRejected(t *testing.T) {
	repo, _ := fixtureDir(t)
	file := planFile(t, repo, "a.conf")
	state := sampleState(t, repo, StateRows{})
	other := samplePlan(filepath.Join(filepath.Dir(repo), "other"), []deployment.ManagedFile{file}, nil)
	noHome := StateSnapshot{repositoryRoot: repo, files: []FileState{{targetPath: "a.conf"}}}
	duplicate := mustPlan(deployment.PlanInput{RepositoryRoot: repo, Platform: "linux", Files: []deployment.ManagedFile{file, file}})
	collision := mustPlan(deployment.PlanInput{RepositoryRoot: repo, Platform: "linux", Files: []deployment.ManagedFile{file},
		Aliases: []deployment.Alias{{Platform: "linux", AliasRelativePath: "a.conf", CanonicalTargetRelativePath: "files/a.conf"}}})
	duplicateAlias := mustPlan(deployment.PlanInput{RepositoryRoot: repo, Platform: "linux",
		Aliases: []deployment.Alias{{Platform: "linux", AliasRelativePath: "bin/x", CanonicalTargetRelativePath: "files/x"},
			{Platform: "linux", AliasRelativePath: "bin/x", CanonicalTargetRelativePath: "files/y"}}})
	cases := []struct {
		name  string
		plan  deployment.Plan
		state StateSnapshot
	}{
		{"other repository", other, state},
		{"no platform", deployment.Plan{}, state},
		{"unset home", samplePlan(repo, []deployment.ManagedFile{file}, nil), noHome},
		{"duplicate files", duplicate, state},
		{"file and alias collision", collision, state},
		{"duplicate aliases", duplicateAlias, state},
	}
	for _, scenario := range cases {
		if _, err := Assemble(scenario.plan, scenario.state, nil); err == nil {
			t.Fatalf("assembly must reject a %s", scenario.name)
		}
	}
}

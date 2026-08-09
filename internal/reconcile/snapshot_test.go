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

func mustAssemble(t *testing.T, plan deployment.Plan, state StateSnapshot) EvaluationSnapshot {
	t.Helper()
	snapshot, err := Assemble(plan, state, nil)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	return snapshot
}
func fixtureDir(t *testing.T) (repo, home string) {
	t.Helper()
	home = t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "repo"), 0o700); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	return filepath.Join(home, "repo"), home
}
func planFile(t *testing.T, repo, target string) deployment.ManagedFile {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	writeSource(t, path, []byte("source "+target))
	return deployment.ManagedFile{Scope: deployment.Scope{Group: "apps"}, Layer: deployment.LayerBase, Kind: deployment.FileOrdinary,
		SourceAbsolutePath: path, SourceRepositoryPath: target, TargetRelativePath: target}
}
func samplePlan(repo string, files []deployment.ManagedFile, aliases []deployment.Alias) deployment.Plan {
	return deployment.NewPlan(deployment.Plan{RepositoryRoot: repo, Platform: "linux", Files: files, Aliases: aliases})
}
func sampleState(t *testing.T, repo string, rows StateRows) StateSnapshot {
	t.Helper()
	rows.RepositoryRoot, rows.HomePath = repo, filepath.Dir(repo)
	return convertRows(t, rows)
}
func findRecord(t *testing.T, records []Evaluation, path string) Evaluation {
	t.Helper()
	for _, record := range records {
		if record.TargetPath == path {
			return record
		}
	}
	t.Fatalf("no record for %q", path)
	return Evaluation{}
}
func fixtureLink(t *testing.T, path, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.Symlink(link, path); err != nil {
		t.Fatalf("symlink %s: %v", path, err)
	}
}
func requireFileJoin(t *testing.T, record Evaluation, target string) {
	t.Helper()
	if record.Entry != PlanEntryFile || record.File.TargetRelativePath != target || record.FileState == nil || !record.FileState.Active {
		t.Fatalf("record %s must join its file descriptor and row", target)
	}
	if record.Target.Kind() != KindFile || record.Source.Snapshot().Token() != TokenOfContent([]byte("source "+target)) {
		t.Fatalf("record %s must join target and source observations", target)
	}
}
func requireRetiredJoin(t *testing.T, record Evaluation) {
	t.Helper()
	if record.Entry != PlanEntryNone || record.Source.Snapshot().Path() != "" {
		t.Fatalf("record %s must carry no producer or source", record.TargetPath)
	}
	if record.FileState != nil {
		if record.FileState.Active {
			t.Fatalf("record %s must join its retired file row", record.TargetPath)
		}
		return
	}
	if record.AliasState == nil || record.AliasState.Active {
		t.Fatalf("record %s must join its retired alias row", record.TargetPath)
	}
}
func requireTransition(t *testing.T, record Evaluation, canonical string) {
	t.Helper()
	if record.FileState == nil || record.AliasState == nil || record.FileState.Active == record.AliasState.Active {
		t.Fatalf("record %s must join exactly one active representation row", record.TargetPath)
	}
	if record.Entry == PlanEntryAlias {
		if record.Alias.CanonicalTargetRelativePath != canonical || record.Target.Kind() != KindSymlink || record.Target.Payload() != canonical {
			t.Fatalf("record %s must join its alias entry and symlink", record.TargetPath)
		}
		return
	}
	if record.File.TargetRelativePath != record.TargetPath || record.Target.Kind() != KindFile {
		t.Fatalf("record %s must join its file entry and observation", record.TargetPath)
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
		StateSnapshot{RepositoryRoot: state.RepositoryRoot, HomePath: state.HomePath,
			Files: []FileState{state.Files[1], state.Files[0]}, Aliases: state.Aliases}).All()
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
	alias := aliasRow("bin/old", "files/old", "apps")
	alias.Status = state.StatusRetired
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
	aliasToFile := fileRow("conf/app", "apps", "files/app")
	aliasToFile.Status = state.StatusRetired
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
	plan.Files[0].TargetRelativePath = "mutated"
	state.Files[0].BaselineContent = deployment.Digest{}
	record := findRecord(t, snapshot.All(), "a.conf")
	*record.FileState.RetiredAt = time.Time{}
	*row.RetiredAt = time.Time{}
	fresh := findRecord(t, snapshot.All(), "a.conf")
	if fresh.FileState.RetiredAt == nil || fresh.FileState.RetiredAt.IsZero() || fresh.FileState.BaselineContent == (deployment.Digest{}) {
		t.Fatal("mutating inputs or read copies must not reach the snapshot")
	}
}
func testSnapshotSecretPlaintext(t *testing.T) {
	repo, _ := fixtureDir(t)
	path := filepath.Join(repo, "app", "token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	writeSource(t, path, secretEnvelope("c2Vrcml0"))
	client, record := sopsClient(t, sops.Behavior{Stdout: []byte("plaintext")}, repo)
	plan := samplePlan(repo, []deployment.ManagedFile{sourceFile(path, "app/token", deployment.FileSecret)}, nil)
	snapshot, err := Assemble(plan, sampleState(t, repo, StateRows{}), client)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if recordRun(record) {
		t.Fatal("assembly must never decrypt secret sources")
	}
	token := findRecord(t, snapshot.All(), "target")
	if token.Entry != PlanEntryFile || token.Target.Kind() != KindAbsent || string(token.Source.Bytes()) != string(secretEnvelope("c2Vrcml0")) {
		t.Fatal("the snapshot must retain ciphertext only, never plaintext")
	}
}

func testSnapshotRejected(t *testing.T) {
	repo, _ := fixtureDir(t)
	file := planFile(t, repo, "a.conf")
	state := sampleState(t, repo, StateRows{})
	other := samplePlan(filepath.Join(filepath.Dir(repo), "other"), []deployment.ManagedFile{file}, nil)
	noHome := StateSnapshot{RepositoryRoot: repo, Files: []FileState{{TargetPath: "a.conf"}}}
	duplicate := deployment.NewPlan(deployment.Plan{RepositoryRoot: repo, Platform: "linux", Files: []deployment.ManagedFile{file, file}})
	collision := deployment.NewPlan(deployment.Plan{RepositoryRoot: repo, Platform: "linux", Files: []deployment.ManagedFile{file},
		Aliases: []deployment.Alias{{Platform: "linux", AliasRelativePath: "a.conf", CanonicalTargetRelativePath: "files/a.conf"}}})
	duplicateAlias := deployment.NewPlan(deployment.Plan{RepositoryRoot: repo, Platform: "linux",
		Aliases: []deployment.Alias{{Platform: "linux", AliasRelativePath: "bin/x", CanonicalTargetRelativePath: "files/x"},
			{Platform: "linux", AliasRelativePath: "bin/x", CanonicalTargetRelativePath: "files/y"}}})
	cases := []struct {
		name  string
		plan  deployment.Plan
		state StateSnapshot
	}{
		{"other repository", other, state},
		{"no platform", deployment.NewPlan(deployment.Plan{RepositoryRoot: repo}), state},
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

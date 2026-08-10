package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

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
	plan, err := deployment.NewPlan(deployment.PlanInput{RepositoryRoot: repo, Platform: "linux", Files: files, Aliases: aliases})
	if err != nil {
		panic(err)
	}
	return plan
}

func mustPlan(input deployment.PlanInput) deployment.Plan {
	plan, err := deployment.NewPlan(input)
	if err != nil {
		panic(err)
	}
	return plan
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
	if record.Entry != PlanEntryFile || record.File.TargetRelativePath != target || record.FileState == nil || !record.FileState.Active() {
		t.Fatalf("record %s must join its file descriptor and row", target)
	}
	if record.Target.Kind() != KindFile {
		t.Fatalf("record %s must join target and source observations", target)
	}
}

func requireRetiredJoin(t *testing.T, record Evaluation) {
	t.Helper()
	if record.Entry != PlanEntryNone || record.Source.Snapshot().Path() != "" {
		t.Fatalf("record %s must carry no producer or source", record.TargetPath)
	}
	if record.FileState != nil {
		if record.FileState.Active() {
			t.Fatalf("record %s must join its retired file row", record.TargetPath)
		}
		return
	}
	if record.AliasState == nil || record.AliasState.Active() {
		t.Fatalf("record %s must join its retired alias row", record.TargetPath)
	}
}

func requireTransition(t *testing.T, record Evaluation, canonical string) {
	t.Helper()
	if record.FileState == nil || record.AliasState == nil || record.FileState.Active() == record.AliasState.Active() {
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

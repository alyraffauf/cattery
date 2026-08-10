package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/state"
)

func TestApplyVerification(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"source drift", testVerifySourceDrift},
		{"target drift", testVerifyTargetDrift},
		{"alias drift", testVerifyAliasDrift},
		{"no-write baselines", testVerifyNoWrites},
		{"sops rechecks", testVerifySecret},
		{"state commits stay silent", testVerifyStateSilent},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// verifyFixture executes one write and returns the records and seams.
func verifyFixture(t *testing.T) (hookPair, []ItemResult) {
	t.Helper()
	pair := hookFixture(t, false)
	records, err := pair.service.RunHookPipeline(context.Background(), PipelineInput{Request: Request{}, Plan: pair.plan, Candidates: pair.candidates})
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	return pair, records
}

// verifyRecords re-runs verification over the executed records.
func verifyRecords(t *testing.T, pair verifyPair) []ItemResult {
	t.Helper()
	verified, err := pair.service.Verify(context.Background(), pair.records, pair.candidates)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return verified
}

// verifyPair bundles the executed records with the verification seams.
type verifyPair struct {
	service    *Service
	records    []ItemResult
	candidates Candidates
	home       string
}

func testVerifySourceDrift(t *testing.T) {
	pair, records := verifyFixture(t)
	if err := os.WriteFile(verifySourcePath(pair), []byte("source changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	verified := verifyRecords(t, verifyPair{service: pair.service, records: records, candidates: pair.candidates})
	if verified[0].Status != StatusPartial {
		t.Fatalf("verified = %+v, want partial after source drift", verified)
	}
}

// verifySourcePath resolves the source path of the first candidate.
func verifySourcePath(pair hookPair) string {
	return pair.candidates.All()[0].record.File.SourceAbsolutePath
}

func testVerifyTargetDrift(t *testing.T) {
	pair, records := verifyFixture(t)
	if err := os.WriteFile(targetPath(pair.home, "a.conf"), []byte("hook rewrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	verified := verifyRecords(t, verifyPair{service: pair.service, records: records, candidates: pair.candidates})
	if verified[0].Status != StatusPartial {
		t.Fatalf("verified = %+v, want partial after target drift", verified)
	}
}

func testVerifyAliasDrift(t *testing.T) {
	pair := aliasVerifyFixture(t)
	if err := os.Remove(targetPath(pair.home, "bin/tool")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("wrong", targetPath(pair.home, "bin/tool")); err != nil {
		t.Fatal(err)
	}
	verified := verifyRecords(t, pair)
	if verified[0].Status != StatusPartial {
		t.Fatalf("verified = %+v, want partial after alias drift", verified)
	}
}

// aliasVerifyFixture executes one realized alias and returns the seams.
func aliasVerifyFixture(t *testing.T) verifyPair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	alias := toolAlias(t)
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalAliasPlan(t, repo, alias), transitions: &transitionFake{}, replacer: &replacerFake{}, baselines: &baselineFake{}})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	plan := PreparedPlan{actions: NewActionPlan([]PlanAction{{TargetPath: "bin/tool", Kind: ActionKindRealizeAlias}})}
	records, err := service.ExecuteAliases(context.Background(), plan, candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return verifyPair{service: service, records: records, candidates: candidates, home: home}
}

func testVerifyNoWrites(t *testing.T) {
	pair, records := verifyFixture(t)
	before := pair.replacer.calls + pair.baselines.calls
	if _, err := pair.service.Verify(context.Background(), records, pair.candidates); err != nil {
		t.Fatalf("verify: %v", err)
	}
	after := pair.replacer.calls + pair.baselines.calls
	if after != before {
		t.Fatalf("verification must not write, calls before = %d after = %d", before, after)
	}
}

func testVerifySecret(t *testing.T) {
	pair := secretVerifyFixture(t)
	verified := verifyRecords(t, pair)
	if verified[0].Status != StatusCompleted {
		t.Fatalf("verified = %+v, want completed after a sops recheck", verified)
	}
	if err := os.WriteFile(targetPath(pair.home, "target"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	verified = verifyRecords(t, pair)
	if verified[0].Status != StatusPartial {
		t.Fatalf("verified = %+v, want partial after secret tampering", verified)
	}
}

// secretVerifyFixture executes one secret write and returns the seams.
func secretVerifyFixture(t *testing.T) verifyPair {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	file := secretSource(t, fileSpec{Repo: repo, Target: "target", Relative: "files/token"})
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, file), client: sopsClient(t, []byte("plaintext")), baselines: &baselineFake{}, replacer: &replacerFake{}})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	plan := PreparedPlan{actions: NewActionPlan([]PlanAction{{TargetPath: "target", Kind: ActionKindWriteSource, SourcePath: "files/token"}})}
	records, err := service.ExecuteFiles(context.Background(), plan, candidates)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return verifyPair{service: service, records: records, candidates: candidates, home: home}
}

func testVerifyStateSilent(t *testing.T) {
	pair, records := verifyFixture(t)
	pair.baselines.calls = 0
	if _, err := pair.service.Verify(context.Background(), records, pair.candidates); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if pair.baselines.calls != 0 || pair.transitions.calls != 0 {
		t.Fatalf("verification must not commit state, baselines = %d transitions = %d", pair.baselines.calls, pair.transitions.calls)
	}
}

// verifyErrorKind asserts a verification failure category.
func verifyErrorKind(t *testing.T, err error, kind failure.Kind) {
	t.Helper()
	if err == nil || !kindIs(err, kind) {
		t.Fatalf("error = %v, want %s", err, kind)
	}
}

// secretRowFor freezes one active secret baseline over the envelope.
func secretRowFor(target string) state.FileBaseline {
	cipher := []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`)
	return state.FileBaseline{
		TargetPath: target, SourcePath: "files/token", SourceKind: deployment.FileSecret, Layer: deployment.LayerBase,
		BaselineContentHash: deployment.SecretSemantic([]byte("plaintext"), [32]byte{7}),
		BaselineSourceHash:  deployment.RawStorage(cipher),
		Status:              state.StatusActive,
	}
}

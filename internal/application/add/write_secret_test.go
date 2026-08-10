package add

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/secrets"
	testfs "github.com/alyraffauf/cattery/internal/testfixture/filesystem"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestAddSecretWrite(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"writes ciphertext source", testSecretWritesCiphertext},
		{"rejects mismatched candidate", testSecretRejectsMismatch},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSecretWritesCiphertext(t *testing.T) {
	stage := newSecretStage(t, true)
	outcome, err := stage.service.writeSecret(context.Background(), stage.identity, stage.item)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outcome.published, stage.plaintext) {
		t.Fatal("published ciphertext is not the validated candidate")
	}
	if outcome.result.Renamed != true {
		t.Fatal("secret source was not published")
	}
	got, err := os.ReadFile(stage.repoSource())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, stage.plaintext) {
		t.Fatalf("source = %q, want the validated ciphertext", got)
	}
}

func testSecretRejectsMismatch(t *testing.T) {
	stage := newSecretStage(t, false)
	outcome, err := stage.service.writeSecret(context.Background(), stage.identity, stage.item)
	if err == nil {
		t.Fatal("writeSecret adopted a mismatched candidate")
	}
	if outcome.published != nil {
		t.Fatal("failure returned a published buffer")
	}
}

// secretStage bundles the service, identity, item, plaintext, and source path
// of one secret-write test. The sops fake round-trips when plaintext is valid
// JSON: Encrypt returns it as the ciphertext and Decrypt returns it again, so
// ValidateCandidate adopts it byte-exact.
type secretStage struct {
	service   *Service
	identity  RepositoryIdentity
	item      ItemPlan
	plaintext []byte
	repo      string
}

func newSecretStage(t *testing.T, roundTrip bool) secretStage {
	t.Helper()
	home := t.TempDir()
	repo := t.TempDir()
	plaintext := []byte(`{"secret":"token-value"}`)
	relative := ".aws/creds"
	if err := testfs.New(home).File(relative, plaintext, 0o600).Materialize(); err != nil {
		t.Fatal(err)
	}
	sourcePath := "_secrets/" + relative
	item, err := NewItemPlan(ItemPlanInput{
		Layer: deployment.LayerBase, Kind: deployment.FileSecret,
		TargetAbsolutePath:   filepath.Join(home, relative),
		TargetRelativePath:   relative,
		SourceRepositoryPath: sourcePath,
		SourceAbsolutePath:   filepath.Join(repo, sourcePath),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := newFakeSopsClient(t, fakeSopsTarget{repo: repo, plaintext: plaintext, roundTrip: roundTrip})
	service := NewService(Dependencies{Writer: filesystem.NewReplacer(), Secrets: client})
	return secretStage{
		service: service, identity: RepositoryIdentity{Root: repo, Home: home},
		item: item, plaintext: plaintext, repo: repo,
	}
}

// fakeSopsTarget bundles the inputs of one fake sops client.
type fakeSopsTarget struct {
	repo      string
	plaintext []byte
	roundTrip bool
}

// newFakeSopsClient wires the test-only sops fake. A round-trip candidate
// echoes the JSON plaintext; a mismatch candidate returns a different
// plaintext so ValidateCandidate rejects it.
func newFakeSopsClient(t *testing.T, target fakeSopsTarget) *secrets.Client {
	t.Helper()
	executable := sops.Build(t)
	behavior := sops.Behavior{Stdout: target.plaintext}
	if !target.roundTrip {
		behavior = sops.Behavior{Stdout: []byte(`{"secret":"other-value"}`)}
	}
	cmd, err := executable.Command(behavior)
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewClient(executable.Path, target.repo, cmd.Env)
}

func (stage secretStage) repoSource() string {
	return filepath.Join(stage.identity.Root, stage.item.SourceRepositoryPath())
}

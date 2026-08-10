package secretlifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/selection"
)

func TestLifecycleSelectionIncludesEveryLayerAndUsesUnion(t *testing.T) {
	stage := newLifecycleStage(t)
	stage.write(t, "_secrets/root", "root")
	stage.write(t, "_linux/_secrets/linux", "linux")
	stage.write(t, "apps/_secrets/base", "base")
	stage.write(t, "apps/_darwin/_secrets/mac", "mac")
	stage.write(t, "tools/_secrets/tool", "tool")

	result, err := stage.service.List(context.Background(), Request{Groups: []string{"apps"}, Sources: []string{"_secrets/root"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := itemSources(result); strings.Join(got, ",") != "_secrets/root,apps/_darwin/_secrets/mac,apps/_secrets/base" {
		t.Fatalf("sources = %v", got)
	}
	if len(stage.secrets.decryptPaths) != 0 || len(stage.secrets.encryptPaths) != 0 {
		t.Fatal("list invoked SOPS")
	}
}

func TestLifecycleRejectsInvalidSelectors(t *testing.T) {
	stage := newLifecycleStage(t)
	stage.write(t, "apps/_secrets/token", "ciphertext")
	stage.write(t, "apps/config", "ordinary")
	cases := []Request{
		{Groups: []string{"missing"}},
		{Sources: []string{"apps/_secrets/token", "apps/_secrets/token"}},
		{Sources: []string{"apps/config"}},
		{Sources: []string{"missing/_secrets/token"}},
	}
	for _, request := range cases {
		if _, err := stage.service.List(context.Background(), request); !hasKind(err, failure.InvalidInput) {
			t.Fatalf("request %+v error = %v", request, err)
		}
	}
}

func TestVerifyContinuesAfterIndependentFailure(t *testing.T) {
	stage := newLifecycleStage(t)
	stage.write(t, "_secrets/bad", "bad")
	stage.write(t, "_secrets/good", "good")
	stage.secrets.failDecrypt["_secrets/bad"] = true

	result, err := stage.service.Verify(context.Background(), Request{})
	if err == nil || len(result.Items) != 2 {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	if result.Items[0].Status != "failed" || result.Items[1].Status != "verified" {
		t.Fatalf("statuses = %+v", result.Items)
	}
	if strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("error leaked plaintext: %v", err)
	}
}

func TestReencryptPreviewAndPublish(t *testing.T) {
	stage := newLifecycleStage(t)
	stage.writeMode(t, "apps/_secrets/token", "old", 0o740)

	preview, err := stage.service.Reencrypt(context.Background(), Request{})
	if !hasKind(err, failure.Difference) || preview.Items[0].Status != "planned" {
		t.Fatalf("preview = %+v, error = %v", preview, err)
	}
	stage.requireContent(t, "apps/_secrets/token", "old")

	result, err := stage.service.Reencrypt(context.Background(), Request{Yes: true})
	if err != nil || result.Items[0].Status != "reencrypted" {
		t.Fatalf("publish = %+v, error = %v", result, err)
	}
	stage.requireContent(t, "apps/_secrets/token", "new:plaintext")
	info, _ := os.Stat(filepath.Join(stage.root, "apps/_secrets/token"))
	if info.Mode().Perm() != 0o740 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if stage.baselines.source != "apps/_secrets/token" || stage.baselines.hash != deployment.RawStorage([]byte("new:plaintext")) {
		t.Fatalf("baseline refresh = %+v", stage.baselines)
	}
	if strings.Join(stage.secrets.encryptPaths, ",") != "apps/_secrets/token,apps/_secrets/token" {
		t.Fatalf("filename overrides = %v", stage.secrets.encryptPaths)
	}
}

func TestReencryptContinuesAndRejectsConflictingFlags(t *testing.T) {
	stage := newLifecycleStage(t)
	stage.write(t, "_secrets/bad", "bad")
	stage.write(t, "_secrets/good", "good")
	stage.secrets.failEncrypt["_secrets/bad"] = true

	result, err := stage.service.Reencrypt(context.Background(), Request{Yes: true})
	if err == nil || result.Items[0].Status != "failed" || result.Items[1].Status != "reencrypted" {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	stage.requireContent(t, "_secrets/bad", "bad")
	stage.requireContent(t, "_secrets/good", "new:plaintext")
	if _, err := stage.service.Reencrypt(context.Background(), Request{Yes: true, DryRun: true}); !hasKind(err, failure.InvalidInput) {
		t.Fatalf("flag error = %v", err)
	}
}

func TestReencryptRoundTripAndPublicationFailuresLeaveSource(t *testing.T) {
	stage := newLifecycleStage(t)
	stage.write(t, "_secrets/token", "old")
	stage.secrets.mismatchValidation = true
	result, err := stage.service.Reencrypt(context.Background(), Request{Yes: true})
	if err == nil || result.Items[0].Status != "failed" {
		t.Fatalf("round-trip result = %+v, error = %v", result, err)
	}
	stage.requireContent(t, "_secrets/token", "old")

	stage.secrets.mismatchValidation = false
	stage.service = NewService(Dependencies{
		RepositorySource: fixedRepository{identity: applicationrepository.RepositoryIdentity{Root: stage.root, Home: stage.home}},
		Secrets:          stage.secrets, Writer: failingWriter{}, Baselines: stage.baselines,
	})
	result, err = stage.service.Reencrypt(context.Background(), Request{Yes: true})
	if err == nil || result.Items[0].Status != "failed" {
		t.Fatalf("publication result = %+v, error = %v", result, err)
	}
	stage.requireContent(t, "_secrets/token", "old")
}

type lifecycleStage struct {
	root      string
	home      string
	service   *Service
	secrets   *fakeSecrets
	baselines *fakeBaselines
}

func newLifecycleStage(t *testing.T) *lifecycleStage {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	secretClient := &fakeSecrets{failDecrypt: map[string]bool{}, failEncrypt: map[string]bool{}}
	baselines := &fakeBaselines{}
	service := NewService(Dependencies{
		RepositorySource: fixedRepository{identity: applicationrepository.RepositoryIdentity{Root: root, Home: home}},
		Secrets:          secretClient, Writer: filesystem.NewReplacer(), Baselines: baselines,
	})
	return &lifecycleStage{root: root, home: home, service: service, secrets: secretClient, baselines: baselines}
}

func (stage *lifecycleStage) write(t *testing.T, relative, content string) {
	t.Helper()
	stage.writeMode(t, relative, content, 0o600)
}

func (stage *lifecycleStage) writeMode(t *testing.T, relative, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(stage.root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func (stage *lifecycleStage) requireContent(t *testing.T, relative, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(stage.root, filepath.FromSlash(relative)))
	if err != nil || string(content) != want {
		t.Fatalf("content = %q, %v; want %q", content, err, want)
	}
}

type fixedRepository struct {
	identity applicationrepository.RepositoryIdentity
}

func (source fixedRepository) Resolve(selection.RepositoryRequest) (applicationrepository.RepositoryIdentity, error) {
	return source.identity, nil
}

type fakeSecrets struct {
	failDecrypt        map[string]bool
	failEncrypt        map[string]bool
	decryptPaths       []string
	encryptPaths       []string
	mismatchValidation bool
}

func (client *fakeSecrets) SetDirectory(string) {}

func (client *fakeSecrets) Decrypt(_ context.Context, ciphertext []byte, path string) ([]byte, error) {
	client.decryptPaths = append(client.decryptPaths, path)
	if client.failDecrypt[path] {
		return nil, failure.New(failure.Operational, "sops decrypt "+path+" failed", nil)
	}
	if strings.HasPrefix(string(ciphertext), "new:") {
		if client.mismatchValidation {
			return []byte("different plaintext"), nil
		}
		return []byte(strings.TrimPrefix(string(ciphertext), "new:")), nil
	}
	return []byte("plaintext"), nil
}

type failingWriter struct{}

func (failingWriter) ReplaceResult(context.Context, filesystem.Precondition, filesystem.ReplacementSpec) (filesystem.ReplaceResult, error) {
	return filesystem.ReplaceResult{}, errors.New("injected publication failure")
}

func (client *fakeSecrets) Encrypt(_ context.Context, plaintext []byte, path string) ([]byte, error) {
	client.encryptPaths = append(client.encryptPaths, path)
	if client.failEncrypt[path] {
		return nil, failure.New(failure.Operational, "sops encrypt "+path+" failed", nil)
	}
	return append([]byte("new:"), plaintext...), nil
}

type fakeBaselines struct {
	source string
	hash   deployment.Digest
}

func (store *fakeBaselines) RefreshSecretSourceHash(_, _, source string, hash deployment.Digest) (bool, error) {
	store.source, store.hash = source, hash
	return true, nil
}

func itemSources(result Result) []string {
	paths := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		paths = append(paths, item.Source)
	}
	return paths
}

func hasKind(err error, want failure.Kind) bool {
	if err == nil {
		return false
	}
	kind, ok := failure.HasKind(err)
	return ok && kind == want
}

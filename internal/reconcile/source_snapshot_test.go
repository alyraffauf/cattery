package reconcile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSourceSnapshot(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"ordinary bytes", testSourceOrdinary},
		{"secret raw storage", testSourceSecret},
		{"malformed secret", testSourceMalformed},
		{"rejected type", testSourceRejectedType},
		{"identity replacement", testSourceReplacement},
		{"mode changes", testSourceModes},
		{"decrypt on demand", testSourceDecrypt},
		{"keyed ordinary rejected", testSourceKeyedOrdinary},
		{"buffer clearing", testSourceClear},
	}
	for _, test := range tests {
		t.Run(test.name, test.run)
	}
}

func managedSource(path, relative string, kind deployment.FileKind) deployment.ManagedFile {
	return deployment.ManagedFile{
		Kind: kind, Layer: deployment.LayerBase, SourceAbsolutePath: path,
		SourceRepositoryPath: relative, TargetRelativePath: "target",
	}
}

func writeSource(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
}

func testSourceOrdinary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	data := []byte("ordinary bytes")
	writeSource(t, path, data)
	observation, err := CaptureSource(managedSource(path, "config", deployment.FileOrdinary), nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := observation.Snapshot()
	if snapshot.Kind() != KindFile || snapshot.Semantic() != deployment.Ordinary(data) {
		t.Fatal("ordinary snapshot did not use exact bytes")
	}
	if snapshot.Storage() != (deployment.Digest{}) || string(observation.Bytes()) != string(data) {
		t.Fatal("ordinary snapshot has unexpected storage or bytes")
	}
}

func secretJSON() []byte { return []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`) }

func testSourceSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	data := secretJSON()
	writeSource(t, path, data)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err := CaptureSource(managedSource(path, "app/token", deployment.FileSecret), nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := observation.Snapshot()
	if snapshot.Storage() != deployment.RawStorage(data) || snapshot.Semantic() != (deployment.Digest{}) {
		t.Fatal("secret snapshot did not preserve raw-storage identity")
	}
	if _, err := os.Stat(path); err != nil || !pathsafe.SameIdentity(snapshot.Identity(), mustIdentity(t, path)) {
		t.Fatal("secret identity does not match the source")
	}
}

func mustIdentity(t *testing.T, path string) pathsafe.Identity {
	t.Helper()
	identity, err := pathsafe.FilesystemIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func testSourceMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad")
	for _, data := range [][]byte{nil, []byte("not json"), []byte(`{"data":"x"}`), []byte(`{"sops":null}`)} {
		writeSource(t, path, data)
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := CaptureSource(managedSource(path, "bad", deployment.FileSecret), nil); err == nil {
			t.Fatalf("accepted malformed secret %q", data)
		}
	}
}

func testSourceRejectedType(t *testing.T) {
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "real"), []byte("x"))
	if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing", "link", "dir"} {
		if _, err := CaptureSource(managedSource(filepath.Join(root, name), name, deployment.FileOrdinary), nil); err == nil {
			t.Fatalf("accepted %s source", name)
		}
	}
}

func testSourceReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	writeSource(t, path, []byte("same"))
	first, err := CaptureSource(managedSource(path, "file", deployment.FileOrdinary), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeSource(t, path, []byte("same"))
	second, err := CaptureSource(managedSource(path, "file", deployment.FileOrdinary), nil)
	if err != nil {
		t.Fatal(err)
	}
	if pathsafe.SameIdentity(first.Snapshot().Identity(), second.Snapshot().Identity()) {
		t.Fatal("replacement reused the source identity")
	}
}

func testSourceModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run")
	writeSource(t, path, []byte("x"))
	first, err := CaptureSource(managedSource(path, "run", deployment.FileOrdinary), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := CaptureSource(managedSource(path, "run", deployment.FileOrdinary), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot().Executable() != 0 || second.Snapshot().Executable() != 0o111 {
		t.Fatalf("executable bits = %o, %o", first.Snapshot().Executable(), second.Snapshot().Executable())
	}
}

func fakeClient(t *testing.T, behavior sops.Behavior) (*secrets.Client, string) {
	t.Helper()
	executable := sops.Build(t)
	command, err := executable.Command(behavior)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range command.Env {
		if strings.HasPrefix(value, "FAKE_SOPS_RECORD=") {
			return secrets.NewClient(executable.Path, t.TempDir(), command.Env), strings.TrimPrefix(value, "FAKE_SOPS_RECORD=")
		}
	}
	t.Fatal("fake sops record missing")
	return nil, ""
}

func testSourceDecrypt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	writeSource(t, path, secretJSON())
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	client, record := fakeClient(t, sops.Behavior{Stdout: []byte("plain")})
	observation, err := CaptureSource(managedSource(path, "app/token", deployment.FileSecret), client)
	if err != nil {
		t.Fatal(err)
	}
	if recordRun(record) {
		t.Fatal("capture decrypted before keyed semantics")
	}
	var key [32]byte
	digest, err := observation.KeyedSecretSemantic(context.Background(), key)
	if err != nil || digest != deployment.SecretSemantic([]byte("plain"), key) {
		t.Fatalf("keyed semantic = %v, %v", digest, err)
	}
	if !recordRun(record) || string(observation.Bytes()) != string(secretJSON()) {
		t.Fatal("decrypt did not preserve ciphertext or invoke sops")
	}
}

func recordRun(path string) bool {
	var record map[string]any
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, &record) == nil
}

func testSourceKeyedOrdinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeSource(t, path, []byte("plain"))
	observation, err := CaptureSource(managedSource(path, "config", deployment.FileOrdinary), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observation.KeyedSecretSemantic(context.Background(), [32]byte{}); err == nil {
		t.Fatal("ordinary source accepted keyed semantics")
	}
}

func testSourceClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	writeSource(t, path, secretJSON())
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err := CaptureSource(managedSource(path, "token", deployment.FileSecret), nil)
	if err != nil {
		t.Fatal(err)
	}
	retained := observation.Bytes()
	observation.Clear()
	if observation.Bytes() != nil {
		t.Fatal("Clear retained the buffer")
	}
	for index, value := range retained {
		if value != 0 {
			t.Fatalf("buffer byte %d was not cleared", index)
		}
	}
}

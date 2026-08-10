package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestExecutableSecrets(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"secret adoption", testSecretsAdd},
		{"secret apply round trip", testSecretsApply},
		{"dependency failure", testSecretsDependency},
		{"hash key recovery", testSecretsKey},
		{"real age round trip", testSecretsRealRoundTrip},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// sopsFreePath returns the current PATH with every directory containing
// an executable named sops removed, so dependency scenarios observe a
// missing dependency regardless of the host environment.
func sopsFreePath() string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	var kept []string
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, "sops")); os.IsNotExist(err) {
			kept = append(kept, dir)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// fakeEnv builds an environment whose PATH exposes the fake executable as
// sops together with its behavior variables.
func fakeEnv(t *testing.T, env execEnv) execEnv {
	t.Helper()
	executable := sops.Build(t)
	command, err := executable.Command(sops.Behavior{Stdout: []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(executable.Path, filepath.Join(binDir, "sops")); err != nil {
		t.Fatal(err)
	}
	env.extraEnv = append(env.extraEnv, "PATH="+binDir+":"+os.Getenv("PATH"))
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "FAKE_SOPS_") {
			env.extraEnv = append(env.extraEnv, entry)
		}
	}
	return env
}

// readTargetFile reads one absolute target path.
func readTargetFile(t *testing.T, home, relative string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read target %s: %v", relative, err)
	}
	return content
}

// secretRun executes the binary under the fake sops environment.
func (env execEnv) secretRun(t *testing.T, stdin []string, args ...string) ProcessResult {
	t.Helper()
	return env.fixture.Run(t, ProcessInput{
		Args: args, Home: env.home, Stdin: joined(stdin), Timeout: 30 * time.Second, Env: env.extraEnv,
	})
}

func testSecretsAdd(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	envelope := []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)
	writeFile(t, filepath.Join(env.home, "token"), envelope)
	env = fakeEnv(t, env)
	result := env.secretRun(t, nil, "add", "--secret", "token")
	if result.Code != 0 {
		t.Fatalf("add: code=%d stderr=%q", result.Code, result.Stderr)
	}
	source, err := os.ReadFile(filepath.Join(env.repo, "_secrets", "token"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if string(source) != string(envelope) {
		t.Fatal("the repository must carry the ciphertext only")
	}
	if string(readTargetFile(t, env.home, "token")) != string(envelope) {
		t.Fatal("the target must be preserved exactly")
	}
}

func testSecretsApply(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	envelope := []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)
	writeFile(t, filepath.Join(env.repo, "_secrets", "token"), envelope)
	env = fakeEnv(t, env)
	result := env.secretRun(t, nil, "apply")
	if result.Code != 0 {
		t.Fatalf("apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	if string(readTargetFile(t, env.home, "token")) != string(envelope) {
		t.Fatal("the target must carry the decrypted bytes")
	}
	status := env.secretRun(t, nil, "status")
	if status.Code != 0 {
		t.Fatalf("status: %+v", status)
	}
	diff := env.secretRun(t, nil, "diff")
	if diff.Code != 0 {
		t.Fatalf("diff: %+v", diff)
	}
	if strings.Contains(diff.Stdout, "data") && strings.Contains(diff.Stdout, "sops") {
		t.Fatalf("diff = %q, a converged secret must not render payload", diff.Stdout)
	}
}

func testSecretsDependency(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	writeFile(t, filepath.Join(env.repo, "_secrets", "token"), []byte(`{"data":"eA==","sops":{"version":"3.9.0"}}`))
	writeFile(t, filepath.Join(env.home, "token"), []byte("plaintext"))
	env.extraEnv = append(env.extraEnv, "PATH="+sopsFreePath())
	result := env.secretRun(t, nil, "apply")
	if result.Code != 4 {
		t.Fatalf("code = %d, want 4 for a missing sops dependency", result.Code)
	}
}

// installIdentity copies the fixture identity into one home so sops can
// decrypt ciphertext produced for the shared recipient.
func installIdentity(t *testing.T, home, keyHome string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(keyHome, ".config", "sops", "age", "keys.txt"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dir := filepath.Join(home, ".config", "sops", "age")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keys.txt"), source, 0o600); err != nil {
		t.Fatal(err)
	}
}

// testSecretsRealRoundTrip proves an actual binary-mode encrypt/decrypt
// round trip through the pinned real sops and age tools with an ephemeral
// identity. It skips only when the real tools are absent, which never
// happens under just test-sops in the pinned shell.
func testSecretsRealRoundTrip(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	fixture := NewSOPSFixture(t)
	if !fixture.RealAvailable() {
		t.Skip("the pinned real sops and age tools are not installed")
	}
	keyHome := t.TempDir()
	fixture.SetupAge(t, keyHome)
	fixture.SetupConfig(t, keyHome, env.repo)
	payload := realSecretPayload()
	env.addRealSecret(t, keyHome, payload)
	env.applyRealSecret(t, keyHome, payload)
}

// realSecretPayload builds the Section 13 27-byte fixture containing NUL
// and invalid UTF-8 bytes.
func realSecretPayload() []byte {
	payload := make([]byte, 27)
	for index := range payload {
		payload[index] = byte(0x41 + index)
	}
	payload[3] = 0
	payload[8] = 0xff
	return payload
}

// addRealSecret adopts one binary target through the real sops pipeline
// and verifies the stored ciphertext shape.
func (env execEnv) addRealSecret(t *testing.T, keyHome string, payload []byte) {
	t.Helper()
	installIdentity(t, env.home, keyHome)
	writeFile(t, filepath.Join(env.home, "token"), payload)
	result := env.secretRun(t, nil, "add", "--secret", "token")
	if result.Code != 0 {
		t.Fatalf("add: code=%d stderr=%q", result.Code, result.Stderr)
	}
	ciphertext, err := os.ReadFile(filepath.Join(env.repo, "_secrets", "token"))
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if !json.Valid(ciphertext) {
		t.Fatal("the encrypted source must be valid JSON")
	}
	if bytes.Contains(ciphertext, payload) {
		t.Fatal("plaintext must never appear in the encrypted source")
	}
}

// applyRealSecret decrypts the stored ciphertext into a second home and
// verifies a byte-exact round trip.
func (env execEnv) applyRealSecret(t *testing.T, keyHome string, payload []byte) {
	t.Helper()
	second := execEnv{fixture: env.fixture, repo: env.repo, home: t.TempDir()}
	if result := second.run(t, nil, "init", second.repo); result.Code != 0 {
		t.Fatalf("second init: %+v", result)
	}
	installIdentity(t, second.home, keyHome)
	if result := second.secretRun(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("second apply: code=%d stderr=%q", result.Code, result.Stderr)
	}
	decrypted, err := os.ReadFile(filepath.Join(second.home, "token"))
	if err != nil {
		t.Fatalf("decrypted target: %v", err)
	}
	if !bytes.Equal(decrypted, payload) {
		t.Fatalf("round trip mismatch: got %x, want %x", decrypted, payload)
	}
	if status := second.secretRun(t, nil, "status"); status.Code != 0 {
		t.Fatalf("status: %+v", status)
	}
}

func testSecretsKey(t *testing.T) {
	env := newExecEnv(t)
	env.initRepository(t)
	envelope := []byte(`{"data":"ZmFrZS1jaXBoZXI=","sops":{"version":"3.9.0"}}`)
	writeFile(t, filepath.Join(env.repo, "_secrets", "token"), envelope)
	env = fakeEnv(t, env)
	if result := env.secretRun(t, nil, "apply"); result.Code != 0 {
		t.Fatalf("apply: %+v", result)
	}
	keyPath := filepath.Join(env.home, ".local", "state", "cattery", "hash.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("hash key: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("hash key length = %d, want 32", len(key))
	}
}

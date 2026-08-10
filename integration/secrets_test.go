package integration

import (
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
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
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
	input := ""
	if len(stdin) > 0 {
		input = strings.Join(stdin, "\n") + "\n"
	}
	return env.fixture.Run(t, ProcessInput{
		Args: args, Home: env.home, Stdin: input, Timeout: 30 * time.Second, Env: env.extraEnv,
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
	env.extraEnv = append(env.extraEnv, "PATH="+os.Getenv("PATH"))
	result := env.secretRun(t, nil, "apply")
	if result.Code != 4 {
		t.Fatalf("code = %d, want 4 for a missing sops dependency", result.Code)
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

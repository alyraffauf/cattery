package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

// SOPSFixture bundles the fake executable and the ephemeral real sops
// environment, when the pinned real tools are discoverable.
type SOPSFixture struct {
	Fake       *sops.Executable
	Client     *secrets.Client
	RealSOPS   string
	RealAge    string
	RealAgeGen string
	AgeKey     string
	ConfigDir  string
}

// NewSOPSFixture installs the fake executable and probes for the pinned
// real sops and age tools under isolated paths. When the real tools are
// missing, the real paths are skipped and never silently reused.
func NewSOPSFixture(t *testing.T) SOPSFixture {
	t.Helper()
	executable := sops.Build(t)
	command, err := executable.Command(sops.Behavior{Stdout: []byte(`{"data":"eA==","sops":{"version":"3.9.0"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	fixture := SOPSFixture{
		Fake:      executable,
		Client:    secrets.NewClient(executable.Path, t.TempDir(), command.Env),
		ConfigDir: filepath.Join(t.TempDir(), ".config", "sops"),
	}
	fixture.RealSOPS = probeTool(t, "sops", "--version")
	fixture.RealAge = probeTool(t, "age", "--version")
	fixture.RealAgeGen = probeTool(t, "age-keygen", "--version")
	return fixture
}

// probeTool returns the resolved binary path or empty when missing.
func probeTool(t *testing.T, name string, args ...string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	command := exec.Command(path, args...)
	command.Env = append(os.Environ(), "HOME="+t.TempDir())
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return ""
	}
	return path
}

// RealAvailable reports whether the pinned real tools exist.
func (fixture SOPSFixture) RealAvailable() bool {
	return fixture.RealSOPS != "" && fixture.RealAge != "" && fixture.RealAgeGen != ""
}

// SetupAge generates one ephemeral age identity in the sops-discovered
// location of the fixture home.
func (fixture SOPSFixture) SetupAge(t *testing.T, home string) string {
	t.Helper()
	ageDir := filepath.Join(home, ".config", "sops", "age")
	if err := os.MkdirAll(ageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(ageDir, "keys.txt")
	command := exec.Command(fixture.RealAgeGen, "-o", key)
	command.Env = append(os.Environ(), "HOME="+home)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("age-keygen: %v\n%s", err, output)
	}
	return key
}

// SetupConfig writes one ephemeral sops config bound to the age identity
// created by SetupAge under the given home into the given directory, the
// repository root cattery uses as the SOPS working directory.
func (fixture SOPSFixture) SetupConfig(t *testing.T, home, directory string) {
	t.Helper()
	publicKey := fixture.agePublicKey(t, home)
	config := "creation_rules:\n  - age: " + publicKey + "\n"
	configPath := filepath.Join(directory, ".sops.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

// agePublicKey extracts the public key line from the fixture identity,
// located under the given home directory.
func (fixture SOPSFixture) agePublicKey(t *testing.T, home string) string {
	t.Helper()
	keyBytes, err := os.ReadFile(filepath.Join(home, ".config", "sops", "age", "keys.txt"))
	if err != nil {
		t.Fatalf("age identity: %v", err)
	}
	for _, line := range strings.Split(string(keyBytes), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# public key: ") {
			return strings.TrimPrefix(line, "# public key: ")
		}
	}
	t.Fatal("no public key in the age identity")
	return ""
}

// Cleanup removes every created identity, config, and binary copy.
func (fixture SOPSFixture) Cleanup(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			t.Errorf("cleanup %s: %v", path, err)
		}
	}
}

// versionLine returns the first line of one tool invocation.
func versionLine(t *testing.T, path string, args ...string) string {
	t.Helper()
	command := exec.Command(path, args...)
	command.Env = append(os.Environ(), "HOME="+t.TempDir())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return strings.Split(strings.TrimSpace(string(output)), "\n")[0]
}

func TestIntegrationSOPSFixture(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"fake installed", testSOPSFake},
		{"real tools probed", testSOPSRealProbe},
		{"no user config reuse", testSOPSIsolation},
		{"cleanup removes paths", testSOPSCleanup},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testSOPSFake(t *testing.T) {
	fixture := NewSOPSFixture(t)
	if fixture.Fake == nil || fixture.Client == nil {
		t.Fatal("the fake executable and client must install")
	}
}

func testSOPSRealProbe(t *testing.T) {
	fixture := NewSOPSFixture(t)
	if fixture.RealAvailable() {
		if strings.TrimSpace(versionLine(t, fixture.RealSOPS, "--version")) == "" {
			t.Fatal("the real sops must report a version")
		}
		if strings.TrimSpace(versionLine(t, fixture.RealAge, "--version")) == "" {
			t.Fatal("the real age must report a version")
		}
	}
}

func testSOPSIsolation(t *testing.T) {
	fixture := NewSOPSFixture(t)
	home := t.TempDir()
	if fixture.RealAvailable() {
		key := fixture.SetupAge(t, home)
		if _, err := os.Stat(key); err != nil {
			t.Fatalf("age key: %v", err)
		}
	}
	if _, err := os.Stat(fixture.ConfigDir); !os.IsNotExist(err) {
		t.Fatal("no fixture may touch a real user config")
	}
}

func testSOPSCleanup(t *testing.T) {
	fixture := NewSOPSFixture(t)
	paths := []string{t.TempDir()}
	fixture.Cleanup(t, paths...)
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cleanup must remove %s", path)
		}
	}
}

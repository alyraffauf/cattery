package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSOPSEncrypt(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"exact argv and stdin", testEncryptExactInvocation},
		{"arbitrary binary input", testEncryptBinaryInput},
		{"invalid json output", testEncryptMalformedOutput},
		{"empty output", testEncryptEmptyOutput},
		{"no repository artifacts", testEncryptNoArtifacts},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// testEncryptExactInvocation echoes stdin as valid JSON so the strict output
// check passes while proving the exact stdin bytes reached the process.
func testEncryptExactInvocation(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	plaintext := []byte(`{"value":"token=sekrit"}`)
	client, env := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{EchoStdin: true}})
	output, err := client.Encrypt(context.Background(), plaintext, "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(output, plaintext) {
		t.Fatalf("stdin not echoed exactly")
	}
	rec := readRecord(t, env)
	want := []string{
		"encrypt", "--filename-override", "app/token",
		"--input-type", "binary", "--output-type", "json", "/dev/stdin",
	}
	if !slices.Equal(rec.Argv[1:], want) {
		t.Fatalf("argv = %v, want %v", rec.Argv, want)
	}
	if rec.Cwd != repository {
		t.Fatalf("cwd = %q, want %q", rec.Cwd, repository)
	}
}
func testEncryptBinaryInput(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	plaintext := []byte{0x00, 0xff, 'a', 0x00, 0x01, 0xfe}
	output := []byte(`{"data":"aGVsbG8g","more":true}`)
	client, env := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: output}})
	got, err := client.Encrypt(context.Background(), plaintext, "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(got, output) {
		t.Fatalf("output = %v, want the fixture json", got)
	}
	if !json.Valid(got) {
		t.Fatalf("output is not valid json")
	}
	if rec := readRecord(t, env); !bytes.Equal(rec.Stdin, plaintext) {
		t.Fatalf("stdin = %v, want %v", rec.Stdin, plaintext)
	}
}
func testEncryptMalformedOutput(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	secret := "plaintext-must-not-leak"
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: []byte("not-json")}})
	output, err := client.Encrypt(context.Background(), []byte(secret), "app/token")
	expectKind(t, err, failure.Operational)
	if output != nil {
		t.Fatalf("output = %d bytes, want none", len(output))
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("plaintext leaked into error: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("malformed output not named: %q", err.Error())
	}
}
func testEncryptEmptyOutput(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository})
	output, err := client.Encrypt(context.Background(), []byte("plaintext"), "app/token")
	expectKind(t, err, failure.Operational)
	if output != nil {
		t.Fatalf("output = %d bytes, want none", len(output))
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("empty output not rejected: %q", err.Error())
	}
}
func testEncryptNoArtifacts(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: []byte(`{"enc":"ok"}`)}})
	output, err := client.Encrypt(context.Background(), []byte("plaintext"), "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(output, []byte(`{"enc":"ok"}`)) {
		t.Fatalf("output mismatch")
	}
	entries, err := os.ReadDir(repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("repository gained %d entries", len(entries))
	}
}

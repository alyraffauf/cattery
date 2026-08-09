package secrets

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSOPSDecrypt(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"exact argv and stdin", testDecryptExactInvocation},
		{"binary plaintext", testDecryptBinaryPlaintext},
		{"empty plaintext", testDecryptEmptyPlaintext},
		{"wrong json failure", testDecryptWrongJSON},
		{"cancellation", testDecryptCancellation},
		{"caller-owned plaintext", testDecryptOwnership},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testDecryptExactInvocation(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	ciphertext := []byte(`{"data":"c2Vrcml0"}`)
	client, env := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{EchoStdin: true}})
	plaintext, err := client.Decrypt(context.Background(), ciphertext, "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(plaintext, ciphertext) {
		t.Fatalf("stdin not echoed exactly")
	}
	rec := readRecord(t, env)
	want := []string{
		"decrypt", "--filename-override", "app/token",
		"--input-type", "json", "--output-type", "binary", "/dev/stdin",
	}
	if !slices.Equal(rec.Argv[1:], want) {
		t.Fatalf("argv = %v, want %v", rec.Argv, want)
	}
	if rec.Cwd != repository {
		t.Fatalf("cwd = %q, want %q", rec.Cwd, repository)
	}
}
func testDecryptBinaryPlaintext(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	plaintext := []byte{0x00, 0xff, 'a', 0x00, 0x01, 0xfe}
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: plaintext}})
	got, err := client.Decrypt(context.Background(), []byte(`{"data":"x"}`), "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch")
	}
}
func testDecryptEmptyPlaintext(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository})
	plaintext, err := client.Decrypt(context.Background(), []byte(`{"data":"x"}`), "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(plaintext) != 0 {
		t.Fatalf("plaintext = %d bytes, want empty", len(plaintext))
	}
}
func testDecryptWrongJSON(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	details := "json: unexpected token"
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stderr: []byte(details), ExitCode: 1}})
	plaintext, err := client.Decrypt(context.Background(), []byte("not-json"), "app/token")
	expectKind(t, err, failure.Operational)
	if plaintext != nil {
		t.Fatalf("plaintext = %d bytes, want none", len(plaintext))
	}
	message := err.Error()
	if strings.Contains(message, details) {
		t.Fatalf("stderr leaked into error: %q", message)
	}
	if !strings.Contains(message, "app/token exited with status 1") {
		t.Fatalf("diagnostic missing context: %q", message)
	}
}
func testDecryptCancellation(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, env := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Sleep: 30 * time.Second}})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := client.Decrypt(ctx, []byte(`{"data":"x"}`), "app/token"); finished <- err }()
	rec := waitForRecord(t, env)
	cancel()
	err := <-finished
	expectKind(t, err, failure.Operational)
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation not named: %q", err.Error())
	}
	if processAlive(rec.ChildPid) {
		t.Fatal("descendant survived cancellation")
	}
}
func testDecryptOwnership(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	ciphertext := []byte(`{"data":"c2Vrcml0"}`)
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{EchoStdin: true}})
	plaintext, err := client.Decrypt(context.Background(), ciphertext, "app/token")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	zeroBytes(plaintext)
	if !bytes.Equal(ciphertext, []byte(`{"data":"c2Vrcml0"}`)) {
		t.Fatal("caller clearing corrupted the input ciphertext")
	}
	second, err := client.Decrypt(context.Background(), ciphertext, "app/token")
	if err != nil {
		t.Fatalf("second decrypt failed: %v", err)
	}
	if !bytes.Equal(second, ciphertext) {
		t.Fatalf("second decrypt lost data")
	}
}

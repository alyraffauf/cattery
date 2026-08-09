package secrets

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSecretCandidate(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"byte-exact equality", testCandidateEquality},
		{"mismatch rejected", testCandidateMismatch},
		{"empty plaintext", testCandidateEmptyPlaintext},
		{"empty candidate", testCandidateEmpty},
		{"malformed candidate", testCandidateMalformed},
		{"partial plaintext", testCandidatePartial},
		{"decrypt failure", testCandidateFailure},
		{"binary bytes", testCandidateBinary},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testCandidateEquality(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	plaintext := []byte("token=sekrit\n")
	candidate := []byte(`{"data":"c2Vrcml0"}`)
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: plaintext}})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: plaintext, Ciphertext: candidate, SourcePath: "app/token"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(adopted, candidate) {
		t.Fatalf("candidate not adopted exactly")
	}
	if !bytes.Equal(plaintext, []byte("token=sekrit\n")) {
		t.Fatal("caller-owned plaintext was modified")
	}
}
func testCandidateMismatch(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: []byte("different-plaintext")}})
	plaintext := []byte("original-plaintext")
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: plaintext, Ciphertext: []byte(`{"data":"x"}`), SourcePath: "app/token"})
	expectKind(t, err, failure.Operational)
	if adopted != nil {
		t.Fatalf("mismatching candidate adopted")
	}
	message := err.Error()
	if strings.Contains(message, "original-plaintext") || strings.Contains(message, "different-plaintext") {
		t.Fatalf("plaintext leaked into error: %q", message)
	}
	if !bytes.Equal(plaintext, []byte("original-plaintext")) {
		t.Fatal("caller-owned plaintext was modified on mismatch")
	}
}
func testCandidateEmptyPlaintext(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	candidate := []byte(`{"data":"x"}`)
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: nil, Ciphertext: candidate, SourcePath: "app/token"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(adopted, candidate) {
		t.Fatalf("empty plaintext candidate not adopted")
	}
}
func testCandidateEmpty(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{ExitCode: 9}})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: []byte("p"), Ciphertext: nil, SourcePath: "app/token"})
	expectKind(t, err, failure.Operational)
	if adopted != nil {
		t.Fatalf("empty candidate adopted")
	}
	if strings.Contains(err.Error(), "status 9") {
		t.Fatalf("decrypt ran for an empty candidate: %q", err.Error())
	}
}
func testCandidateMalformed(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{ExitCode: 9}})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: []byte("p"), Ciphertext: []byte("not-json"), SourcePath: "app/token"})
	expectKind(t, err, failure.Operational)
	if adopted != nil {
		t.Fatalf("malformed candidate adopted")
	}
	if strings.Contains(err.Error(), "status 9") {
		t.Fatalf("decrypt ran for a malformed candidate: %q", err.Error())
	}
}
func testCandidatePartial(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	plaintext := []byte("full-plaintext")
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: plaintext[:5]}})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: plaintext, Ciphertext: []byte(`{"data":"x"}`), SourcePath: "app/token"})
	expectKind(t, err, failure.Operational)
	if adopted != nil {
		t.Fatalf("partial plaintext candidate adopted")
	}
}
func testCandidateFailure(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{ExitCode: 2}})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: []byte("p"), Ciphertext: []byte(`{"data":"x"}`), SourcePath: "app/token"})
	expectKind(t, err, failure.Operational)
	if adopted != nil {
		t.Fatalf("candidate adopted despite decrypt failure")
	}
	if !strings.Contains(err.Error(), "status 2") {
		t.Fatalf("decrypt failure not reported: %q", err.Error())
	}
}
func testCandidateBinary(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	plaintext := []byte{0x00, 0xff, 'a', 0x00, 0x01, 0xfe}
	candidate := []byte(`{"data":"c2Vrcml0"}`)
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stdout: plaintext}})
	adopted, err := client.ValidateCandidate(context.Background(), Candidate{Plaintext: plaintext, Ciphertext: candidate, SourcePath: "app/token"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(adopted, candidate) {
		t.Fatalf("binary plaintext candidate not adopted")
	}
}

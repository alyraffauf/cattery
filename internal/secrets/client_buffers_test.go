package secrets

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/testfixture/sops"
)

func TestSafeDiagnosticPathQuotesLineBreakingNames(t *testing.T) {
	if got := safeDiagnosticPath("app/token"); got != "app/token" {
		t.Fatalf("ordinary path = %q", got)
	}
	if got := safeDiagnosticPath("app/line\nbreak"); got != `"app/line\nbreak"` {
		t.Fatalf("unsafe path = %q", got)
	}
}

func testLargeStderr(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	marker := []byte("large-stderr-secret-")
	stderr := bytes.Repeat(marker, 128*1024)
	client, _ := newTestClient(t, clientTarget{executable: executable, repository: repository, behavior: sops.Behavior{Stderr: stderr, ExitCode: 7}})
	output, err := client.Run(context.Background(), basicRequest("decrypt", "app/token"))
	expectKind(t, err, failure.Operational)
	if output != nil {
		t.Fatalf("output = %d bytes, want none", len(output))
	}
	if strings.Contains(err.Error(), string(marker)) {
		t.Fatal("large stderr leaked into error")
	}
}

func testEnvironmentCopy(t *testing.T) {
	executable := sops.Build(t)
	repository := t.TempDir()
	command, err := executable.Command(sops.Behavior{Stdout: []byte("known-output")})
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(executable.Path, repository, command.Env)
	for index, entry := range command.Env {
		if strings.HasPrefix(entry, "FAKE_SOPS_SPEC=") {
			command.Env[index] = "FAKE_SOPS_SPEC=" + filepath.Join(repository, "missing-spec")
		}
	}
	output, err := client.Run(context.Background(), basicRequest("encrypt", "app/token"))
	if err != nil {
		t.Fatalf("copied environment was changed: %v", err)
	}
	if !bytes.Equal(output, []byte("known-output")) {
		t.Fatalf("output = %q, want known-output", output)
	}
}

func testBufferZeroing(t *testing.T) {
	testZeroBytes(t)
	testBoundedClear(t)
}

func testZeroBytes(t *testing.T) {
	data := []byte("sensitive-buffer")
	zeroBytes(data)
	if len(bytes.Trim(data, "\x00")) != 0 {
		t.Fatal("zeroBytes left non-zero data")
	}
}

func testBoundedClear(t *testing.T) {
	overflowed := false
	capture := newBounded(4, func() { overflowed = true })
	capture.Write([]byte("abcd"))
	if overflowed {
		t.Fatal("overflow flagged before the limit")
	}
	capture.Write([]byte("xyz"))
	if !overflowed {
		t.Fatal("overflow not flagged past the limit")
	}
	if string(capture.buf) != "abcd" {
		t.Fatalf("captured %q, want abcd", capture.buf)
	}
	owned := capture.buf
	capture.clear()
	if capture.buf != nil {
		t.Fatal("clear left a buffer")
	}
	if !bytes.Equal(owned, make([]byte, len(owned))) {
		t.Fatal("clear left captured bytes in its backing buffer")
	}
}

func testDrainCapture(t *testing.T) {
	drain := newDrain(8)
	drain.Write([]byte("abcdefghijklmnop"))
	if len(drain.buf) != 8 {
		t.Fatalf("drain kept %d bytes, want 8", len(drain.buf))
	}
	owned := drain.buf
	drain.clear()
	if drain.buf != nil {
		t.Fatal("drain clear left a buffer")
	}
	if !bytes.Equal(owned, make([]byte, len(owned))) {
		t.Fatal("drain clear left captured bytes in its backing buffer")
	}
}

package diff

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

const textLimit = 1 << 20

// TestSafeDiffRecord pins the record contract: the four
// tagged variants for printable text, binary/large ordinary files,
// metadata-only changes, and secrets, with control/bidi/invalid-UTF-8 runes
// demoted to binary, escaped labels, verified sizes and hashes, and zero
// payload on None and Secret records.
func TestSafeDiffRecord(t *testing.T) {
	for _, row := range safeDiffCases {
		t.Run(row.name, func(t *testing.T) {
			checkSafeDiff(t, row)
		})
	}
}

// safeDiffCase is one row of the record-construction matrix.
type safeDiffCase struct {
	name        string
	kind        deployment.FileKind
	repoPath    string
	targetPath  string
	sourceBytes []byte
	targetBytes []byte
	wantTag     Tag
	wantZero    bool
	wantSource  string
	wantTarget  string
	wantLines   []string
}

// safeDiffCases enumerates the Section 9.6 rules: printable text produces a
// unified diff, equal content produces a metadata-only record, carriage
// return/ESC/bidi/invalid UTF-8 or an oversized side demotes to binary,
// secrets carry only classification, and labels never expose control runes.
var safeDiffCases = []safeDiffCase{
	{name: "text diff", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("alpha\nbeta\n"), targetBytes: []byte("alpha\ngamma\n"),
		wantTag: TagText, wantSource: "repo/files/config", wantTarget: "$HOME/config",
		wantLines: []string{"--- repo/files/config", "+++ $HOME/config", "-beta", "+gamma"}},
	{name: "text against absent target", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("line1\nline2\n"),
		wantTag:     TagText,
		wantLines:   []string{"-line1", "-line2"}},
	{name: "metadata only", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("same\n"), targetBytes: []byte("same\n"),
		wantTag: TagNone, wantZero: true},
	{name: "carriage return binary", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("a\rb\n"), targetBytes: []byte("short\n"),
		wantTag: TagBinary},
	{name: "escape control binary", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("a\x1bb\n"), targetBytes: []byte("short\n"),
		wantTag: TagBinary},
	{name: "bidi format binary", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("a\u202eb\n"), targetBytes: []byte("short\n"),
		wantTag: TagBinary},
	{name: "invalid utf8 binary", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte{0xff, 0xfe}, targetBytes: []byte("short\n"),
		wantTag: TagBinary},
	{name: "source over limit", repoPath: "files/config", targetPath: "config",
		sourceBytes: bytes.Repeat([]byte{'a'}, textLimit+1), targetBytes: []byte("short\n"),
		wantTag: TagBinary},
	{name: "target over limit", repoPath: "files/config", targetPath: "config",
		sourceBytes: []byte("short\n"), targetBytes: bytes.Repeat([]byte{'b'}, textLimit+1),
		wantTag: TagBinary},
	{name: "secret differs", kind: deployment.FileSecret, repoPath: "app/token", targetPath: "token",
		sourceBytes: []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`),
		targetBytes: []byte(`{"data":"QUFB","sops":{"version":"3.9.0"}}`),
		wantTag:     TagSecret, wantZero: true},
	{name: "secret metadata only", kind: deployment.FileSecret, repoPath: "app/token", targetPath: "token",
		sourceBytes: []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`),
		targetBytes: []byte(`{"data":"c2VjcmV0","sops":{"version":"3.9.0"}}`),
		wantTag:     TagNone, wantZero: true},
	{name: "escaped labels", repoPath: "files/\x1bconfig", targetPath: "config\x1b",
		sourceBytes: []byte("alpha\nbeta\n"), targetBytes: []byte("alpha\ngamma\n"),
		wantTag: TagText, wantSource: "repo/files/\\x1bconfig", wantTarget: "$HOME/config\\x1b",
		wantLines: []string{"--- repo/files/\\x1bconfig"}},
}

// checkSafeDiff builds one record from its row and compares every fact.
func checkSafeDiff(t *testing.T, row safeDiffCase) {
	t.Helper()
	record, err := Build(materialize(t, t.TempDir(), row), row.targetBytes)
	if err != nil {
		t.Fatalf("%s: Build: %v", row.name, err)
	}
	checkIdentified(t, row, record)
	checkLabels(t, row, record)
	if row.wantZero {
		checkZeroPayload(t, row, record)
	}
	if row.wantTag == TagBinary {
		checkBinaryFacts(t, row, record)
	}
	requirePrintable(t, row, record.Lines())
	requireFragments(t, row, record.Lines())
}

// checkIdentified verifies the tag and destination path of one record.
func checkIdentified(t *testing.T, row safeDiffCase, record SafeRecord) {
	t.Helper()
	if record.Tag() != row.wantTag {
		t.Fatalf("%s: tag = %v, want %v", row.name, record.Tag(), row.wantTag)
	}
	if record.TargetPath() != row.targetPath {
		t.Fatalf("%s: target path = %q, want %q", row.name, record.TargetPath(), row.targetPath)
	}
}

// checkLabels verifies the escaped diff header labels of one record.
func checkLabels(t *testing.T, row safeDiffCase, record SafeRecord) {
	t.Helper()
	if row.wantSource != "" && record.SourceLabel() != row.wantSource {
		t.Fatalf("%s: source label = %q, want %q", row.name, record.SourceLabel(), row.wantSource)
	}
	if row.wantTarget != "" && record.TargetLabel() != row.wantTarget {
		t.Fatalf("%s: target label = %q, want %q", row.name, record.TargetLabel(), row.wantTarget)
	}
}

// requireFragments fails when any expected unified-diff fragment is absent.
func requireFragments(t *testing.T, row safeDiffCase, lines string) {
	t.Helper()
	for _, fragment := range row.wantLines {
		if !strings.Contains(lines, fragment) {
			t.Fatalf("%s: lines lack %q:\n%s", row.name, fragment, lines)
		}
	}
}

// materialize writes the source and optional target files, captures both
// snapshots, and assembles the evaluation of one table row.
func materialize(t *testing.T, root string, row safeDiffCase) reconcile.Evaluation {
	t.Helper()
	sourcePath := filepath.Join(root, "source")
	if err := os.WriteFile(sourcePath, row.sourceBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	file := deployment.ManagedFile{
		Kind: row.kind, Layer: deployment.LayerBase,
		SourceAbsolutePath: sourcePath, SourceRepositoryPath: row.repoPath,
		TargetRelativePath: row.targetPath,
	}
	if file.Kind == "" {
		file.Kind = deployment.FileOrdinary
	}
	observation, err := reconcile.CaptureSource(file, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := captureTarget(t, root, row)
	if err != nil {
		t.Fatal(err)
	}
	return reconcile.Evaluation{
		TargetPath: row.targetPath, Entry: reconcile.PlanEntryFile,
		File: file, Source: observation, Target: target,
	}
}

// captureTarget captures the destination of one row, writing its bytes first
// so a missing side freezes as an absent target.
func captureTarget(t *testing.T, root string, row safeDiffCase) (reconcile.TargetSnapshot, error) {
	if len(row.targetBytes) > 0 {
		path := filepath.Join(root, "target")
		if err := os.WriteFile(path, row.targetBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return reconcile.CaptureTarget(reconcile.Destination{Root: root, Relative: "target"})
}

// checkZeroPayload requires the None and Secret variants to carry no lines,
// sizes, or hashes.
func checkZeroPayload(t *testing.T, row safeDiffCase, record SafeRecord) {
	t.Helper()
	if record.Lines() != "" || record.SourceSize() != 0 || record.TargetSize() != 0 {
		t.Fatalf("%s: content or size leaked", row.name)
	}
	if record.SourceHash() != (deployment.Digest{}) || record.TargetHash() != (deployment.Digest{}) {
		t.Fatalf("%s: hash leaked", row.name)
	}
}

// checkBinaryFacts verifies that a binary record carries exactly the captured
// sizes and semantic hashes and nothing else.
func checkBinaryFacts(t *testing.T, row safeDiffCase, record SafeRecord) {
	t.Helper()
	if record.SourceSize() != int64(len(row.sourceBytes)) || record.TargetSize() != int64(len(row.targetBytes)) {
		t.Fatalf("%s: sizes = %d/%d, want %d/%d", row.name, record.SourceSize(), record.TargetSize(),
			len(row.sourceBytes), len(row.targetBytes))
	}
	if record.SourceHash() != deployment.Ordinary(row.sourceBytes) || record.TargetHash() != deployment.Ordinary(row.targetBytes) {
		t.Fatalf("%s: hashes mismatch", row.name)
	}
	if record.Lines() != "" {
		t.Fatalf("%s: binary record carried lines", row.name)
	}
}

// requirePrintable fails when any rendered line contains a rune a terminal
// could interpret as control or formatting.
func requirePrintable(t *testing.T, row safeDiffCase, lines string) {
	t.Helper()
	for _, r := range lines {
		if r != '\n' && r != '\t' && !unicode.IsPrint(r) {
			t.Fatalf("%s: unsafe rune %U in lines", row.name, r)
		}
	}
}

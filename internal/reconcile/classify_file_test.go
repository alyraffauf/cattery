package reconcile

import (
	"io/fs"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

// TestFileClassification exhaustively classifies the complete
// source/target/baseline matrix for regular files, modes, and unbaselined
// safety, including database loss
// and source-only and target-only changes.
func TestFileClassification(t *testing.T) {
	for _, row := range fileClassificationCases {
		t.Run(row.name, func(t *testing.T) {
			got := ClassifyFile(fileRecordAt(row), semanticsAt(row))
			if got != row.want {
				t.Fatalf("classification = %+v, want %+v", got, row.want)
			}
		})
	}
}

// fileCase is one named row of the exhaustive classification table.
type fileCase struct {
	name     string
	source   []byte
	cipher   []byte
	target   []byte
	kind     EntryKind
	mode     fs.FileMode
	exec     fs.FileMode
	secret   bool
	baseline *FileState
	want     FileClassification
}

// fileClassificationCases enumerates the classification Cartesian product:
// the five core baselined rows of Section 9.2, secret storage semantics of
// Section 8.3, the unbaselined safety rows of Section 9.3, independent mode
// reconciliation of Sections 4.5 and 7.1, unexpected target types of Section
// 7.3, and retained-baseline reactivation of Section 9.5.
var fileClassificationCases = []fileCase{
	{name: "no-op", kind: KindFile, source: sourceA, target: sourceA, mode: 0o644,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNoOp, Reason: ReasonNoChange, Convergence: ConvergenceConverged}},
	{name: "mode correction", kind: KindFile, source: sourceA, target: sourceA, mode: 0o644, exec: 0o100,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionCorrectMode, Reason: ReasonModeCorrection, Convergence: ConvergencePending}},
	{name: "rw bits preserved", kind: KindFile, source: sourceA, target: sourceA, mode: 0o600,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNoOp, Reason: ReasonNoChange, Convergence: ConvergenceConverged}},
	{name: "source-only change", kind: KindFile, source: sourceB, target: sourceA, mode: 0o644,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionWriteSourceToTarget, Reason: ReasonSourceChanged, Convergence: ConvergencePending}},
	{name: "target-only drift", kind: KindFile, source: sourceA, target: targetX, mode: 0o644,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonTargetDrift, Convergence: ConvergenceDecisionRequired}},
	{name: "both changed converged", kind: KindFile, source: sourceB, target: sourceB, mode: 0o644,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionEstablishBaseline, Reason: ReasonAlreadyConverged, Convergence: ConvergenceConverged}},
	{name: "conflict", kind: KindFile, source: sourceB, target: targetX, mode: 0o644,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonConflict, Convergence: ConvergenceDecisionRequired}},
	{name: "absent target drift", source: sourceA, kind: KindAbsent,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonTargetDrift, Convergence: ConvergenceDecisionRequired}},
	{name: "absent target conflict", source: sourceB, kind: KindAbsent,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonConflict, Convergence: ConvergenceDecisionRequired}},
	{name: "secret no-op", kind: KindFile, source: plainA, cipher: cipherA, target: plainA, secret: true, mode: 0o600,
		baseline: secretStateAt(plainA, cipherA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNoOp, Reason: ReasonNoChange, Convergence: ConvergenceConverged}},
	{name: "secret mode correction", kind: KindFile, source: plainA, cipher: cipherA, target: plainA, secret: true, mode: 0o600, exec: 0o100,
		baseline: secretStateAt(plainA, cipherA, 0o100),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionCorrectMode, Reason: ReasonModeCorrection, Convergence: ConvergencePending}},
	{name: "secret re-encryption", kind: KindFile, source: plainA, cipher: cipherB, target: plainA, secret: true, mode: 0o600,
		baseline: secretStateAt(plainA, cipherA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNoOp, Reason: ReasonNoChange, Convergence: ConvergenceConverged}},
	{name: "secret source change", kind: KindFile, source: plainB, cipher: cipherB, target: plainA, secret: true, mode: 0o600,
		baseline: secretStateAt(plainA, cipherA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionWriteSourceToTarget, Reason: ReasonSourceChanged, Convergence: ConvergencePending}},
	{name: "secret target drift", kind: KindFile, source: plainA, cipher: cipherA, target: plainB, secret: true, mode: 0o600,
		baseline: secretStateAt(plainA, cipherA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonTargetDrift, Convergence: ConvergenceDecisionRequired}},
	{name: "secret converged", kind: KindFile, source: plainB, cipher: cipherB, target: plainB, secret: true, mode: 0o600,
		baseline: secretStateAt(plainA, cipherA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionEstablishBaseline, Reason: ReasonAlreadyConverged, Convergence: ConvergenceConverged}},
	{name: "secret conflict", kind: KindFile, source: plainB, cipher: cipherB, target: plainC, secret: true, mode: 0o600,
		baseline: secretStateAt(plainA, cipherA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonConflict, Convergence: ConvergenceDecisionRequired}},
	{name: "database loss create", source: sourceA, kind: KindAbsent,
		want: FileClassification{TargetPath: "a.conf", Action: ActionCreateTarget, Reason: ReasonUnbaselinedAbsent, Convergence: ConvergencePending}},
	{name: "database loss adopt", kind: KindFile, source: sourceA, target: sourceA, mode: 0o644,
		want: FileClassification{TargetPath: "a.conf", Action: ActionEstablishBaseline, Reason: ReasonUnbaselinedEqual, Convergence: ConvergenceConverged}},
	{name: "database loss mode", kind: KindFile, source: sourceA, target: sourceA, mode: 0o644, exec: 0o100,
		want: FileClassification{TargetPath: "a.conf", Action: ActionCorrectMode, Reason: ReasonModeCorrection, Convergence: ConvergencePending}},
	{name: "database loss differ", kind: KindFile, source: sourceA, target: targetX, mode: 0o644,
		want: FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonUnbaselinedDiffer, Convergence: ConvergenceDecisionRequired}},
	{name: "database loss secret adopt", kind: KindFile, source: plainA, cipher: cipherA, target: plainA, secret: true, mode: 0o600,
		want: FileClassification{TargetPath: "a.conf", Action: ActionEstablishBaseline, Reason: ReasonUnbaselinedEqual, Convergence: ConvergenceConverged}},
	{name: "symlink drift", source: sourceA, kind: KindSymlink,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonUnexpectedTargetType, Convergence: ConvergenceDecisionRequired}},
	{name: "symlink unbaselined", source: sourceA, kind: KindSymlink,
		want: FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonUnexpectedTargetType, Convergence: ConvergenceDecisionRequired}},
	{name: "directory target", source: sourceA, kind: KindDirectory,
		baseline: stateAt(sourceA, sourceA, 0),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonUnexpectedTargetType, Convergence: ConvergenceRejected}},
	{name: "directory unbaselined", source: sourceA, kind: KindDirectory,
		want: FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonUnexpectedTargetType, Convergence: ConvergenceRejected}},
	{name: "special target", source: sourceA, kind: KindSpecial,
		want: FileClassification{TargetPath: "a.conf", Action: ActionNeedsDecision, Reason: ReasonUnexpectedTargetType, Convergence: ConvergenceRejected}},
	{name: "retired reactivation", kind: KindFile, source: sourceA, target: sourceA, mode: 0o644,
		baseline: retired(stateAt(sourceA, sourceA, 0)),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionNoOp, Reason: ReasonNoChange, Convergence: ConvergenceConverged}},
	{name: "retired source change", kind: KindFile, source: sourceB, target: sourceA, mode: 0o644,
		baseline: retired(stateAt(sourceA, sourceA, 0)),
		want:     FileClassification{TargetPath: "a.conf", Action: ActionWriteSourceToTarget, Reason: ReasonSourceChanged, Convergence: ConvergencePending}},
}

// fileRecordAt assembles one complete file evaluation from its table row.
func fileRecordAt(row fileCase) Evaluation {
	source := ordinarySource(row.source, row.exec)
	if row.secret {
		source = secretSource(row.cipher, row.exec)
	}
	kind := deployment.FileOrdinary
	if row.secret {
		kind = deployment.FileSecret
	}
	return Evaluation{TargetPath: "a.conf", Entry: PlanEntryFile,
		File:   deployment.ManagedFile{Kind: kind, TargetRelativePath: "a.conf"},
		Source: source, Target: targetAt(row.kind, row.target, row.mode), FileState: row.baseline}
}

// semanticsAt derives the current semantic fingerprints of a table row:
// unkeyed for ordinary content, keyed under a fixed test key for secrets.
func semanticsAt(row fileCase) FileSemantics {
	semantics := FileSemantics{Source: digestOf(row.source), Target: digestOf(row.target)}
	if row.secret {
		semantics = FileSemantics{Source: keyed(row.source), Target: keyed(row.target)}
	}
	return semantics
}

// targetAt freezes a target snapshot with the kind, bytes, and mode of a row.
func targetAt(kind EntryKind, bytes []byte, mode fs.FileMode) TargetSnapshot {
	snapshot := TargetSnapshot{kind: kind, mode: mode}
	if kind == KindFile {
		snapshot.token = TokenOfContent(bytes)
		snapshot.digest = deployment.Ordinary(bytes)
	}
	return snapshot
}

// ordinarySource freezes an ordinary source observation around its bytes.
func ordinarySource(bytes []byte, executable fs.FileMode) SourceObservation {
	return SourceObservation{snapshot: SourceSnapshot{kind: KindFile,
		token: TokenOfContent(bytes), semantic: deployment.Ordinary(bytes), executable: executable & 0o111}}
}

// secretSource freezes a secret source observation around its ciphertext.
func secretSource(cipher []byte, executable fs.FileMode) SourceObservation {
	return SourceObservation{secret: true, snapshot: SourceSnapshot{kind: KindFile,
		token: TokenOfContent(cipher), storage: deployment.RawStorage(cipher), executable: executable & 0o111}}
}

// stateAt builds one active ordinary file baseline over exact byte digests.
func stateAt(content, source []byte, bits fs.FileMode) *FileState {
	return &FileState{targetPath: "a.conf", sourceKind: deployment.FileOrdinary,
		baselineContent: digestOf(content), baselineSource: digestOf(source), executableBits: bits, active: true}
}

// secretStateAt builds one active secret baseline: keyed plaintext content
// fingerprint and unkeyed ciphertext storage fingerprint.
func secretStateAt(plain, cipher []byte, bits fs.FileMode) *FileState {
	return &FileState{targetPath: "a.conf", sourceKind: deployment.FileSecret,
		baselineContent: keyed(plain), baselineSource: deployment.RawStorage(cipher), executableBits: bits, active: true}
}

// retired returns an inactive copy of a state row.
func retired(row *FileState) *FileState {
	copy := *row
	copy.active = false
	return &copy
}

// keyed fingerprints plaintext under the fixed zero test key.
func keyed(bytes []byte) deployment.Digest {
	return deployment.SecretSemantic(bytes, [32]byte{})
}

// digestOf fingerprints ordinary bytes unkeyed.
func digestOf(bytes []byte) deployment.Digest {
	return deployment.Ordinary(bytes)
}

var (
	sourceA = []byte("config a")
	sourceB = []byte("config b")
	plainA  = []byte("secret a")
	plainB  = []byte("secret b")
	plainC  = []byte("secret c")
	targetX = []byte("target x")
	cipherA = []byte(`{"data":"aA==","sops":{"version":"3.9.0"}}`)
	cipherB = []byte(`{"data":"bB==","sops":{"version":"3.9.0"}}`)
)

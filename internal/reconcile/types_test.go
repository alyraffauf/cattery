package reconcile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func TestReconciliationContract(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"enum validity", testEnumValidity},
		{"digest width", testDigestWidth},
		{"content token equality", testContentToken},
		{"decision specs copy choices", testDecisionSpecCopy},
		{"state snapshots copy records", testStateSnapshotCopy},
		{"target snapshot accessors", testSnapshotAccessors},
		{"source snapshot accessors", testSourceSnapshotAccessors},
		{"zero-value target snapshot", testTargetZeroValue},
		{"no provider-owned interface", testNoProviderInterface},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testEnumValidity(t *testing.T) {
	actions := []Action{
		ActionNoOp, ActionCorrectMode, ActionCreateTarget, ActionWriteSourceToTarget,
		ActionEstablishBaseline, ActionNeedsDecision, ActionRetireState, ActionCreateAlias,
		ActionReplaceAlias, ActionVerifyAlias, ActionRetireAliasState,
	}
	for _, action := range actions {
		if !action.Valid() {
			t.Fatalf("known action %d must be valid", action)
		}
	}
	reasons := []Reason{
		ReasonNoChange, ReasonModeCorrection, ReasonSourceChanged, ReasonTargetDrift,
		ReasonAlreadyConverged, ReasonConflict, ReasonUnbaselinedAbsent, ReasonUnbaselinedEqual,
		ReasonUnbaselinedDiffer, ReasonUnexpectedTargetType, ReasonSourceRemoved, ReasonAliasExact,
		ReasonAliasWrong, ReasonAliasOccupied, ReasonRepresentationIntact, ReasonRepresentationDrift,
		ReasonInactivePlatform, ReasonAlreadyRetired,
	}
	for _, reason := range reasons {
		if !reason.Valid() {
			t.Fatalf("known reason %d must be valid", reason)
		}
	}
	testSmallEnumValidity(t)
}

func testSmallEnumValidity(t *testing.T) {
	kinds := []EntryKind{KindAbsent, KindFile, KindDirectory, KindSymlink, KindSpecial}
	for _, kind := range kinds {
		if !kind.Valid() {
			t.Fatalf("known kind %d must be valid", kind)
		}
	}
	convergences := []Convergence{Converged, ActionPending, DecisionRequired, Rejected}
	for _, convergence := range convergences {
		if !convergence.Valid() {
			t.Fatalf("known convergence %d must be valid", convergence)
		}
	}
	choices := []DecisionChoice{ChoiceOverwrite, ChoiceSkip, ChoiceAbort, ChoiceDiff}
	for _, choice := range choices {
		if !choice.Valid() {
			t.Fatalf("known choice %d must be valid", choice)
		}
	}
	for _, invalid := range []bool{
		Action(99).Valid(), Reason(99).Valid(), Convergence(99).Valid(),
		DecisionChoice(99).Valid(), EntryKind(99).Valid(),
	} {
		if invalid {
			t.Fatal("unknown enum values must be invalid")
		}
	}
}

func testDigestWidth(t *testing.T) {
	var digest deployment.Digest
	if len(digest) != 32 {
		t.Fatalf("deployment.Digest width = %d, want 32", len(digest))
	}
	if len(ContentToken{}) != 32 {
		t.Fatalf("ContentToken width = %d, want 32", len(ContentToken{}))
	}
}

func testContentToken(t *testing.T) {
	token := TokenOfContent([]byte("payload"))
	if token != TokenOfContent([]byte("payload")) {
		t.Fatal("equal bytes must produce equal tokens")
	}
	if TokenOfContent(nil) != TokenOfContent([]byte{}) {
		t.Fatal("nil and empty bytes must share one token")
	}
	if token == TokenOfContent([]byte("other")) {
		t.Fatal("distinct bytes must produce distinct tokens")
	}
}

func testDecisionSpecCopy(t *testing.T) {
	candidate := DecisionSpec{
		TargetPath: ".config/app/config",
		Action:     ActionNeedsDecision,
		Reason:     ReasonTargetDrift,
		Choices:    []DecisionChoice{ChoiceOverwrite, ChoiceSkip, ChoiceAbort, ChoiceDiff},
	}
	spec, err := NewDecisionSpec(candidate)
	if err != nil {
		t.Fatalf("NewDecisionSpec: %v", err)
	}
	candidate.Choices[0] = ChoiceDiff
	if len(spec.Choices) != 4 || spec.Choices[0] != ChoiceOverwrite {
		t.Fatal("spec must copy its choice slice defensively")
	}
	choices := spec.AllChoices()
	choices[0] = ChoiceAbort
	if spec.Choices[0] != ChoiceOverwrite {
		t.Fatal("AllChoices must return an independent copy")
	}
}

func testStateSnapshotCopy(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	files := []FileState{{TargetPath: "git/config", Active: true, RetiredAt: &when}}
	aliases := []AliasState{{AliasPath: "bin/x", Active: true, RetiredAt: &when}}
	snapshot := StateSnapshot{RepositoryRoot: "/repo", HomePath: "/home", Files: files, Aliases: aliases}
	copiedFiles := snapshot.AllFiles()
	copiedFiles[0].TargetPath = "moved"
	*copiedFiles[0].RetiredAt = time.Time{}
	copiedAliases := snapshot.AllAliases()
	copiedAliases[0].AliasPath = "moved"
	*copiedAliases[0].RetiredAt = time.Time{}
	if files[0].TargetPath != "git/config" || !files[0].RetiredAt.Equal(when) {
		t.Fatal("source file record mutated through its defensive copy")
	}
	if aliases[0].AliasPath != "bin/x" || !aliases[0].RetiredAt.Equal(when) {
		t.Fatal("source alias record mutated through its defensive copy")
	}
	if len(snapshot.Files) != 1 || len(snapshot.Aliases) != 1 {
		t.Fatal("snapshot records must survive copying")
	}
	if (StateSnapshot{}).AllFiles() != nil || (StateSnapshot{}).AllAliases() != nil {
		t.Fatal("zero-value snapshot must copy to nil")
	}
}

func testSnapshotAccessors(t *testing.T) {
	snapshot := TargetSnapshot{
		destination: Destination{Root: "/home", Relative: "app.conf"},
		parent:      pathsafe.Identity{},
		identity:    pathsafe.Identity{},
		kind:        KindFile, token: TokenOfContent([]byte("x")),
		digest: deployment.Ordinary([]byte("x")), mode: 0o755, payload: "payload",
	}
	if snapshot.Destination() != snapshot.destination {
		t.Fatal("Destination must echo its field")
	}
	if snapshot.Kind() != snapshot.kind || snapshot.Mode() != snapshot.mode {
		t.Fatal("kind and mode must echo their fields")
	}
	if snapshot.Token() != snapshot.token || snapshot.Digest() != snapshot.digest {
		t.Fatal("hashes must echo their fields")
	}
	if snapshot.Payload() != snapshot.payload {
		t.Fatal("payload must echo its field")
	}
	if snapshot.Parent() != snapshot.parent || snapshot.Identity() != snapshot.identity {
		t.Fatal("identities must echo their fields")
	}
}

func testSourceSnapshotAccessors(t *testing.T) {
	source := SourceSnapshot{
		path:       "/repo/files/x",
		kind:       KindFile,
		token:      TokenOfContent([]byte("x")),
		semantic:   deployment.Ordinary([]byte("x")),
		storage:    deployment.RawStorage([]byte("x")),
		executable: 0o111,
	}
	if source.Path() != source.path || source.Kind() != source.kind {
		t.Fatal("path and kind must echo their fields")
	}
	if source.Token() != source.token || source.Semantic() != source.semantic {
		t.Fatal("token and semantic must echo their fields")
	}
	if source.Storage() != source.storage || source.Executable() != source.executable {
		t.Fatal("storage and executable must echo their fields")
	}
}

func testTargetZeroValue(t *testing.T) {
	var zero TargetSnapshot
	if zero.Kind() != KindAbsent || zero.Identity().Path() != "" || zero.Parent().Path() != "" {
		t.Fatal("zero-value target snapshot must report absent facts")
	}
	if zero.Token() != (ContentToken{}) || zero.Digest() != (deployment.Digest{}) {
		t.Fatal("zero-value target snapshot must carry zero hashes")
	}
	if zero.Mode() != 0 || zero.Payload() != "" || zero.Destination() != (Destination{}) {
		t.Fatal("zero-value target snapshot must carry zero facts")
	}
}

func testNoProviderInterface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if fileDeclaresInterface(t, entry.Name()) {
			t.Fatalf("%s declares an interface; reconcile holds no provider-owned interface", entry.Name())
		}
	}
}

func fileDeclaresInterface(t *testing.T, name string) bool {
	file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		if _, ok := node.(*ast.InterfaceType); ok {
			found = true
			return false
		}
		return true
	})
	return found
}

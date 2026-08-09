// Package reconcile owns the immutable evaluation records and precondition
// vocabulary of the snapshot pipeline (PLAN.md Sections 9 and 12.4); no
// provider-owned interface lives in this package.
package reconcile

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/state"
)

// Destination names one HOME-relative target beneath a canonical root.
type Destination struct {
	Root     string
	Relative string
}

// EntryKind names the object type observed at one destination path.
type EntryKind int

const (
	KindAbsent EntryKind = iota
	KindFile
	KindDirectory
	KindSymlink
	KindSpecial
)

func (k EntryKind) Valid() bool { return k >= KindAbsent && k <= KindSpecial }

// ContentToken is an immutable digest of exact bytes.
type ContentToken [32]byte

// TokenOfContent derives the content token of data.
func TokenOfContent(data []byte) ContentToken { return sha256.Sum256(data) }

// Action names the reconciliation step a classified representation requires.
type Action int

const (
	ActionNoOp Action = iota
	ActionCorrectMode
	ActionCreateTarget
	ActionWriteSourceToTarget
	ActionEstablishBaseline
	ActionNeedsDecision
	ActionRetireState
	ActionCreateAlias
	ActionReplaceAlias
	ActionVerifyAlias
	ActionRetireAliasState
)

func (a Action) Valid() bool { return a >= ActionNoOp && a <= ActionRetireAliasState }

// Reason explains why a classification chose an action for one representation.
type Reason int

const (
	ReasonNoChange Reason = iota
	ReasonModeCorrection
	ReasonSourceChanged
	ReasonTargetDrift
	ReasonAlreadyConverged
	ReasonConflict
	ReasonUnbaselinedAbsent
	ReasonUnbaselinedEqual
	ReasonUnbaselinedDiffer
	ReasonUnexpectedTargetType
	ReasonSourceRemoved
	ReasonAliasExact
	ReasonAliasWrong
	ReasonAliasOccupied
	ReasonRepresentationIntact
	ReasonRepresentationDrift
	ReasonInactivePlatform
	ReasonAlreadyRetired
)

func (r Reason) Valid() bool { return r >= ReasonNoChange && r <= ReasonAlreadyRetired }

// Convergence names the outcome class of one classified representation.
type Convergence int

const (
	Converged Convergence = iota
	ActionPending
	DecisionRequired
	Rejected
)

func (c Convergence) Valid() bool { return c >= Converged && c <= Rejected }

// DecisionChoice names one answer a user may give to a resolution prompt.
type DecisionChoice int

const (
	ChoiceOverwrite DecisionChoice = iota
	ChoiceSkip
	ChoiceAbort
	ChoiceDiff
)

func (c DecisionChoice) Valid() bool { return c >= ChoiceOverwrite && c <= ChoiceDiff }

// DecisionSpec is one immutable, ordered resolution request: the target path,
// the action and reason that produced it, and the choices the user may pick.
type DecisionSpec struct {
	TargetPath string
	Action     Action
	Reason     Reason
	Choices    []DecisionChoice
}

// NewDecisionSpec validates candidate and returns a spec whose choice slice
// is a defensive copy, so caller-owned slices never mutate the spec.
func NewDecisionSpec(candidate DecisionSpec) (DecisionSpec, error) {
	if !state.IsSlashRelative(candidate.TargetPath) {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision target %q is not a slash-relative path", candidate.TargetPath)
	}
	if !candidate.Action.Valid() {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision spec has invalid action %d", candidate.Action)
	}
	if !candidate.Reason.Valid() {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision spec has invalid reason %d", candidate.Reason)
	}
	if len(candidate.Choices) == 0 {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision spec for %q has no choices", candidate.TargetPath)
	}
	for _, choice := range candidate.Choices {
		if !choice.Valid() {
			return DecisionSpec{}, fmt.Errorf("reconcile: decision spec for %q has invalid choice %d", candidate.TargetPath, choice)
		}
	}
	candidate.Choices = append([]DecisionChoice(nil), candidate.Choices...)
	return candidate, nil
}

// AllChoices returns a defensive copy of the resolution choices.
func (s DecisionSpec) AllChoices() []DecisionChoice {
	return append([]DecisionChoice(nil), s.Choices...)
}

// SourceSnapshot freezes the immutable facts of one deployment source.
type SourceSnapshot struct {
	path       string
	identity   pathsafe.Identity
	kind       EntryKind
	token      ContentToken
	semantic   deployment.Digest
	storage    deployment.Digest
	executable fs.FileMode
}

func (s SourceSnapshot) Path() string                { return s.path }
func (s SourceSnapshot) Identity() pathsafe.Identity { return s.identity }
func (s SourceSnapshot) Kind() EntryKind             { return s.kind }
func (s SourceSnapshot) Token() ContentToken         { return s.token }
func (s SourceSnapshot) Semantic() deployment.Digest { return s.semantic }
func (s SourceSnapshot) Storage() deployment.Digest  { return s.storage }
func (s SourceSnapshot) Executable() fs.FileMode     { return s.executable }

// TargetSnapshot freezes the immutable facts of one destination observation
// and doubles as the immutable target precondition (PLAN.md Section 12.4).
type TargetSnapshot struct {
	destination Destination
	parent      pathsafe.Identity
	kind        EntryKind
	identity    pathsafe.Identity
	token       ContentToken
	digest      deployment.Digest
	mode        fs.FileMode
	payload     string
}

func (t TargetSnapshot) Destination() Destination    { return t.destination }
func (t TargetSnapshot) Parent() pathsafe.Identity   { return t.parent }
func (t TargetSnapshot) Kind() EntryKind             { return t.kind }
func (t TargetSnapshot) Identity() pathsafe.Identity { return t.identity }
func (t TargetSnapshot) Token() ContentToken         { return t.token }
func (t TargetSnapshot) Digest() deployment.Digest   { return t.digest }
func (t TargetSnapshot) Mode() fs.FileMode           { return t.mode }
func (t TargetSnapshot) Payload() string             { return t.payload }

// FileState is one immutable evaluation record of a persisted file row.
type FileState struct {
	TargetPath      string
	GroupName       string
	SourcePath      string
	SourceKind      deployment.FileKind
	Layer           deployment.Layer
	BaselineContent deployment.Digest
	BaselineSource  deployment.Digest
	ExecutableBits  fs.FileMode
	Active          bool
	RetiredAt       *time.Time
}

// AliasState is one immutable evaluation record of a persisted alias row.
type AliasState struct {
	AliasPath           string
	CanonicalTargetPath string
	GroupName           string
	Layer               state.AliasLayer
	Active              bool
	RetiredAt           *time.Time
}

// StateSnapshot is the immutable evaluation snapshot of every file and alias
// row of one canonical repository pair.
type StateSnapshot struct {
	RepositoryRoot string
	HomePath       string
	Files          []FileState
	Aliases        []AliasState
}

// cloneRecords returns a defensive copy of rows, cloning retirement
// timestamps so callers cannot mutate state-owned pointers.
func cloneRecords[T any](rows []T, clone func(*T)) []T {
	if rows == nil {
		return nil
	}
	out := make([]T, len(rows))
	copy(out, rows)
	for index := range out {
		clone(&out[index])
	}
	return out
}

// AllFiles returns a defensive copy of the file records.
func (s StateSnapshot) AllFiles() []FileState {
	return cloneRecords(s.Files, func(record *FileState) {
		record.RetiredAt = state.CloneTimestamp(record.RetiredAt)
	})
}

// AllAliases returns a defensive copy of the alias records.
func (s StateSnapshot) AllAliases() []AliasState {
	return cloneRecords(s.Aliases, func(record *AliasState) {
		record.RetiredAt = state.CloneTimestamp(record.RetiredAt)
	})
}

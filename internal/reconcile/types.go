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
	ActionRetireFileState
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
	ConvergenceConverged Convergence = iota
	ConvergencePending
	ConvergenceDecisionRequired
	ConvergenceRejected
)

func (c Convergence) Valid() bool { return c >= ConvergenceConverged && c <= ConvergenceRejected }

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
	targetPath string
	action     Action
	reason     Reason
	choices    []DecisionChoice
}

// DecisionSpecInput is the mutable input accepted by NewDecisionSpec.
type DecisionSpecInput struct {
	TargetPath string
	Action     Action
	Reason     Reason
	Choices    []DecisionChoice
}

// NewDecisionSpec validates candidate and returns a spec whose choice slice
// is a defensive copy, so caller-owned slices never mutate the spec.
func NewDecisionSpec(input DecisionSpecInput) (DecisionSpec, error) {
	if !state.IsSlashRelative(input.TargetPath) {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision target %q is not a slash-relative path", input.TargetPath)
	}
	if !input.Action.Valid() {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision spec has invalid action %d", input.Action)
	}
	if !input.Reason.Valid() {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision spec has invalid reason %d", input.Reason)
	}
	if len(input.Choices) == 0 {
		return DecisionSpec{}, fmt.Errorf("reconcile: decision spec for %q has no choices", input.TargetPath)
	}
	for _, choice := range input.Choices {
		if !choice.Valid() {
			return DecisionSpec{}, fmt.Errorf("reconcile: decision spec for %q has invalid choice %d", input.TargetPath, choice)
		}
	}
	return DecisionSpec{targetPath: input.TargetPath, action: input.Action, reason: input.Reason, choices: append([]DecisionChoice(nil), input.Choices...)}, nil
}

func (spec DecisionSpec) TargetPath() string { return spec.targetPath }
func (spec DecisionSpec) Action() Action     { return spec.action }
func (spec DecisionSpec) Reason() Reason     { return spec.reason }

// AllChoices returns a defensive copy of the resolution choices.
func (spec DecisionSpec) AllChoices() []DecisionChoice {
	return append([]DecisionChoice(nil), spec.choices...)
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

func (snapshot SourceSnapshot) Path() string                { return snapshot.path }
func (snapshot SourceSnapshot) Identity() pathsafe.Identity { return snapshot.identity }
func (snapshot SourceSnapshot) Kind() EntryKind             { return snapshot.kind }
func (snapshot SourceSnapshot) Token() ContentToken         { return snapshot.token }

// Semantic returns the ordinary semantic digest; secret snapshots leave it zero.
func (snapshot SourceSnapshot) Semantic() deployment.Digest { return snapshot.semantic }

// Storage returns the encrypted storage digest; ordinary snapshots leave it zero.
func (snapshot SourceSnapshot) Storage() deployment.Digest { return snapshot.storage }
func (snapshot SourceSnapshot) Executable() fs.FileMode    { return snapshot.executable }

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

func (snapshot TargetSnapshot) Destination() Destination    { return snapshot.destination }
func (snapshot TargetSnapshot) Parent() pathsafe.Identity   { return snapshot.parent }
func (snapshot TargetSnapshot) Kind() EntryKind             { return snapshot.kind }
func (snapshot TargetSnapshot) Identity() pathsafe.Identity { return snapshot.identity }
func (snapshot TargetSnapshot) Token() ContentToken         { return snapshot.token }
func (snapshot TargetSnapshot) Digest() deployment.Digest   { return snapshot.digest }
func (snapshot TargetSnapshot) Mode() fs.FileMode           { return snapshot.mode }
func (snapshot TargetSnapshot) Payload() string             { return snapshot.payload }

// FileState is one immutable evaluation record of a persisted file row.
type FileState struct {
	targetPath      string
	groupName       string
	sourcePath      string
	sourceKind      deployment.FileKind
	layer           deployment.Layer
	baselineContent deployment.Digest
	baselineSource  deployment.Digest
	executableBits  fs.FileMode
	active          bool
	retiredAt       *time.Time
}

func (fileState FileState) TargetPath() string                 { return fileState.targetPath }
func (fileState FileState) GroupName() string                  { return fileState.groupName }
func (fileState FileState) SourcePath() string                 { return fileState.sourcePath }
func (fileState FileState) SourceKind() deployment.FileKind    { return fileState.sourceKind }
func (fileState FileState) Layer() deployment.Layer            { return fileState.layer }
func (fileState FileState) BaselineContent() deployment.Digest { return fileState.baselineContent }
func (fileState FileState) BaselineSource() deployment.Digest  { return fileState.baselineSource }
func (fileState FileState) ExecutableBits() fs.FileMode        { return fileState.executableBits }
func (fileState FileState) Active() bool                       { return fileState.active }
func (fileState FileState) RetiredAt() *time.Time              { return state.CloneTimestamp(fileState.retiredAt) }

// AliasState is one immutable evaluation record of a persisted alias row.
type AliasState struct {
	aliasPath           string
	canonicalTargetPath string
	groupName           string
	layer               state.AliasLayer
	active              bool
	retiredAt           *time.Time
}

func (aliasState AliasState) AliasPath() string           { return aliasState.aliasPath }
func (aliasState AliasState) CanonicalTargetPath() string { return aliasState.canonicalTargetPath }
func (aliasState AliasState) GroupName() string           { return aliasState.groupName }
func (aliasState AliasState) Layer() state.AliasLayer     { return aliasState.layer }
func (aliasState AliasState) Active() bool                { return aliasState.active }
func (aliasState AliasState) RetiredAt() *time.Time {
	return state.CloneTimestamp(aliasState.retiredAt)
}

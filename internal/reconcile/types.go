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

func (s DecisionSpec) TargetPath() string { return s.targetPath }
func (s DecisionSpec) Action() Action     { return s.action }
func (s DecisionSpec) Reason() Reason     { return s.reason }

// AllChoices returns a defensive copy of the resolution choices.
func (s DecisionSpec) AllChoices() []DecisionChoice {
	return append([]DecisionChoice(nil), s.choices...)
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

func (s FileState) TargetPath() string                 { return s.targetPath }
func (s FileState) GroupName() string                  { return s.groupName }
func (s FileState) SourcePath() string                 { return s.sourcePath }
func (s FileState) SourceKind() deployment.FileKind    { return s.sourceKind }
func (s FileState) Layer() deployment.Layer            { return s.layer }
func (s FileState) BaselineContent() deployment.Digest { return s.baselineContent }
func (s FileState) BaselineSource() deployment.Digest  { return s.baselineSource }
func (s FileState) ExecutableBits() fs.FileMode        { return s.executableBits }
func (s FileState) Active() bool                       { return s.active }
func (s FileState) RetiredAt() *time.Time              { return state.CloneTimestamp(s.retiredAt) }

// AliasState is one immutable evaluation record of a persisted alias row.
type AliasState struct {
	aliasPath           string
	canonicalTargetPath string
	groupName           string
	layer               state.AliasLayer
	active              bool
	retiredAt           *time.Time
}

func (s AliasState) AliasPath() string           { return s.aliasPath }
func (s AliasState) CanonicalTargetPath() string { return s.canonicalTargetPath }
func (s AliasState) GroupName() string           { return s.groupName }
func (s AliasState) Layer() state.AliasLayer     { return s.layer }
func (s AliasState) Active() bool                { return s.active }
func (s AliasState) RetiredAt() *time.Time       { return state.CloneTimestamp(s.retiredAt) }

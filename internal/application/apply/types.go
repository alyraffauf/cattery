// Package apply implements `cattery apply`: the
// target-mutating deployment path with pre-decision collection, hook-gated
// source revalidation, and per-target execution preconditions. The package
// is Cobra-free; the CLI talks to the service through the frozen Request,
// DecisionRequest, and Result shapes below.
package apply

import (
	"context"
	"fmt"

	"github.com/alyraffauf/cattery/internal/application/evaluation"
	"github.com/alyraffauf/cattery/internal/application/outcome"
	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/hooks"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/state"
)

// Dependencies bundles the injectable seams of the apply service: repository
// resolution, plan compilation, state reads and writes, SOPS, atomic
// replacement, hook execution, and the dependency probe. Construction is
// side-effect-free; effects begin inside Apply.
type Dependencies struct {
	RepositorySource RepositorySource
	Compiler         Compiler
	State            StateReader
	Baselines        BaselineStore
	Transitions      TransitionStore
	Retirements      RetirementStore
	Client           SecretClient
	Secrets          *secrets.Client
	Replacer         AtomicReplacer
	Hooks            HookExecutor
	Probe            DependencyProbe
	Resolver         DecisionResolver
	ProtectedTrees   []string
	Platform         string
}

// RepositorySource resolves the canonical repository pair for a selection
// request. The composition root satisfies it with a selection resolver bound
// to the canonical home and the state default lookup.
type RepositorySource = applicationrepository.RepositorySource

// RepositoryIdentity is the canonical repository pair one apply compiles from.
type RepositoryIdentity = applicationrepository.RepositoryIdentity

// Compiler compiles the current-platform plan from a repository.
type Compiler = applicationrepository.Compiler

// StateReader is the narrow read-only port over the persisted rows and the
// per-installation secret hash key of one repository pair. It never
// registers, retires, or mutates rows.
type StateReader = evaluation.StateReader

// BaselineStore establishes or replaces the equal source/target baseline
// after a durable write, for both file and alias rows.
type BaselineStore interface {
	UpsertFileBaseline(root, home string, baseline state.FileBaseline) (state.FileBaseline, error)
	UpsertAliasBaseline(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error)
}

// TransitionStore atomically switches the active representation of one path
// between the file and alias tables.
type TransitionStore interface {
	TransitionToAlias(root, home string, baseline state.AliasBaseline) (state.AliasBaseline, error)
	TransitionToFile(root, home string, baseline state.FileBaseline) (state.FileBaseline, error)
}

// RetirementStore retires the tracking row of one removed source.
type RetirementStore interface {
	RetireFileBaseline(root, home, target string) (state.FileBaseline, error)
	RetireAliasBaseline(root, home, aliasPath string) (state.AliasBaseline, error)
}

// SecretClient validates an encrypted candidate round trip during apply.
type SecretClient interface {
	ValidateCandidate(context.Context, secrets.Candidate) ([]byte, error)
}

// AtomicReplacer durably replaces one target or alias entry from exact
// validated bytes without touching unmanaged inode aliases.
type AtomicReplacer interface {
	ReplaceResult(context.Context, filesystem.Precondition, filesystem.ReplacementSpec) (filesystem.ReplaceResult, error)
	RealizeAlias(context.Context, filesystem.Precondition, filesystem.AliasSpec) (filesystem.AliasRealization, error)
}

// HookExecutor runs the ordered trusted hooks of one phase with the exact
// apply result environment.
type HookExecutor interface {
	Execute(context.Context, hooks.ExecuteInput, []deployment.Hook) error
}

// DependencyProbe verifies the SOPS dependency before any write begins.
type DependencyProbe interface {
	Probe(context.Context) error
}

// DecisionResolver resolves one frozen decision request; presentation IO
// lives in the CLI adapter, and policy stays in the service.
type DecisionResolver interface {
	Resolve(context.Context, DecisionRequest) (DecisionResponse, error)
}

// DifferenceProvider renders one safe difference for the current apply
// candidate. It returns false when the target cannot produce a difference.
type DifferenceProvider func(context.Context, string) (SafeDifference, bool)

// DifferenceResolver is the optional prompt seam for resolvers that can show
// a safe difference before asking for the final decision.
type DifferenceResolver interface {
	ResolveWithDifference(context.Context, DecisionRequest, DifferenceProvider) (DecisionResponse, error)
}

// RepositoryInput carries the raw repository fields the CLI adapter copies
// mechanically: the explicit --repo value and its presence, the raw
// CATTERY_REPO value and its presence, and the initial working directory for
// relative resolution. Presence is significant: an empty value with presence
// blocks fallback.
type RepositoryInput = applicationrepository.RepositoryInput

// Request is the frozen input of one apply: the raw repository fields, the
// raw group arguments in command-line order, and the policy flags.
type Request struct {
	Repository     RepositoryInput
	Groups         []string
	DryRun         bool
	NonInteractive bool
	NoHooks        bool
	SkipSecrets    bool
}

// DecisionChoice is the application-owned choice vocabulary one prompt may
// resolve. It mirrors the reconcile choices without exposing them.
type DecisionChoice string

const (
	// ChoiceOverwrite replaces the drifted target.
	ChoiceOverwrite DecisionChoice = "overwrite"
	// ChoiceSkip leaves the target and continues.
	ChoiceSkip DecisionChoice = "skip"
	// ChoiceAbort stops the whole apply.
	ChoiceAbort DecisionChoice = "abort"
	// ChoiceDiff shows the safe source/target difference first.
	ChoiceDiff DecisionChoice = "diff"
)

// DecisionRequest is the frozen prompt request the CLI resolves: the
// HOME-relative target path and the allowed choices projected from the
// reconcile decision spec. Only application-owned types appear.
type DecisionRequest struct {
	targetPath string
	choices    []DecisionChoice
}

// DecisionRequestInput carries the projection fields of one prompt request.
type DecisionRequestInput struct {
	TargetPath string
	Choices    []DecisionChoice
}

// NewDecisionRequest validates candidate field-by-field and freezes it. The
// returned request shares no storage with input.
func NewDecisionRequest(candidate DecisionRequestInput) (DecisionRequest, error) {
	if candidate.TargetPath == "" {
		return DecisionRequest{}, fmt.Errorf("apply: decision request has empty target path")
	}
	if len(candidate.Choices) == 0 {
		return DecisionRequest{}, fmt.Errorf("apply: decision request for %q has no choices", candidate.TargetPath)
	}
	for _, choice := range candidate.Choices {
		if !validChoice(choice) {
			return DecisionRequest{}, fmt.Errorf("apply: decision request for %q has invalid choice %q", candidate.TargetPath, choice)
		}
	}
	return DecisionRequest{targetPath: candidate.TargetPath, choices: append([]DecisionChoice(nil), candidate.Choices...)}, nil
}

func validChoice(choice DecisionChoice) bool {
	switch choice {
	case ChoiceOverwrite, ChoiceSkip, ChoiceAbort, ChoiceDiff:
		return true
	}
	return false
}

// TargetPath returns the frozen target path.
func (r DecisionRequest) TargetPath() string { return r.targetPath }

// Choices returns a defensive copy of the allowed choices.
func (r DecisionRequest) Choices() []DecisionChoice {
	return append([]DecisionChoice(nil), r.choices...)
}

// DecisionResponse is the frozen prompt response the CLI returns.
type DecisionResponse struct {
	Choice DecisionChoice
}

// DiffTag names the safe difference classes the CLI may render. It mirrors
// the diff record tags without exposing them.
type DiffTag string

const (
	// DiffTagNone marks a record with no renderable difference.
	DiffTagNone DiffTag = "none"
	// DiffTagText marks a unified text difference.
	DiffTagText DiffTag = "text"
	// DiffTagBinary marks a size/hash-only binary difference.
	DiffTagBinary DiffTag = "binary"
	// DiffTagSecret marks a secret record with no payload.
	DiffTagSecret DiffTag = "secret"
)

// SafeDifference is the frozen safe difference response projected from the
// diff record: renderable text lines plus size and hash labels, with hashes
// as hexadecimal strings so no backend digest type escapes.
type SafeDifference struct {
	Tag        DiffTag
	SourceSize int
	TargetSize int
	SourceHash string
	TargetHash string
	Lines      []string
}

// LinesCopy returns a defensive copy of the rendered text lines.
func (d SafeDifference) LinesCopy() []string {
	return append([]string(nil), d.Lines...)
}

// ActionKind names one apply action class.
type ActionKind string

const (
	// ActionKindWriteSource writes validated source bytes to the target.
	ActionKindWriteSource ActionKind = "write-source"
	// ActionKindReplaceFile replaces one regular target file.
	ActionKindReplaceFile ActionKind = "replace-file"
	// ActionKindRealizeAlias creates or replaces one symlink entry.
	ActionKindRealizeAlias ActionKind = "realize-alias"
	// ActionKindRetireFile retires one removed file row.
	ActionKindRetireFile ActionKind = "retire-file"
	// ActionKindRetireAlias retires one removed alias row.
	ActionKindRetireAlias ActionKind = "retire-alias"
	// ActionKindTransitionToAlias switches one file row to an alias row.
	ActionKindTransitionToAlias ActionKind = "transition-to-alias"
	// ActionKindTransitionToFile switches one alias row to a file row.
	ActionKindTransitionToFile ActionKind = "transition-to-file"
)

// PlanAction is one immutable apply action: the HOME-relative target, the
// action class, the repository-relative source for content actions, and
// whether an occupied alias path was confirmed for replacement.
type PlanAction struct {
	TargetPath string
	Kind       ActionKind
	SourcePath string
	Overwrite  bool
}

// ActionPlan freezes the ordered execution actions of one apply defensively.
type ActionPlan struct {
	actions []PlanAction
}

// NewActionPlan freezes the ordered actions with a defensive copy.
func NewActionPlan(actions []PlanAction) ActionPlan {
	return ActionPlan{actions: append([]PlanAction(nil), actions...)}
}

// Items returns a defensive copy of the ordered execution actions.
func (p ActionPlan) Items() []PlanAction {
	return append([]PlanAction(nil), p.actions...)
}

// ItemStatus marks the outcome of one per-target apply record.
type ItemStatus = outcome.ItemStatus

const (
	StatusPlanned   = outcome.StatusPlanned
	StatusCompleted = outcome.StatusCompleted
	StatusPartial   = outcome.StatusPartial
)

// ItemResult is one per-target apply record: the HOME-relative target, the
// outcome status, the storage kind, and the action class.
type ItemResult struct {
	TargetPath string
	Status     ItemStatus
	Kind       ActionKind
}

// Summary counts the per-target outcome records of one apply.
type Summary = outcome.Summary

// Result is the frozen outcome of one apply: the target-sorted per-target
// records and the outcome counts.
type Result struct {
	Items   []ItemResult
	Summary Summary
}

// ItemsCopy returns a defensive copy of the per-target records.
func (r Result) ItemsCopy() []ItemResult {
	return append([]ItemResult(nil), r.Items...)
}

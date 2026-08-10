// Package add implements `cattery add` (PLAN.md Section 11.6): the sole
// target-to-repository content path. Each regular file beneath $HOME is
// adopted as an ordinary or SOPS-encrypted source under the root scope or an
// explicit group, and the repository recompiles to the same target. The
// package is Cobra-free: no CLI type appears here, and the CLI talks to the
// service through the frozen Request and Result shapes below.
package add

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/alyraffauf/cattery/internal/application/outcome"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

// Dependencies bundles the injectable seams of the add service: repository
// resolution, plan compilation, atomic source replacement, and baseline
// persistence. Construction is side-effect-free; effects begin inside Add.
type Dependencies struct {
	RepositorySource RepositorySource
	Compiler         Compiler
	Writer           AtomicWriter
	Baselines        BaselineStore
}

// RepositorySource resolves the canonical repository pair for a selection
// request. The composition root satisfies it with a selection resolver bound
// to the canonical home and the state default lookup.
type RepositorySource interface {
	Resolve(selection.RepositoryRequest) (RepositoryIdentity, error)
}

// RepositoryIdentity is the canonical repository pair one add compiles from.
type RepositoryIdentity struct {
	Root string
	Home string
}

// Compiler compiles the current-platform plan from a repository so add can
// prove a derived source recompiles to its target.
type Compiler interface {
	Compile(repository.CompileInput) (deployment.Plan, error)
}

// AtomicWriter durably replaces one source entry from exact validated bytes.
// The filesystem adapter satisfies it structurally; request and result values
// live in internal/filesystem, so the adapter never imports application.
type AtomicWriter interface {
	ReplaceResult(context.Context, filesystem.Precondition, filesystem.ReplacementSpec) (filesystem.ReplaceResult, error)
}

// BaselineStore establishes the equal source/target baseline after a write.
type BaselineStore interface {
	UpsertFileBaseline(root, home string, baseline state.FileBaseline) (state.FileBaseline, error)
}

// RepositoryInput carries the raw repository fields the CLI adapter copies
// mechanically: the explicit --repo value and its presence, the raw
// CATTERY_REPO value and its presence, and the initial working directory for
// relative resolution. Presence is significant: an empty value with presence
// blocks fallback.
type RepositoryInput struct {
	RawExplicit string
	ExplicitSet bool
	RawEnv      string
	EnvSet      bool
	WorkingDir  string
}

// Request is the frozen input of one add: the raw repository fields, the raw
// target arguments in command-line order, the explicit option values with
// separate presence bits, and the dry-run policy. An omitted --group,
// --platform, or --secret leaves its Set bit false so the service never
// mistakes it for an explicit ordinary/root/base request.
type Request struct {
	Repository  RepositoryInput
	Targets     []string
	Group       string
	GroupSet    bool
	Platform    string
	PlatformSet bool
	Secret      bool
	SecretSet   bool
	DryRun      bool
}

// ItemPlan is one immutable preflighted action for a single target: the
// inferred scope, layer, and kind, the canonical target identity, the
// repository source destination, and the executable bits preserved from the
// target.
type ItemPlan struct {
	scope                deployment.Scope
	layer                deployment.Layer
	kind                 deployment.FileKind
	targetAbsolutePath   string
	targetRelativePath   string
	sourceAbsolutePath   string
	sourceRepositoryPath string
	executableBits       fs.FileMode
}

// ItemPlanInput carries the validated candidate fields of one item plan.
type ItemPlanInput struct {
	Scope                deployment.Scope
	Layer                deployment.Layer
	Kind                 deployment.FileKind
	TargetAbsolutePath   string
	TargetRelativePath   string
	SourceAbsolutePath   string
	SourceRepositoryPath string
	ExecutableBits       fs.FileMode
}

// NewItemPlan validates candidate field-by-field and freezes it. The returned
// plan shares no storage with input.
func NewItemPlan(candidate ItemPlanInput) (ItemPlan, error) {
	if err := validateItemPlan(candidate); err != nil {
		return ItemPlan{}, err
	}
	return ItemPlan{
		scope:                candidate.Scope,
		layer:                candidate.Layer,
		kind:                 candidate.Kind,
		targetAbsolutePath:   candidate.TargetAbsolutePath,
		targetRelativePath:   candidate.TargetRelativePath,
		sourceAbsolutePath:   candidate.SourceAbsolutePath,
		sourceRepositoryPath: candidate.SourceRepositoryPath,
		executableBits:       candidate.ExecutableBits,
	}, nil
}

func validateItemPlan(candidate ItemPlanInput) error {
	if candidate.TargetAbsolutePath == "" {
		return fmt.Errorf("add: item plan has empty target absolute path")
	}
	if candidate.TargetRelativePath == "" {
		return fmt.Errorf("add: item plan has empty target relative path")
	}
	if candidate.SourceAbsolutePath == "" {
		return fmt.Errorf("add: item plan has empty source absolute path")
	}
	if candidate.SourceRepositoryPath == "" {
		return fmt.Errorf("add: item plan has empty source repository path")
	}
	if !candidate.Layer.Valid() {
		return fmt.Errorf("add: item plan has invalid layer %q", candidate.Layer)
	}
	if !candidate.Kind.Valid() {
		return fmt.Errorf("add: item plan has invalid kind %q", candidate.Kind)
	}
	if candidate.ExecutableBits&^deployment.ExecutableBitMask != 0 {
		return fmt.Errorf("add: item plan has invalid executable bits %o", candidate.ExecutableBits)
	}
	return nil
}

// Scope returns the frozen scope.
func (p ItemPlan) Scope() deployment.Scope { return p.scope }

// Layer returns the frozen layer.
func (p ItemPlan) Layer() deployment.Layer { return p.layer }

// Kind returns the frozen storage kind.
func (p ItemPlan) Kind() deployment.FileKind { return p.kind }

// TargetAbsolutePath returns the canonical absolute target path.
func (p ItemPlan) TargetAbsolutePath() string { return p.targetAbsolutePath }

// TargetRelativePath returns the HOME-relative target path.
func (p ItemPlan) TargetRelativePath() string { return p.targetRelativePath }

// SourceAbsolutePath returns the canonical absolute source destination.
func (p ItemPlan) SourceAbsolutePath() string { return p.sourceAbsolutePath }

// SourceRepositoryPath returns the repository-relative source destination.
func (p ItemPlan) SourceRepositoryPath() string { return p.sourceRepositoryPath }

// ExecutableBits returns the executable bits preserved from the target.
func (p ItemPlan) ExecutableBits() fs.FileMode { return p.executableBits }

// BatchPlan is the immutable plan of one complete add batch: the
// target-sorted items for display and a separate execution order that names
// every item exactly once.
type BatchPlan struct {
	items          []ItemPlan
	executionOrder []int
}

// BatchPlanInput carries the display-sorted items and the execution order.
type BatchPlanInput struct {
	Items          []ItemPlan
	ExecutionOrder []int
}

// NewBatchPlan validates the execution order and freezes both slices.
func NewBatchPlan(candidate BatchPlanInput) (BatchPlan, error) {
	if err := validateExecutionOrder(len(candidate.Items), candidate.ExecutionOrder); err != nil {
		return BatchPlan{}, err
	}
	return BatchPlan{
		items:          append([]ItemPlan(nil), candidate.Items...),
		executionOrder: append([]int(nil), candidate.ExecutionOrder...),
	}, nil
}

// validateExecutionOrder requires the order to name every item index exactly
// once, so each preflighted action executes once and only once.
func validateExecutionOrder(count int, order []int) error {
	seen := make([]bool, count)
	for _, entry := range order {
		if entry < 0 || entry >= count {
			return fmt.Errorf("add: execution order entry %d is out of range", entry)
		}
		if seen[entry] {
			return fmt.Errorf("add: execution order repeats item %d", entry)
		}
		seen[entry] = true
	}
	if len(order) != count {
		return fmt.Errorf("add: execution order covers %d items, want %d", len(order), count)
	}
	return nil
}

// Items returns a defensive copy of the display-sorted item plans.
func (p BatchPlan) Items() []ItemPlan {
	return append([]ItemPlan(nil), p.items...)
}

// ExecutionOrder returns a defensive copy of the execution indices.
func (p BatchPlan) ExecutionOrder() []int {
	return append([]int(nil), p.executionOrder...)
}

// ItemStatus marks the outcome of one per-target add record.
type ItemStatus = outcome.ItemStatus

const (
	StatusPlanned   = outcome.StatusPlanned
	StatusCompleted = outcome.StatusCompleted
	StatusPartial   = outcome.StatusPartial
)

// ItemResult is one per-target add record: the HOME-relative target, the
// repository-relative source, the storage kind, and the outcome status.
type ItemResult struct {
	Target string
	Source string
	Status ItemStatus
}

// Summary counts the per-target outcome records of one add.
type Summary = outcome.Summary

// Result is the frozen outcome of one add: the target-sorted per-target
// records and the outcome counts.
type Result struct {
	Items   []ItemResult
	Summary Summary
}

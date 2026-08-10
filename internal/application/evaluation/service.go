package evaluation

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

// Service runs one configured immutable evaluation pipeline.
type Service struct {
	source                       RepositorySource
	compiler                     Compiler
	state                        StateReader
	secrets                      *secrets.Client
	protectedTrees               []string
	platform                     deployment.Layer
	platformError                error
	commandLabel                 string
	includeUnmanagedTargetDigest bool
}

// NewService constructs an evaluation service bound to its dependencies.
func NewService(dependencies Dependencies) *Service {
	platform, err := deployment.ParseLayer(dependencies.Platform)
	if err != nil || platform == deployment.LayerBase {
		platform = ""
	}
	var platformError error
	if dependencies.Platform != "" && err != nil {
		platformError = failure.New(failure.InvalidInput, dependencies.CommandLabel+": invalid configured platform "+dependencies.Platform, err)
	}
	return &Service{
		source:                       dependencies.RepositorySource,
		compiler:                     dependencies.Compiler,
		state:                        dependencies.State,
		secrets:                      dependencies.Secrets,
		protectedTrees:               dependencies.ProtectedTrees,
		platform:                     platform,
		platformError:                platformError,
		commandLabel:                 dependencies.CommandLabel,
		includeUnmanagedTargetDigest: dependencies.IncludeUnmanagedTargetDigest,
	}
}

// Evaluate resolves the repository and runs selection, compilation, snapshot,
// assembly, semantic fingerprinting, and classification without mutation.
func (service *Service) Evaluate(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if service.platform == "" {
		if service.platformError != nil {
			return Result{}, service.platformError
		}
		return Result{}, failure.New(failure.InvalidInput, service.commandLabel+": platform must be linux or darwin", nil)
	}
	identity, err := service.resolve(request.Repository)
	if err != nil {
		return Result{}, err
	}
	rows, err := service.readRows(identity)
	if err != nil {
		return Result{}, err
	}
	return service.evaluateRows(ctx, evaluationInput{identity: identity, rows: rows, groups: request.Groups})
}

type evaluationInput struct {
	identity RepositoryIdentity
	rows     stateRows
	groups   []string
}

func (service *Service) evaluateRows(ctx context.Context, input evaluationInput) (Result, error) {
	full, chosen, err := service.chosen(input)
	if err != nil {
		return Result{}, err
	}
	plan, snapshot, err := service.selected(input, full, chosen)
	if err != nil {
		return Result{}, err
	}
	assembly, err := reconcile.Assemble(plan, snapshot, service.secrets)
	if err != nil {
		return Result{}, failure.New(failure.Operational, service.commandLabel+": assemble snapshot", err)
	}
	records, err := service.classify(ctx, assembly)
	if err != nil {
		return Result{}, err
	}
	return Result{
		RepositoryRoot: input.identity.Root,
		HomePath:       input.identity.Home,
		Platform:       string(service.platform),
		Hooks:          plan.Hooks(),
		Records:        records,
	}, nil
}

func (service *Service) chosen(input evaluationInput) (deployment.Plan, selection.Selection, error) {
	full, err := service.compile(input.identity, nil)
	if err != nil {
		return deployment.Plan{}, selection.Selection{}, err
	}
	chosen, err := selection.CompiledAndPersisted(full.Groups(), persistedGroups(input.rows), input.groups)
	if err != nil {
		return deployment.Plan{}, selection.Selection{}, failure.New(failure.InvalidInput, service.commandLabel+": select groups", err)
	}
	return full, chosen, nil
}

func (service *Service) selected(input evaluationInput, full deployment.Plan, chosen selection.Selection) (deployment.Plan, reconcile.StateSnapshot, error) {
	plan, err := service.selectedPlan(input.identity, full, chosen)
	if err != nil {
		return deployment.Plan{}, reconcile.StateSnapshot{}, err
	}
	snapshot, err := reconcile.NewStateSnapshot(selectedRows(input.identity, input.rows, chosen))
	if err != nil {
		return deployment.Plan{}, reconcile.StateSnapshot{}, failure.New(failure.Operational, service.commandLabel+": snapshot state", err)
	}
	return plan, snapshot, nil
}

func (service *Service) resolve(input RepositoryInput) (RepositoryIdentity, error) {
	identity, err := service.source.Resolve(selection.RepositoryRequest{
		RawExplicit: input.RawExplicit,
		ExplicitSet: input.ExplicitSet,
		RawEnv:      input.RawEnv,
		EnvSet:      input.EnvSet,
		WorkingDir:  input.WorkingDir,
	})
	if err != nil {
		return RepositoryIdentity{}, failure.New(failure.InvalidInput, service.commandLabel+": resolve repository", err)
	}
	return identity, nil
}

func (service *Service) readRows(identity RepositoryIdentity) (stateRows, error) {
	files, err := service.state.FileBaselines(identity.Root, identity.Home)
	if err != nil {
		return stateRows{}, failure.New(failure.Operational, service.commandLabel+": read file rows", err)
	}
	aliases, err := service.state.AliasBaselines(identity.Root, identity.Home)
	if err != nil {
		return stateRows{}, failure.New(failure.Operational, service.commandLabel+": read alias rows", err)
	}
	return stateRows{files: files, aliases: aliases}, nil
}

func (service *Service) compile(identity RepositoryIdentity, selected []string) (deployment.Plan, error) {
	plan, err := service.compiler.Compile(repository.CompileInput{
		Platform:       service.platform,
		RepositoryRoot: identity.Root,
		HomeRoot:       identity.Home,
		Protected:      service.protectedTrees,
		Selected:       selected,
	})
	if err != nil {
		return deployment.Plan{}, compileFailure(service.commandLabel+": compile plan", err)
	}
	return plan, nil
}

func compileFailure(message string, cause error) error {
	var pathError *os.PathError
	if errors.As(cause, &pathError) {
		return failure.New(failure.Operational, message, cause)
	}
	return failure.New(failure.InvalidInput, message, cause)
}

func (service *Service) selectedPlan(identity RepositoryIdentity, full deployment.Plan, chosen selection.Selection) (deployment.Plan, error) {
	if chosen.Root {
		return full, nil
	}
	selected := intersectGroups(chosen.Groups, full.Groups())
	if len(selected) == 0 {
		return deployment.NewPlan(deployment.PlanInput{RepositoryRoot: identity.Root, Platform: string(service.platform)})
	}
	return service.compile(identity, selected)
}

func intersectGroups(selected, current []string) []string {
	var common []string
	for _, name := range selected {
		if slices.Contains(current, name) {
			common = append(common, name)
		}
	}
	return common
}

type stateRows struct {
	files   []state.FileBaseline
	aliases []state.AliasBaseline
}

func persistedGroups(rows stateRows) selection.PersistedGroups {
	sets := &groupSets{active: make(map[string]bool), all: make(map[string]bool)}
	for _, row := range rows.files {
		sets.remember(row.GroupName, row.Status == state.StatusActive)
	}
	for _, row := range rows.aliases {
		sets.remember(row.GroupName, row.Status == state.StatusActive)
	}
	return selection.PersistedGroups{Active: sortedKeys(sets.active), All: sortedKeys(sets.all)}
}

type groupSets struct {
	active map[string]bool
	all    map[string]bool
}

func (sets *groupSets) remember(name string, active bool) {
	if name == "" {
		return
	}
	sets.all[name] = true
	if active {
		sets.active[name] = true
	}
}

func sortedKeys(names map[string]bool) []string {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

func selectedRows(identity RepositoryIdentity, rows stateRows, chosen selection.Selection) reconcile.StateRows {
	return reconcile.StateRows{
		RepositoryRoot: identity.Root,
		HomePath:       identity.Home,
		Files:          keepFileRows(rows.files, chosen),
		Aliases:        keepAliasRows(rows.aliases, chosen),
	}
}

func keepFileRows(rows []state.FileBaseline, chosen selection.Selection) []state.FileBaseline {
	kept := append([]state.FileBaseline(nil), rows...)
	return slices.DeleteFunc(kept, func(row state.FileBaseline) bool { return !rowKept(row.GroupName, chosen) })
}

func keepAliasRows(rows []state.AliasBaseline, chosen selection.Selection) []state.AliasBaseline {
	kept := append([]state.AliasBaseline(nil), rows...)
	return slices.DeleteFunc(kept, func(row state.AliasBaseline) bool { return !rowKept(row.GroupName, chosen) })
}

func rowKept(group string, chosen selection.Selection) bool {
	if group == "" {
		return chosen.Root
	}
	return slices.Contains(chosen.Groups, group)
}

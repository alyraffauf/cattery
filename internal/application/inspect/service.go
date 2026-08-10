package inspect

import (
	"context"
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

type Service struct {
	source         RepositorySource
	compiler       Compiler
	state          StateReader
	secrets        *secrets.Client
	protectedTrees []string
	platform       deployment.Layer
	platformError  error
}

// NewService constructs the inspection service bound to the dependencies.
func NewService(dependencies Dependencies) *Service {
	platform, err := deployment.ParseLayer(dependencies.Platform)
	if err != nil || platform == deployment.LayerBase {
		platform = ""
	}
	var platformError error
	if dependencies.Platform != "" && err != nil {
		platformError = failure.New(failure.InvalidInput, "inspect: invalid configured platform "+dependencies.Platform, err)
	}
	return &Service{
		source:         dependencies.RepositorySource,
		compiler:       dependencies.Compiler,
		state:          dependencies.State,
		secrets:        dependencies.Secrets,
		protectedTrees: dependencies.ProtectedTrees,
		platform:       platform,
		platformError:  platformError,
	}
}

// Evaluate performs one immutable selection, compile, snapshot, and
// classification evaluation with on-demand secret semantics (PLAN.md Section
// 9.1). No status/diff rendering, hook, prompt, registration, or mutation
// occurs.
func (service *Service) Evaluate(ctx context.Context, request Request) (Result, error) {
	return service.evaluate(ctx, request)
}

// evaluate resolves the repository and runs the selection, compile,
// snapshot, and classification pipeline.
func (service *Service) evaluate(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if service.platform == "" {
		if service.platformError != nil {
			return Result{}, service.platformError
		}
		return Result{}, failure.New(failure.InvalidInput, "inspect: platform must be linux or darwin", nil)
	}
	identity, err := service.resolve(request.Repository)
	if err != nil {
		return Result{}, err
	}
	rows, err := service.readRows(identity)
	if err != nil {
		return Result{}, err
	}
	return service.evaluateRows(ctx, scopeInput{identity: identity, rows: rows, groups: request.Groups})
}

// scopeInput bundles the repository pair, its rows, and the requested
// groups of one evaluation.
type scopeInput struct {
	identity RepositoryIdentity
	rows     stateRows
	groups   []string
}

// evaluateRows compiles the full plan, selects the scopes, and assembles
// one classified snapshot of the selected rows.
func (service *Service) evaluateRows(ctx context.Context, input scopeInput) (Result, error) {
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
		return Result{}, failure.New(failure.Operational, "inspect: assemble snapshot", err)
	}
	records, err := service.classify(ctx, assembly)
	if err != nil {
		return Result{}, err
	}
	return Result{home: input.identity.Home, records: records}, nil
}

// chosen compiles the full plan and validates the group selection.
func (service *Service) chosen(input scopeInput) (deployment.Plan, selection.Selection, error) {
	full, err := service.compile(input.identity, nil)
	if err != nil {
		return deployment.Plan{}, selection.Selection{}, err
	}
	chosen, err := selection.CompiledAndPersisted(full.Groups(), persistedGroups(input.rows), input.groups)
	if err != nil {
		return deployment.Plan{}, selection.Selection{}, failure.New(failure.InvalidInput, "inspect: select groups", err)
	}
	return full, chosen, nil
}

// selected restricts the full plan and the rows to the selection and
// converts them into an immutable state snapshot.
func (service *Service) selected(input scopeInput, full deployment.Plan, chosen selection.Selection) (deployment.Plan, reconcile.StateSnapshot, error) {
	plan, err := service.selectedPlan(input.identity, full, chosen)
	if err != nil {
		return deployment.Plan{}, reconcile.StateSnapshot{}, err
	}
	snapshot, err := reconcile.NewStateSnapshot(selectedRows(input.identity, input.rows, chosen))
	if err != nil {
		return deployment.Plan{}, reconcile.StateSnapshot{}, failure.New(failure.Operational, "inspect: snapshot state", err)
	}
	return plan, snapshot, nil
}

// resolve maps the raw repository fields and resolves the canonical pair.
func (service *Service) resolve(input RepositoryInput) (RepositoryIdentity, error) {
	identity, err := service.source.Resolve(repositoryRequest(input))
	if err != nil {
		return RepositoryIdentity{}, failure.New(failure.InvalidInput, "inspect: resolve repository", err)
	}
	return identity, nil
}

// repositoryRequest copies the raw repository fields into the selection
// request shape.
func repositoryRequest(input RepositoryInput) selection.RepositoryRequest {
	return selection.RepositoryRequest{
		RawExplicit: input.RawExplicit,
		ExplicitSet: input.ExplicitSet,
		RawEnv:      input.RawEnv,
		EnvSet:      input.EnvSet,
		WorkingDir:  input.WorkingDir,
	}
}

func (service *Service) readRows(identity RepositoryIdentity) (stateRows, error) {
	files, err := service.state.FileBaselines(identity.Root, identity.Home)
	if err != nil {
		return stateRows{}, failure.New(failure.Operational, "inspect: read file rows", err)
	}
	aliases, err := service.state.AliasBaselines(identity.Root, identity.Home)
	if err != nil {
		return stateRows{}, failure.New(failure.Operational, "inspect: read alias rows", err)
	}
	return stateRows{files: files, aliases: aliases}, nil
}

// compile validates the repository and returns the plan restricted to the
// selection (nil selects everything).
func (service *Service) compile(identity RepositoryIdentity, selected []string) (deployment.Plan, error) {
	plan, err := service.compiler.Compile(repository.CompileInput{
		Platform:       service.platform,
		RepositoryRoot: identity.Root,
		HomeRoot:       identity.Home,
		Protected:      service.protectedTrees,
		Selected:       selected,
	})
	if err != nil {
		return deployment.Plan{}, compileFailure("inspect: compile plan", err)
	}
	return plan, nil
}

// selectedPlan restricts the full plan to the selection: root-only
// selections keep it, explicit selections filter to the selected repository
// groups, and pure state-only selections yield an empty plan.
func (service *Service) selectedPlan(identity RepositoryIdentity, full deployment.Plan, chosen selection.Selection) (deployment.Plan, error) {
	if chosen.Root {
		return full, nil
	}
	selected := intersectGroups(chosen.Groups, full.Groups())
	if len(selected) == 0 {
		return emptyPlan(identity, service.platform)
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

// emptyPlan builds the degenerate plan of a pure state-only selection,
// which carries no producer and only joins persisted rows.
func emptyPlan(identity RepositoryIdentity, platform deployment.Layer) (deployment.Plan, error) {
	return deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: identity.Root,
		Platform:       string(platform),
	})
}

type stateRows struct {
	files   []state.FileBaseline
	aliases []state.AliasBaseline
}

// persistedGroups derives Active and All group names from the persisted
// rows.
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

// sortedKeys returns the map keys in bytewise order.
func sortedKeys(names map[string]bool) []string {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

// selectedRows keeps root rows only for root selections and rows of the
// selected groups otherwise.
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

// rowKept reports whether a row group belongs to the selection.
func rowKept(group string, chosen selection.Selection) bool {
	if group == "" {
		return chosen.Root
	}
	return slices.Contains(chosen.Groups, group)
}

// evaluatedRecord pairs one snapshot evaluation with its three
// classifications.
type evaluatedRecord struct {
	record     reconcile.Evaluation
	file       reconcile.FileClassification
	alias      reconcile.AliasClassification
	retirement reconcile.RetirementClassification
}

// classify computes the semantic fingerprints and the file, alias, and
// retirement classifications of every evaluation record.
func (service *Service) classify(ctx context.Context, assembly reconcile.EvaluationSnapshot) ([]evaluatedRecord, error) {
	semantics := &semanticState{reader: service.state, client: service.secrets}
	records := assembly.All()
	evaluated := make([]evaluatedRecord, 0, len(records))
	for _, record := range records {
		fingerprints, err := semantics.fingerprints(ctx, assembly.HomePath, record)
		if err != nil {
			return nil, err
		}
		evaluated = append(evaluated, evaluatedRecord{
			record:     record,
			file:       reconcile.ClassifyFile(record, fingerprints),
			alias:      reconcile.ClassifyAlias(record, fingerprints),
			retirement: reconcile.ClassifyRetirement(record, assembly.Platform),
		})
	}
	return evaluated, nil
}

// semanticState carries the per-evaluation hash key, recovered once at
// most and only when a secret record needs fingerprints.
type semanticState struct {
	reader  StateReader
	client  *secrets.Client
	key     [32]byte
	haveKey bool
}

// fingerprints derives the semantic fingerprints of one evaluation record;
// secrets fingerprint on demand per PLAN.md Section 9.1.
func (state *semanticState) fingerprints(ctx context.Context, home string, record reconcile.Evaluation) (reconcile.FileSemantics, error) {
	if record.Entry != reconcile.PlanEntryFile {
		return reconcile.FileSemantics{}, nil
	}
	if record.File.Kind == deployment.FileOrdinary {
		return reconcile.FileSemantics{
			Source: record.Source.Snapshot().Semantic(),
			Target: record.Target.Digest(),
		}, nil
	}
	return state.secretFingerprints(ctx, home, record)
}

// secretFingerprints derives the keyed fingerprints of one secret record:
// the source decrypts only when its raw storage changed or the row is
// unbaselined with a regular target.
func (state *semanticState) secretFingerprints(ctx context.Context, home string, record reconcile.Evaluation) (reconcile.FileSemantics, error) {
	semantics := reconcile.FileSemantics{}
	targetFile := record.Target.Kind() == reconcile.KindFile
	if !targetFile && !secretDecryptNeeded(record) {
		return semantics, nil
	}
	if err := state.recover(); err != nil {
		return semantics, err
	}
	if targetFile {
		content, err := readTargetContent(home, record)
		if err != nil {
			return semantics, failure.New(failure.Operational, "inspect: read target "+record.TargetPath, err)
		}
		semantics.Target = deployment.SecretSemantic(content, state.key)
	}
	if secretDecryptNeeded(record) {
		source, err := record.Source.KeyedSemantic(ctx, state.key)
		if err != nil {
			return semantics, categorized(err, "inspect: decrypt source "+record.File.SourceRepositoryPath)
		}
		semantics.Source = source
	}
	return semantics, nil
}

func categorized(err error, message string) error {
	if _, ok := failure.HasKind(err); ok {
		return err
	}
	return failure.New(failure.Operational, message, err)
}

// secretDecryptNeeded reports whether a secret source must decrypt for
// classification (PLAN.md Section 9.1).
func secretDecryptNeeded(record reconcile.Evaluation) bool {
	if record.FileState == nil {
		return record.Target.Kind() == reconcile.KindFile
	}
	return record.Source.Snapshot().Storage() != record.FileState.BaselineSource()
}

// recover loads the per-installation hash key once for the evaluation.
func (state *semanticState) recover() error {
	if state.haveKey {
		return nil
	}
	key, err := state.reader.RecoverHashKey()
	if err != nil {
		return failure.New(failure.Operational, "inspect: recover hash key", err)
	}
	state.key, state.haveKey = key, true
	return nil
}

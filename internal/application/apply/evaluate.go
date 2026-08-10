package apply

import (
	"context"
	"os"
	"path/filepath"
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

// Service performs one apply evaluation against the injectable ports.
type Service struct {
	source         RepositorySource
	compiler       Compiler
	state          StateReader
	secrets        *secrets.Client
	client         SecretClient
	replacer       AtomicReplacer
	baselines      BaselineStore
	transitions    TransitionStore
	retirements    RetirementStore
	hooks          HookExecutor
	probe          DependencyProbe
	resolver       DecisionResolver
	protectedTrees []string
	platform       deployment.Layer
}

func NewService(dependencies Dependencies) *Service {
	platform, err := deployment.ParseLayer(dependencies.Platform)
	if err != nil || platform == deployment.LayerBase {
		platform = ""
	}
	return &Service{
		source:         dependencies.RepositorySource,
		compiler:       dependencies.Compiler,
		state:          dependencies.State,
		secrets:        dependencies.Secrets,
		client:         dependencies.Client,
		replacer:       dependencies.Replacer,
		baselines:      dependencies.Baselines,
		transitions:    dependencies.Transitions,
		retirements:    dependencies.Retirements,
		hooks:          dependencies.Hooks,
		probe:          dependencies.Probe,
		resolver:       dependencies.Resolver,
		protectedTrees: dependencies.ProtectedTrees,
		platform:       platform,
	}
}

func (service *Service) Evaluate(ctx context.Context, request Request) (Candidates, error) {
	return service.evaluate(ctx, request)
}

// evaluate resolves the repository and runs the selection, compile, snapshot, and classification pipeline.
func (service *Service) evaluate(ctx context.Context, request Request) (Candidates, error) {
	if err := ctx.Err(); err != nil {
		return Candidates{}, err
	}
	if service.platform == "" {
		return Candidates{}, failure.New(failure.InvalidInput, "apply: platform must be linux or darwin", nil)
	}
	identity, err := service.resolve(request.Repository)
	if err != nil {
		return Candidates{}, err
	}
	rows, err := service.readRows(identity)
	if err != nil {
		return Candidates{}, err
	}
	return service.evaluateRows(ctx, scopeInput{identity: identity, rows: rows, groups: request.Groups})
}

type scopeInput struct {
	identity RepositoryIdentity
	rows     stateRows
	groups   []string
}

// evaluateRows compiles the full plan, selects the scopes, and assembles one classified snapshot of the selected rows.
func (service *Service) evaluateRows(ctx context.Context, input scopeInput) (Candidates, error) {
	full, chosen, err := service.chosen(input)
	if err != nil {
		return Candidates{}, err
	}
	plan, snapshot, err := service.selected(input, full, chosen)
	if err != nil {
		return Candidates{}, err
	}
	assembly, err := reconcile.Assemble(plan, snapshot, service.secrets)
	if err != nil {
		return Candidates{}, failure.New(failure.Operational, "apply: assemble snapshot", err)
	}
	candidates, err := service.classify(ctx, assembly)
	if err != nil {
		return Candidates{}, err
	}
	return Candidates{root: input.identity.Root, home: input.identity.Home, records: candidates}, nil
}

func (service *Service) chosen(input scopeInput) (deployment.Plan, selection.Selection, error) {
	full, err := service.compile(input.identity, nil)
	if err != nil {
		return deployment.Plan{}, selection.Selection{}, err
	}
	chosen, err := selection.CompiledAndPersisted(full.Groups(), persistedGroups(input.rows), input.groups)
	if err != nil {
		return deployment.Plan{}, selection.Selection{}, failure.New(failure.InvalidInput, "apply: select groups", err)
	}
	return full, chosen, nil
}

func (service *Service) selected(input scopeInput, full deployment.Plan, chosen selection.Selection) (deployment.Plan, reconcile.StateSnapshot, error) {
	plan, err := service.selectedPlan(input.identity, full, chosen)
	if err != nil {
		return deployment.Plan{}, reconcile.StateSnapshot{}, err
	}
	snapshot, err := reconcile.NewStateSnapshot(selectedRows(input.identity, input.rows, chosen))
	if err != nil {
		return deployment.Plan{}, reconcile.StateSnapshot{}, failure.New(failure.Operational, "apply: snapshot state", err)
	}
	return plan, snapshot, nil
}

// resolve maps the raw repository fields and resolves the canonical pair.
func (service *Service) resolve(input RepositoryInput) (RepositoryIdentity, error) {
	identity, err := service.source.Resolve(repositoryRequest(input))
	if err != nil {
		return RepositoryIdentity{}, failure.New(failure.InvalidInput, "apply: resolve repository", err)
	}
	return identity, nil
}

// repositoryRequest copies the raw repository fields into the selection request shape.
func repositoryRequest(input RepositoryInput) selection.RepositoryRequest {
	return selection.RepositoryRequest{
		RawExplicit: input.RawExplicit,
		ExplicitSet: input.ExplicitSet,
		RawEnv:      input.RawEnv,
		EnvSet:      input.EnvSet,
		WorkingDir:  input.WorkingDir,
	}
}

// readRows reads every persisted file and alias row of the repository pair.
func (service *Service) readRows(identity RepositoryIdentity) (stateRows, error) {
	files, err := service.state.FileBaselines(identity.Root, identity.Home)
	if err != nil {
		return stateRows{}, failure.New(failure.Operational, "apply: read file rows", err)
	}
	aliases, err := service.state.AliasBaselines(identity.Root, identity.Home)
	if err != nil {
		return stateRows{}, failure.New(failure.Operational, "apply: read alias rows", err)
	}
	return stateRows{files: files, aliases: aliases}, nil
}

// compile validates the repository and returns the plan restricted to the selection (nil selects everything).
func (service *Service) compile(identity RepositoryIdentity, selected []string) (deployment.Plan, error) {
	plan, err := service.compiler.Compile(repository.CompileInput{
		Platform:       service.platform,
		RepositoryRoot: identity.Root,
		HomeRoot:       identity.Home,
		Protected:      service.protectedTrees,
		Selected:       selected,
	})
	if err != nil {
		return deployment.Plan{}, failure.New(failure.InvalidInput, "apply: compile plan", err)
	}
	return plan, nil
}

// selectedPlan restricts the full plan to the selection: root-only selections keep it, explicit selections filter to the selected repository
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

// intersectGroups keeps selected names that are also current groups.
func intersectGroups(selected, current []string) []string {
	var common []string
	for _, name := range selected {
		if slices.Contains(current, name) {
			common = append(common, name)
		}
	}
	return common
}

// emptyPlan builds the degenerate plan of a pure state-only selection, which carries no producer and only joins persisted rows.
func emptyPlan(identity RepositoryIdentity, platform deployment.Layer) (deployment.Plan, error) {
	return deployment.NewPlan(deployment.PlanInput{
		RepositoryRoot: identity.Root,
		Platform:       string(platform),
	})
}

// stateRows bundles the persisted rows of one repository pair.
type stateRows struct {
	files   []state.FileBaseline
	aliases []state.AliasBaseline
}

// persistedGroups derives Active and All group names from the persisted rows.
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

// groupSets accumulates the distinct group names of the persisted rows.
type groupSets struct {
	active map[string]bool
	all    map[string]bool
}

// remember records one row group in the active and all sets.
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

// selectedRows keeps root rows only for root selections and rows of the selected groups otherwise.
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

// Candidate is one immutable evaluation record with its three pure classifications and the semantic fingerprints used to derive them.
type Candidate struct {
	record     reconcile.Evaluation
	file       reconcile.FileClassification
	alias      reconcile.AliasClassification
	retirement reconcile.RetirementClassification
	semantics  reconcile.FileSemantics
}

// Candidates freezes the evaluated records of one apply in deterministic target-path order.
type Candidates struct {
	root    string
	home    string
	records []Candidate
}

// Root returns the canonical repository root of the evaluated pair.
func (c Candidates) Root() string { return c.root }

func (c Candidates) Home() string { return c.home }

func (c Candidates) All() []Candidate {
	return append([]Candidate(nil), c.records...)
}

// classify computes the semantic fingerprints and the file, alias, and retirement classifications of every evaluation record.
func (service *Service) classify(ctx context.Context, assembly reconcile.EvaluationSnapshot) ([]Candidate, error) {
	semantics := &semanticState{reader: service.state, client: service.secrets}
	records := assembly.All()
	candidates := make([]Candidate, 0, len(records))
	for _, record := range records {
		fingerprints, err := semantics.fingerprints(ctx, assembly.HomePath, record)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, Candidate{
			record:     record,
			file:       reconcile.ClassifyFile(record, fingerprints),
			alias:      reconcile.ClassifyAlias(record, fingerprints),
			retirement: reconcile.ClassifyRetirement(record, assembly.Platform),
			semantics:  fingerprints,
		})
	}
	return candidates, nil
}

// semanticState carries the per-evaluation hash key, recovered once at most and only when a secret record needs fingerprints.
type semanticState struct {
	reader  StateReader
	client  *secrets.Client
	key     [32]byte
	haveKey bool
}

// fingerprints derives the semantic fingerprints of one evaluation record; secrets fingerprint on demand per PLAN.md Section 9.1.
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

// secretFingerprints derives the keyed fingerprints of one secret record: the source decrypts only when its raw storage changed or the row is
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
		content, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(record.TargetPath)))
		if err != nil {
			return semantics, failure.New(failure.Operational, "apply: read target "+record.TargetPath, err)
		}
		semantics.Target = deployment.SecretSemantic(content, state.key)
	}
	if secretDecryptNeeded(record) {
		source, err := record.Source.KeyedSemantic(ctx, state.key)
		if err != nil {
			return semantics, failure.New(failure.Operational, "apply: decrypt source "+record.File.SourceRepositoryPath, err)
		}
		semantics.Source = source
	}
	return semantics, nil
}

// secretDecryptNeeded reports whether a secret source must decrypt for classification (PLAN.md Section 9.1).
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
		return failure.New(failure.Operational, "apply: recover hash key", err)
	}
	state.key = key
	state.haveKey = true
	return nil
}

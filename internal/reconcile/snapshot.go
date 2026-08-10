package reconcile

import (
	"fmt"
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/secrets"
	"github.com/alyraffauf/cattery/internal/state"
)

// PlanEntryKind names the representation one plan entry produces at a path.
type PlanEntryKind int

const (
	PlanEntryNone PlanEntryKind = iota
	PlanEntryFile
	PlanEntryAlias
)

// Valid reports whether kind is one of the supported constants.
func (k PlanEntryKind) Valid() bool { return k >= PlanEntryNone && k <= PlanEntryAlias }

// Evaluation is the immutable per-path join across one complete selected
// platform plan: the producing plan entry, the current target observation,
// and the persisted file and alias rows. The source observation's retained
// buffer is shared by design with every returned copy, so later phases write
// validated bytes and clear secret material exactly once.
type Evaluation struct {
	TargetPath string
	Entry      PlanEntryKind
	File       deployment.ManagedFile
	Alias      deployment.Alias
	Source     SourceObservation
	Target     TargetSnapshot
	FileState  *FileState
	AliasState *AliasState
}

// EvaluationSnapshot is the immutable joined record of one complete selected
// platform plan. Every destination path in the plan or in persisted state
// carries its plan entry, current target observation, and state rows, sorted
// bytewise by path; nothing is classified here.
type EvaluationSnapshot struct {
	RepositoryRoot string
	HomePath       string
	Platform       string
	records        []Evaluation
}

// Assemble freezes one complete selected platform plan into an immutable
// per-path snapshot: sources are captured, targets are captured at every
// union path, and persisted state rows join their paths. Plan and state
// inputs may arrive in any order; records always return sorted. Secret
// sources keep ciphertext only; no decryption ever happens here.
func Assemble(plan deployment.Plan, stateSnapshot StateSnapshot, client *secrets.Client) (EvaluationSnapshot, error) {
	if err := requireAssemblyPlan(plan, stateSnapshot); err != nil {
		return EvaluationSnapshot{}, err
	}
	files, aliases, err := entryIndexes(plan)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	input := joinInput{home: stateSnapshot.HomePath(), files: files, aliases: aliases,
		fileRows: byFileState(stateSnapshot.AllFiles()), aliasRows: byAliasState(stateSnapshot.AllAliases()), client: client}
	records, err := joinedRecords(input)
	if err != nil {
		return EvaluationSnapshot{}, err
	}
	sort.SliceStable(records, func(first, second int) bool {
		return records[first].TargetPath < records[second].TargetPath
	})
	return EvaluationSnapshot{RepositoryRoot: plan.RepositoryRoot(), HomePath: stateSnapshot.HomePath(),
		Platform: plan.Platform(), records: records}, nil
}

// requireAssemblyPlan rejects a plan that cannot describe the state pair.
func requireAssemblyPlan(plan deployment.Plan, state StateSnapshot) error {
	if plan.RepositoryRoot() == "" || state.HomePath() == "" {
		return fmt.Errorf("reconcile: snapshot assembly requires canonical repository and home paths")
	}
	if plan.RepositoryRoot() != state.RepositoryRoot() {
		return fmt.Errorf("reconcile: plan repository %q does not match state repository %q", plan.RepositoryRoot(), state.RepositoryRoot())
	}
	if plan.Platform() == "" {
		return fmt.Errorf("reconcile: snapshot assembly requires a selected platform")
	}
	return nil
}

// entryIndexes indexes plan entries by destination path, rejecting duplicate
// file, duplicate alias, and file/alias collisions at one path.
func entryIndexes(plan deployment.Plan) (map[string]deployment.ManagedFile, map[string]deployment.Alias, error) {
	planFiles := plan.Files()
	files := make(map[string]deployment.ManagedFile, len(planFiles))
	for _, file := range planFiles {
		if _, occupied := files[file.TargetRelativePath]; occupied {
			return nil, nil, fmt.Errorf("reconcile: plan has duplicate file entry %q", file.TargetRelativePath)
		}
		files[file.TargetRelativePath] = file
	}
	planAliases := plan.Aliases()
	aliases := make(map[string]deployment.Alias, len(planAliases))
	for _, alias := range planAliases {
		if _, occupied := aliases[alias.AliasRelativePath]; occupied {
			return nil, nil, fmt.Errorf("reconcile: plan has duplicate alias entry %q", alias.AliasRelativePath)
		}
		if _, occupied := files[alias.AliasRelativePath]; occupied {
			return nil, nil, fmt.Errorf("reconcile: plan collides on %q", alias.AliasRelativePath)
		}
		aliases[alias.AliasRelativePath] = alias
	}
	return files, aliases, nil
}

// joinInput bundles the immutable inputs one record join needs.
type joinInput struct {
	home      string
	files     map[string]deployment.ManagedFile
	aliases   map[string]deployment.Alias
	fileRows  map[string]*FileState
	aliasRows map[string]*AliasState
	client    *secrets.Client
}

// joinedRecords captures every union path and joins it with its rows.
func joinedRecords(input joinInput) ([]Evaluation, error) {
	paths := unionPaths(input)
	records := make([]Evaluation, 0, len(paths))
	for _, path := range paths {
		record, err := recordFor(path, input)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// unionPaths merges plan and state paths into one deduplicated list.
func unionPaths(input joinInput) []string {
	paths := make(map[string]bool, len(input.files)+len(input.aliases)+len(input.fileRows)+len(input.aliasRows))
	for path := range input.files {
		paths[path] = true
	}
	for path := range input.aliases {
		paths[path] = true
	}
	for path := range input.fileRows {
		paths[path] = true
	}
	for path := range input.aliasRows {
		paths[path] = true
	}
	joined := make([]string, 0, len(paths))
	for path := range paths {
		joined = append(joined, path)
	}
	return joined
}

// recordFor captures one path's source and target and joins its rows.
func recordFor(path string, input joinInput) (Evaluation, error) {
	target, err := CaptureTarget(Destination{Root: input.home, Relative: path})
	if err != nil {
		return Evaluation{}, err
	}
	record := Evaluation{TargetPath: path, Target: target,
		FileState: cloneFileRecord(input.fileRows[path]), AliasState: cloneAliasRecord(input.aliasRows[path])}
	if file, present := input.files[path]; present {
		record.Entry = PlanEntryFile
		record.File = file
		source, err := CaptureSource(file, input.client)
		if err != nil {
			return Evaluation{}, err
		}
		record.Source = source
		return record, nil
	}
	if alias, present := input.aliases[path]; present {
		record.Entry = PlanEntryAlias
		record.Alias = alias
	}
	return record, nil
}

// byFileState indexes the caller-owned state slice by target path. The slice
// is freshly allocated by StateSnapshot.All, so these pointers remain local.
func byFileState(rows []FileState) map[string]*FileState {
	index := make(map[string]*FileState, len(rows))
	for rowIndex := range rows {
		index[rows[rowIndex].TargetPath()] = &rows[rowIndex]
	}
	return index
}

// byAliasState indexes alias records by alias path.
func byAliasState(rows []AliasState) map[string]*AliasState {
	index := make(map[string]*AliasState, len(rows))
	for rowIndex := range rows {
		index[rows[rowIndex].AliasPath()] = &rows[rowIndex]
	}
	return index
}

// cloneFileRecord returns an independent copy of a file record.
func cloneFileRecord(record *FileState) *FileState {
	if record == nil {
		return nil
	}
	copyRecord := *record
	copyRecord.retiredAt = state.CloneTimestamp(record.retiredAt)
	return &copyRecord
}

// cloneAliasRecord returns an independent copy of an alias record.
func cloneAliasRecord(record *AliasState) *AliasState {
	if record == nil {
		return nil
	}
	copyRecord := *record
	copyRecord.retiredAt = state.CloneTimestamp(record.retiredAt)
	return &copyRecord
}

// All returns a defensive copy of every joined record in bytewise path
// order, cloning the persisted rows so callers cannot reach the snapshot.
func (snapshot EvaluationSnapshot) All() []Evaluation {
	if snapshot.records == nil {
		return nil
	}
	records := append([]Evaluation(nil), snapshot.records...)
	for index := range records {
		records[index].FileState = cloneFileRecord(records[index].FileState)
		records[index].AliasState = cloneAliasRecord(records[index].AliasState)
	}
	return records
}

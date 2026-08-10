package apply

import (
	"context"
	"io/fs"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/state"
)

// ExecuteFiles runs the regular-file actions of one apply sequentially:
// each target is re-frozen, written durably with its mode policy, and
// baselined in a short state commit. A later failure preserves accurate
// earlier state and never imports target bytes (PLAN.md Section 11.5).
func (service *Service) ExecuteFiles(ctx context.Context, plan PreparedPlan, candidates Candidates) ([]ItemResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	byPath := candidatesByPath(candidates)
	results := make([]ItemResult, 0)
	for _, action := range plan.Actions().Items() {
		if action.Kind != ActionKindWriteSource && action.Kind != ActionKindReplaceFile {
			continue
		}
		job := fileJob{action: action, candidate: byPath[action.TargetPath], root: candidates.Root(), home: candidates.Home()}
		record, err := service.executeFile(ctx, job)
		results = append(results, record)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

// fileJob bundles one file action with its candidate and the repository
// pair locations.
type fileJob struct {
	action    PlanAction
	candidate Candidate
	root      string
	home      string
}

// candidatesByPath indexes the candidates by target path.
func candidatesByPath(candidates Candidates) map[string]Candidate {
	index := make(map[string]Candidate, len(candidates.All()))
	for _, candidate := range candidates.All() {
		index[candidate.record.TargetPath] = candidate
	}
	return index
}

// executeFile re-freezes one target, writes the exact source bytes with the
// managed mode, and establishes the baseline. A target that already carries
// the exact bytes recovers by equality without a write.
func (service *Service) executeFile(ctx context.Context, job fileJob) (ItemResult, error) {
	precondition, err := filesystem.Freeze(filesystem.Destination{Root: job.home, Relative: job.action.TargetPath})
	if err != nil {
		return partialRecord(job), failure.New(failure.Operational, "apply: freeze target "+job.action.TargetPath, err)
	}
	content, contentHash, clear, err := service.sourceContent(ctx, job.candidate)
	if err != nil {
		return partialRecord(job), err
	}
	if clear != nil {
		defer clear()
	}
	durable, err := service.writeTarget(ctx, writeSpec{precondition: precondition, content: content, candidate: job.candidate})
	if err != nil {
		return partialRecord(job), err
	}
	if err := service.commitBaseline(ctx, job, baselineCommit{contentHash: contentHash, durable: durable}); err != nil {
		return partialRecord(job), err
	}
	if !durable {
		return partialRecord(job), nil
	}
	return completedRecord(job), nil
}

// baselineCommit bundles the content fingerprint and durability of one
// completed file write.
type baselineCommit struct {
	contentHash deployment.Digest
	durable     bool
}

// commitBaseline switches an active alias row to the file representation or
// upserts the file baseline, only after a durable write.
func (service *Service) commitBaseline(ctx context.Context, job fileJob, commit baselineCommit) error {
	if !commit.durable {
		return failure.New(failure.Operational, "apply: baseline write is not durable: "+job.action.TargetPath, nil)
	}
	if job.candidate.record.AliasState != nil && job.candidate.record.AliasState.Active() {
		_, err := service.transitions.TransitionToFile(job.root, job.home, baselineRow(job.candidate, commit.contentHash))
		if err != nil {
			return failure.New(failure.Operational, "apply: transition to file "+job.action.TargetPath, err)
		}
		return nil
	}
	_, err := service.baselines.UpsertFileBaseline(job.root, job.home, baselineRow(job.candidate, commit.contentHash))
	if err != nil {
		return failure.New(failure.Operational, "apply: baseline "+job.action.TargetPath, err)
	}
	return nil
}

// writeSpec bundles the frozen precondition, exact bytes, and candidate of
// one target write.
type writeSpec struct {
	precondition filesystem.Precondition
	content      []byte
	candidate    Candidate
}

// sourceContent returns the exact source bytes of one file action: the
// retained ordinary bytes, or the decrypted plaintext for secrets. The
// content hash is the keyed secret fingerprint or the ordinary digest.
func (service *Service) sourceContent(ctx context.Context, candidate Candidate) ([]byte, deployment.Digest, func(), error) {
	snapshot := candidate.record.Source.Snapshot()
	if candidate.record.File.Kind != deployment.FileSecret {
		return candidate.record.Source.Bytes(), snapshot.Semantic(), nil, nil
	}
	plaintext, err := service.secrets.Decrypt(ctx, candidate.record.Source.Bytes(), candidate.record.File.SourceRepositoryPath)
	if err != nil {
		return nil, deployment.Digest{}, nil, failure.New(failure.Operational, "apply: decrypt source "+candidate.record.File.SourceRepositoryPath, err)
	}
	key, err := service.state.RecoverHashKey()
	if err != nil {
		return nil, deployment.Digest{}, nil, failure.New(failure.Operational, "apply: recover hash key", err)
	}
	return plaintext, deployment.SecretSemantic(plaintext, key), clearBytes(plaintext), nil
}

// clearBytes returns a function that zeroes the plaintext buffer.
func clearBytes(plaintext []byte) func() {
	return func() {
		for index := range plaintext {
			plaintext[index] = 0
		}
	}
}

// writeTarget writes the exact bytes with the managed mode unless the
// target already carries them, and reports whether the result is durable.
func (service *Service) writeTarget(ctx context.Context, spec writeSpec) (bool, error) {
	mode := service.targetMode(spec.precondition, spec.candidate)
	if spec.precondition.Target().Token() == filesystem.TokenOfContent(spec.content) && spec.precondition.Target().Mode().Perm() == mode.Perm() {
		return true, nil
	}
	result, err := service.replacer.ReplaceResult(ctx, spec.precondition, filesystem.ReplacementSpec{Content: spec.content, Mode: mode})
	if err != nil {
		return false, failure.New(failure.Operational, "apply: replace target "+spec.candidate.record.TargetPath, err)
	}
	return result.DirectorySynced, nil
}

// targetMode derives the managed mode of one file action from the mode
// policy and the current target facts.
func (service *Service) targetMode(precondition filesystem.Precondition, candidate Candidate) (mode fs.FileMode) {
	executable := candidate.record.File.SourceExecutableBits
	if candidate.record.File.Kind == deployment.FileSecret {
		return filesystem.SecretTargetMode(executable)
	}
	return filesystem.OrdinaryTargetMode(precondition.Target().Mode(), executable, precondition.Target().Kind() == filesystem.KindAbsent)
}

// baselineRow freezes the active baseline of one file action from the
// candidate facts and the content fingerprint.
func baselineRow(candidate Candidate, contentHash deployment.Digest) state.FileBaseline {
	file := candidate.record.File
	return state.FileBaseline{
		TargetPath:          file.TargetRelativePath,
		GroupName:           file.Scope.Group,
		SourcePath:          file.SourceRepositoryPath,
		SourceKind:          file.Kind,
		Layer:               file.Layer,
		BaselineContentHash: contentHash,
		BaselineSourceHash:  sourceHash(candidate),
		ExecutableBits:      uint32(file.SourceExecutableBits),
		Status:              state.StatusActive,
	}
}

// sourceHash is the retained storage fingerprint of the source: the raw
// ciphertext digest for secrets and the ordinary content digest otherwise.
func sourceHash(candidate Candidate) deployment.Digest {
	snapshot := candidate.record.Source.Snapshot()
	if candidate.record.File.Kind == deployment.FileSecret {
		return snapshot.Storage()
	}
	return snapshot.Semantic()
}

// completedRecord marks one durable, baselined target.
func completedRecord(job fileJob) ItemResult {
	return ItemResult{
		TargetPath: job.action.TargetPath,
		Status:     StatusCompleted,
		Secret:     job.candidate.record.File.Kind == deployment.FileSecret,
		Kind:       job.action.Kind,
	}
}

// partialRecord marks one durable target without an equal baseline.
func partialRecord(job fileJob) ItemResult {
	return ItemResult{
		TargetPath: job.action.TargetPath,
		Status:     StatusPartial,
		Secret:     job.candidate.record.File.Kind == deployment.FileSecret,
		Kind:       job.action.Kind,
	}
}

package add

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/state"
)

// execute runs one batch sequentially in plan execution order, writing each
// source, revalidating the target, and establishing the equal baseline. A
// later failure leaves earlier adopted items accurately recorded; the batch
// is never rolled back.
func (service *Service) execute(ctx context.Context, identity RepositoryIdentity, plan BatchPlan) (Result, error) {
	items := plan.Items()
	exec := &executor{service: service, identity: identity, records: make([]ItemResult, 0, len(items))}
	for _, index := range plan.ExecutionOrder() {
		if err := ctx.Err(); err != nil {
			exec.errors = append(exec.errors, err)
			break
		}
		exec.runItem(ctx, items[index])
	}
	return exec.finish(), exec.joinedError()
}

// executor accumulates the per-target records, the per-batch hash key, and
// any write failures of one batch so each step stays under the param limit.
type executor struct {
	service  *Service
	identity RepositoryIdentity
	records  []ItemResult
	errors   []error
	key      [32]byte
	haveKey  bool
}

// runItem writes one item, revalidates the target, and records the outcome.
func (exec *executor) runItem(ctx context.Context, item ItemPlan) {
	outcome, err := exec.write(ctx, item)
	if err != nil {
		exec.errors = append(exec.errors, err)
		exec.records = append(exec.records, partialRecord(item))
		return
	}
	defer clearBytes(outcome.target)
	if targetChanged(item, outcome) {
		exec.records = append(exec.records, partialRecord(item))
		return
	}
	exec.adopt(ctx, item, outcome)
}

// write dispatches one item to its ordinary or secret writer.
func (exec *executor) write(ctx context.Context, item ItemPlan) (writeOutcome, error) {
	if item.Kind() == deployment.FileSecret {
		return exec.service.writeSecret(ctx, exec.identity, item)
	}
	return exec.service.writeOrdinary(ctx, exec.identity, item)
}

// adopt establishes the equal baseline and records the item completed, or
// records it partial when the baseline cannot be established.
func (exec *executor) adopt(ctx context.Context, item ItemPlan, outcome writeOutcome) {
	baseline, err := exec.baseline(item, outcome)
	if err != nil {
		exec.recordFailure(item, err)
		return
	}
	if _, err := exec.service.deps.Baselines.UpsertFileBaseline(exec.identity.Root, exec.identity.Home, baseline); err != nil {
		exec.recordFailure(item, err)
		return
	}
	exec.records = append(exec.records, completedRecord(item))
}

// recordFailure marks one item partial and accumulates the cause.
func (exec *executor) recordFailure(item ItemPlan, err error) {
	exec.errors = append(exec.errors, err)
	exec.records = append(exec.records, partialRecord(item))
}

// baseline builds the equal content/source baseline for one written item.
func (exec *executor) baseline(item ItemPlan, outcome writeOutcome) (state.FileBaseline, error) {
	content, err := exec.contentHash(item, outcome)
	if err != nil {
		return state.FileBaseline{}, err
	}
	return state.FileBaseline{
		TargetPath:          item.TargetRelativePath(),
		GroupName:           item.Scope().Group,
		SourcePath:          item.SourceRepositoryPath(),
		SourceKind:          item.Kind(),
		Layer:               item.Layer(),
		ExecutableBits:      uint32(item.ExecutableBits()),
		Status:              state.StatusActive,
		AppliedAt:           time.Now().UTC(),
		BaselineContentHash: content,
		BaselineSourceHash:  deployment.RawStorage(outcome.published),
	}, nil
}

// contentHash returns the ordinary digest or the keyed secret semantic digest.
func (exec *executor) contentHash(item ItemPlan, outcome writeOutcome) (deployment.Digest, error) {
	if item.Kind() != deployment.FileSecret {
		return deployment.Ordinary(outcome.target), nil
	}
	key, err := exec.recoverKey()
	if err != nil {
		return deployment.Digest{}, err
	}
	return deployment.SecretSemantic(outcome.target, key), nil
}

// recoverKey loads the per-installation hash key once per batch.
func (exec *executor) recoverKey() ([32]byte, error) {
	if exec.haveKey {
		return exec.key, nil
	}
	if exec.service.write.HashKey == nil {
		return [32]byte{}, failure.New(failure.Operational, "add: recover hash key", errors.New("hash key recovery is not configured"))
	}
	key, err := exec.service.write.HashKey.RecoverHashKey()
	if err != nil {
		return [32]byte{}, failure.New(failure.Operational, "add: recover hash key", err)
	}
	exec.key = key
	exec.haveKey = true
	return key, nil
}

// targetChanged reports whether the target still matches the bytes captured
// before the write. A read failure is treated as a change so no baseline is
// established against an unreadable target.
func targetChanged(item ItemPlan, outcome writeOutcome) bool {
	current, err := readValidatedTarget(item.TargetAbsolutePath())
	if err != nil {
		return true
	}
	defer clearBytes(current)
	return !bytes.Equal(current, outcome.target)
}

// completedRecord renders one item as a StatusCompleted record.
func completedRecord(item ItemPlan) ItemResult {
	return record(item, StatusCompleted)
}

// partialRecord renders one item as a StatusPartial record.
func partialRecord(item ItemPlan) ItemResult {
	return record(item, StatusPartial)
}

// record renders one item with its target, source, kind, and status.
func record(item ItemPlan, status ItemStatus) ItemResult {
	return itemRecord(item, status)
}

// finish assembles the result and outcome summary.
func (exec *executor) finish() Result {
	return Result{Items: exec.records, Summary: summarize(exec.records)}
}

// summarize counts the per-target outcome records.
func summarize(records []ItemResult) Summary {
	var summary Summary
	for index := range records {
		countOutcome(records[index].Status, &summary)
	}
	return summary
}

// countOutcome increments the matching summary bucket.
func countOutcome(status ItemStatus, summary *Summary) {
	switch status {
	case StatusCompleted:
		summary.Completed++
	case StatusPartial:
		summary.Partial++
	case StatusPlanned:
		summary.Planned++
	}
}

// joinedError returns nil when every item succeeded, otherwise one
// categorized failure wrapping every recorded cause.
func (exec *executor) joinedError() error {
	if len(exec.errors) == 0 {
		return nil
	}
	return failure.New(failure.Operational, "add: batch completed with errors", errors.Join(exec.errors...))
}

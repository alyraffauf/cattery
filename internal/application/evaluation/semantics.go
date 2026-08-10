package evaluation

import (
	"context"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
	"github.com/alyraffauf/cattery/internal/secrets"
)

func (service *Service) classify(ctx context.Context, assembly reconcile.EvaluationSnapshot) ([]Record, error) {
	semantics := &semanticState{reader: service.state, client: service.secrets, commandLabel: service.commandLabel}
	records := assembly.All()
	evaluated := make([]Record, 0, len(records))
	for _, record := range records {
		fingerprints, err := semantics.fingerprints(fingerprintInput{
			context: ctx, home: assembly.HomePath, record: record,
			includeUnmanagedTargetDigest: service.includeUnmanagedTargetDigest,
		})
		if err != nil {
			return nil, err
		}
		evaluated = append(evaluated, Record{
			Evaluation: record,
			File:       reconcile.ClassifyFile(record, fingerprints),
			Alias:      reconcile.ClassifyAlias(record, fingerprints),
			Retirement: reconcile.ClassifyRetirement(record, assembly.Platform),
			Semantics:  fingerprints,
		})
	}
	return evaluated, nil
}

type fingerprintInput struct {
	context                      context.Context
	home                         string
	record                       reconcile.Evaluation
	includeUnmanagedTargetDigest bool
}

// semanticState recovers the installation hash key at most once per run.
type semanticState struct {
	reader       StateReader
	client       *secrets.Client
	commandLabel string
	key          [32]byte
	haveKey      bool
}

func (state *semanticState) fingerprints(input fingerprintInput) (reconcile.FileSemantics, error) {
	ctx, home, record := input.context, input.home, input.record
	if record.Entry != reconcile.PlanEntryFile {
		if input.includeUnmanagedTargetDigest && record.Target.Kind() == reconcile.KindFile {
			return reconcile.FileSemantics{Target: record.Target.Digest()}, nil
		}
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

func (state *semanticState) secretFingerprints(ctx context.Context, home string, record reconcile.Evaluation) (reconcile.FileSemantics, error) {
	semantics := reconcile.FileSemantics{}
	targetFile := record.Target.Kind() == reconcile.KindFile
	if !targetFile && !SecretDecryptionNeeded(record) {
		return semantics, nil
	}
	if err := state.recover(); err != nil {
		return semantics, err
	}
	if targetFile {
		content, err := ReadTargetContent(home, record, state.commandLabel)
		if err != nil {
			return semantics, err
		}
		semantics.Target = deployment.SecretSemantic(content, state.key)
	}
	if SecretDecryptionNeeded(record) {
		source, err := record.Source.KeyedSemantic(ctx, state.key)
		if err != nil {
			return semantics, categorized(err, state.commandLabel+": decrypt source "+record.File.SourceRepositoryPath)
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

// SecretDecryptionNeeded reports whether classification needs a secret source
// plaintext for this record.
func SecretDecryptionNeeded(record reconcile.Evaluation) bool {
	if record.FileState == nil {
		return record.Target.Kind() == reconcile.KindFile
	}
	return record.Source.Snapshot().Storage() != record.FileState.BaselineSource()
}

func (state *semanticState) recover() error {
	if state.haveKey {
		return nil
	}
	key, err := state.reader.RecoverHashKey()
	if err != nil {
		return failure.New(failure.Operational, state.commandLabel+": recover hash key", err)
	}
	state.key, state.haveKey = key, true
	return nil
}

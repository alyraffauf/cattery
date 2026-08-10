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
		semanticFingerprints, err := semantics.semanticFingerprints(fingerprintInput{
			context: ctx, home: assembly.HomePath, record: record,
			includeUnmanagedTargetDigest: service.includeUnmanagedTargetDigest,
		})
		if err != nil {
			return nil, err
		}
		evaluated = append(evaluated, Record{
			Evaluation: record,
			File:       reconcile.ClassifyFile(record, semanticFingerprints),
			Alias:      reconcile.ClassifyAlias(record, semanticFingerprints),
			Retirement: reconcile.ClassifyRetirement(record, assembly.Platform),
			Semantics:  semanticFingerprints,
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

func (semanticState *semanticState) semanticFingerprints(input fingerprintInput) (reconcile.FileSemantics, error) {
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
	return semanticState.secretSemanticFingerprints(ctx, home, record)
}

func (semanticState *semanticState) secretSemanticFingerprints(ctx context.Context, home string, record reconcile.Evaluation) (reconcile.FileSemantics, error) {
	semantics := reconcile.FileSemantics{}
	targetFile := record.Target.Kind() == reconcile.KindFile
	if !targetFile && !SecretDecryptionNeeded(record) {
		return semantics, nil
	}
	if err := semanticState.loadHashKey(); err != nil {
		return semantics, err
	}
	if targetFile {
		content, err := ReadTargetContent(home, record, semanticState.commandLabel)
		if err != nil {
			return semantics, err
		}
		semantics.Target = deployment.SecretSemantic(content, semanticState.key)
	}
	if SecretDecryptionNeeded(record) {
		source, err := record.Source.KeyedSemantic(ctx, semanticState.key)
		if err != nil {
			return semantics, categorized(err, semanticState.commandLabel+": decrypt source "+record.File.SourceRepositoryPath)
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

func (semanticState *semanticState) loadHashKey() error {
	if semanticState.haveKey {
		return nil
	}
	key, err := semanticState.reader.RecoverHashKey()
	if err != nil {
		return failure.New(failure.Operational, semanticState.commandLabel+": load hash key", err)
	}
	semanticState.key, semanticState.haveKey = key, true
	return nil
}

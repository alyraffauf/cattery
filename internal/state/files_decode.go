package state

import (
	"fmt"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func scanFileBaseline(source scanner) (FileBaseline, error) {
	var baseline FileBaseline
	var raw fileRawRow
	err := source.Scan(&baseline.RepositoryID, &baseline.TargetPath, &baseline.GroupName,
		&baseline.SourcePath, &raw.kind, &raw.layer, &raw.contentHash, &raw.sourceHash,
		&raw.executable, &raw.status, &raw.applied, &raw.retired)
	if err != nil {
		return FileBaseline{}, fmt.Errorf("state: scan file baseline: %w", err)
	}
	if err := decodeFileBaseline(&baseline, raw); err != nil {
		return FileBaseline{}, err
	}
	return baseline, nil
}

// fileRawRow carries the stored text/byte form of one files row.
type fileRawRow struct {
	kind, layer, status, applied string
	retired                      *string
	contentHash, sourceHash      []byte
	executable                   int64
}

func decodeFileBaseline(baseline *FileBaseline, raw fileRawRow) error {
	if err := decodeFileEnums(baseline, raw); err != nil {
		return err
	}
	if err := decodeFileTimes(baseline, raw); err != nil {
		return err
	}
	content, err := decodeDigest(raw.contentHash)
	if err != nil {
		return err
	}
	sourceHash, err := decodeDigest(raw.sourceHash)
	if err != nil {
		return err
	}
	baseline.BaselineContentHash = content
	baseline.BaselineSourceHash = sourceHash
	baseline.ExecutableBits = uint32(raw.executable)
	return nil
}

func decodeFileEnums(baseline *FileBaseline, raw fileRawRow) error {
	kind, err := deployment.ParseFileKind(raw.kind)
	if err != nil {
		return err
	}
	layer, err := deployment.ParseLayer(raw.layer)
	if err != nil {
		return err
	}
	status, err := ParseSourceStatus(raw.status)
	if err != nil {
		return err
	}
	baseline.SourceKind = kind
	baseline.Layer = layer
	baseline.Status = status
	return nil
}

func decodeFileTimes(baseline *FileBaseline, raw fileRawRow) error {
	appliedAt, err := time.Parse(time.RFC3339Nano, raw.applied)
	if err != nil {
		return err
	}
	retiredAt, err := decodeOptionalTimestamp(raw.retired)
	if err != nil {
		return err
	}
	baseline.AppliedAt = appliedAt
	baseline.RetiredAt = retiredAt
	return nil
}

func decodeOptionalTimestamp(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil {
		return nil, fmt.Errorf("state: retired_at: %w", err)
	}
	return &parsed, nil
}

func errMissingFileBaseline(target string) error {
	return fmt.Errorf("state: no file baseline for target %q", target)
}

// errDualActiveRepresentation reports corruption: paths active in both tables.
func errDualActiveRepresentation(count int) error {
	return fmt.Errorf(
		"state: %d paths are active in both files and aliases; reset state to repair",
		count)
}

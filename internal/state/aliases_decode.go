package state

import (
	"fmt"
	"time"
)

func scanAliasBaseline(source scanner) (AliasBaseline, error) {
	var baseline AliasBaseline
	var raw aliasRawRow
	err := source.Scan(&baseline.RepositoryID, &baseline.AliasPath, &baseline.CanonicalTargetPath,
		&baseline.GroupName, &raw.layer, &raw.status, &raw.applied, &raw.retired)
	if err != nil {
		return AliasBaseline{}, fmt.Errorf("state: scan alias baseline: %w", err)
	}
	if err := decodeAliasBaseline(&baseline, raw); err != nil {
		return AliasBaseline{}, err
	}
	return baseline, nil
}

// aliasRawRow carries the stored text/byte form of one aliases row.
type aliasRawRow struct {
	layer, status, applied string
	retired                *string
}

func decodeAliasBaseline(baseline *AliasBaseline, raw aliasRawRow) error {
	layer, err := ParseAliasLayer(raw.layer)
	if err != nil {
		return err
	}
	status, err := ParseSourceStatus(raw.status)
	if err != nil {
		return err
	}
	appliedAt, err := time.Parse(time.RFC3339Nano, raw.applied)
	if err != nil {
		return err
	}
	retiredAt, err := decodeOptionalTimestamp(raw.retired)
	if err != nil {
		return err
	}
	baseline.Layer = layer
	baseline.Status = status
	baseline.AppliedAt = appliedAt
	baseline.RetiredAt = retiredAt
	return nil
}

func errMissingAliasBaseline(alias string) error {
	return fmt.Errorf("state: no alias baseline for alias %q", alias)
}

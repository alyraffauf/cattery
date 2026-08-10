package state

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/pathsafe"
)

func prepareFileBaseline(root, home string, baseline FileBaseline) (string, string, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return "", "", err
	}
	if err := validateFileBaseline(baseline); err != nil {
		return "", "", err
	}
	return root, home, nil
}

func validateFileBaseline(baseline FileBaseline) error {
	if err := validateFilePaths(baseline); err != nil {
		return err
	}
	return validateFileMetadata(baseline)
}

func validateFilePaths(baseline FileBaseline) error {
	if !IsSlashRelative(baseline.TargetPath) {
		return fmt.Errorf("state: file baseline target %q is not a slash-relative path", baseline.TargetPath)
	}
	if !IsSlashRelative(baseline.SourcePath) {
		return fmt.Errorf("state: file baseline source %q is not a slash-relative path", baseline.SourcePath)
	}
	if baseline.GroupName != "" {
		if err := pathsafe.GroupName(baseline.GroupName); err != nil {
			return fmt.Errorf("state: file baseline group: %w", err)
		}
	}
	return nil
}

func validateFileMetadata(baseline FileBaseline) error {
	if !baseline.SourceKind.Valid() {
		return fmt.Errorf("state: file baseline has invalid source kind %q", baseline.SourceKind)
	}
	if !baseline.Layer.Valid() {
		return fmt.Errorf("state: file baseline has invalid layer %q", baseline.Layer)
	}
	if baseline.BaselineContentHash == (deployment.Digest{}) || baseline.BaselineSourceHash == (deployment.Digest{}) {
		return fmt.Errorf("state: file baseline %q has an unset digest", baseline.TargetPath)
	}
	if baseline.ExecutableBits > 0o777 {
		return fmt.Errorf("state: file baseline %q has invalid executable bits %o", baseline.TargetPath, baseline.ExecutableBits)
	}
	return nil
}

func (store *Store) ensureKeyID(kind deployment.FileKind) (*deployment.Digest, error) {
	if kind != deployment.FileSecret {
		return nil, nil
	}
	key, err := store.RecoverHashKey()
	if err != nil {
		return nil, err
	}
	identifier := KeyIDForKey(key)
	return &identifier, nil
}

func applyFileBatch(transaction *sql.Tx, batch fileBatch) error {
	if err := execIn(transaction, repositoryUpsertSQL, batch.root, batch.home, batch.now, batch.now); err != nil {
		return err
	}
	if err := execIn(transaction, fileUpsertSQL,
		batch.root, batch.home, batch.baseline.TargetPath, batch.baseline.GroupName, batch.baseline.SourcePath,
		string(batch.baseline.SourceKind), string(batch.baseline.Layer),
		batch.baseline.BaselineContentHash[:], batch.baseline.BaselineSourceHash[:],
		batch.baseline.ExecutableBits, batch.now); err != nil {
		return err
	}
	if batch.keyID != nil {
		if err := commitKeyIdentifier(transaction, batch.keyID); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) requireRepository(root, home string) (Repository, error) {
	root, home, err := canonicalRepositoryPair(root, home)
	if err != nil {
		return Repository{}, err
	}
	return store.LookupRepository(root, home)
}

func scanAndCommitFile(transaction *sql.Tx, key fileBaselineKey) (FileBaseline, error) {
	return commitStateRead(transaction, func(transaction *sql.Tx) (FileBaseline, error) {
		baseline, err := scanFileBaseline(transaction.QueryRow(fileByPairTargetSQL, key.root, key.home, key.target))
		if errors.Is(err, sql.ErrNoRows) {
			return FileBaseline{}, errMissingFileBaseline(key.target)
		}
		return baseline, err
	})
}

func (store *Store) readFileBaselines(statement, root, home string) ([]FileBaseline, error) {
	if err := store.checkRepresentationCorruption(root, home); err != nil {
		return nil, err
	}
	rows, err := store.database.conn.Query(statement, root, home)
	if err != nil {
		return nil, fmt.Errorf("state: list file baselines: %w", err)
	}
	defer rows.Close()
	var baselines []FileBaseline
	for rows.Next() {
		baseline, err := scanFileBaseline(rows)
		if err != nil {
			return nil, err
		}
		baselines = append(baselines, baseline)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list file baselines: %w", err)
	}
	return CopyFileBaselines(baselines), nil
}

func (store *Store) checkRepresentationCorruption(root, home string) error {
	rows, err := store.database.conn.Query(dualActiveByPairSQL, root, home)
	if err != nil {
		return fmt.Errorf("state: check representation corruption: %w", err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("state: check representation corruption: %w", err)
	}
	if len(paths) > 0 {
		return errDualActiveRepresentation(len(paths))
	}
	return nil
}

func (store *Store) readFileGroups(statement, root, home string) ([]string, error) {
	return store.queryStrings(statement, "file groups", root, home)
}

// queryStrings runs statement with args and returns the single string column
// from every row, wrapping query and iteration errors with label so each caller
// names its own read in the error.
func (store *Store) queryStrings(statement, label string, args ...any) ([]string, error) {
	rows, err := store.database.conn.Query(statement, args...)
	if err != nil {
		return nil, fmt.Errorf("state: list %s: %w", label, err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("state: list %s: %w", label, err)
	}
	return values, nil
}

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

package evaluation

import (
	"io"
	"os"
	"path/filepath"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// ReadTargetContent reads the regular target only while its captured identity
// and mode remain stable. This prevents semantic classification from trusting
// bytes read through a swapped target path.
func ReadTargetContent(home string, record reconcile.Evaluation, commandLabel string) ([]byte, error) {
	if record.Target.Kind() != reconcile.KindFile {
		return nil, nil
	}
	path := filepath.Join(home, filepath.FromSlash(record.TargetPath))
	file, err := openValidatedTarget(path, record, commandLabel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, failure.New(failure.Operational, commandLabel+": read target "+record.TargetPath, err)
	}
	if err := validateOpenedTarget(targetReadInput{file: file, record: record, path: path, commandLabel: commandLabel}); err != nil {
		return nil, err
	}
	return content, nil
}

func openValidatedTarget(path string, record reconcile.Evaluation, commandLabel string) (*os.File, error) {
	entry, err := os.Lstat(path)
	if err != nil {
		return nil, failure.New(failure.Operational, commandLabel+": read target "+record.TargetPath, err)
	}
	if !entry.Mode().IsRegular() || !record.Target.Identity().SameFileInfo(entry) {
		return nil, failure.New(failure.Operational, commandLabel+": target changed "+record.TargetPath, nil)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, failure.New(failure.Operational, commandLabel+": read target "+record.TargetPath, err)
	}
	if err := validateOpenedTarget(targetReadInput{file: file, record: record, path: path, commandLabel: commandLabel}); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

type targetReadInput struct {
	file         *os.File
	record       reconcile.Evaluation
	path         string
	commandLabel string
}

func validateOpenedTarget(input targetReadInput) error {
	info, err := input.file.Stat()
	if err != nil || !input.record.Target.Identity().SameFileInfo(info) || info.Mode().Perm() != input.record.Target.Mode() {
		return failure.New(failure.Operational, input.commandLabel+": target changed "+input.path, err)
	}
	return nil
}

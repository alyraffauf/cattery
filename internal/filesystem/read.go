package filesystem

import (
	"fmt"
	"io"
	"os"
)

// ReadFrozen reads the regular file named by precondition through one open
// descriptor and proves that descriptor and the path stayed equal to the
// frozen entry before and after the read.
func ReadFrozen(precondition Precondition) ([]byte, error) {
	frozen := precondition.Target()
	if frozen.Kind() != KindFile {
		return nil, fmt.Errorf("filesystem: frozen source is not a regular file")
	}
	if err := precondition.Revalidate(); err != nil {
		return nil, err
	}

	file, err := os.Open(targetPath(precondition.Destination()))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := validateOpenedFile(file, frozen); err != nil {
		return nil, err
	}

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if err := validateOpenedFile(file, frozen); err != nil {
		return nil, err
	}
	if err := precondition.Revalidate(); err != nil {
		return nil, err
	}
	return content, nil
}

func validateOpenedFile(file *os.File, frozen TargetFacts) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !MatchesIdentityAndMode(frozen.Identity(), info, frozen.Mode()) {
		return fmt.Errorf("filesystem: opened source changed at %s", frozen.Identity().Path())
	}
	return nil
}

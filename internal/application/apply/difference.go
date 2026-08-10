package apply

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyraffauf/cattery/internal/diff"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/filesystem"
	"github.com/alyraffauf/cattery/internal/reconcile"
)

// differenceProvider binds safe diff rendering to the immutable candidates
// collected for one apply. The prompt never re-resolves repository state.
func (service *Service) differenceProvider(candidates Candidates) DifferenceProvider {
	byPath := candidatesByPath(candidates)
	return func(ctx context.Context, target string) (SafeDifference, bool) {
		candidate, ok := byPath[target]
		if !ok {
			return SafeDifference{}, false
		}
		return service.safeDifference(ctx, candidates.Home(), candidate)
	}
}

// safeDifference reads and revalidates one ordinary target before building
// the output-safe diff. Secret records remain payload-free by construction.
func (service *Service) safeDifference(ctx context.Context, home string, candidate Candidate) (SafeDifference, bool) {
	if err := ctx.Err(); err != nil || candidate.record.Entry != reconcile.PlanEntryFile || candidate.record.Target.Kind() != reconcile.KindFile {
		return SafeDifference{}, false
	}
	destination := filesystem.Destination{Root: home, Relative: candidate.record.TargetPath}
	precondition, err := filesystem.Freeze(destination)
	if err != nil || precondition.Target().Kind() != filesystem.KindFile {
		return SafeDifference{}, false
	}
	content, err := readFrozenTarget(precondition)
	if err != nil {
		return SafeDifference{}, false
	}
	record, err := diff.Build(candidate.record, content)
	if err != nil {
		return SafeDifference{}, false
	}
	return safeDifferenceFrom(record), true
}

func readFrozenTarget(precondition filesystem.Precondition) ([]byte, error) {
	destination := precondition.Destination()
	path := filepath.Join(destination.Root, filepath.FromSlash(destination.Relative))
	file, err := os.Open(path)
	if err != nil {
		return nil, failure.New(failure.Operational, "apply: read target "+destination.Relative, err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, failure.New(failure.Operational, "apply: read target "+destination.Relative, err)
	}
	if err := precondition.Revalidate(); err != nil {
		return nil, failure.New(failure.Operational, "apply: target changed "+destination.Relative, err)
	}
	return content, nil
}

func safeDifferenceFrom(record diff.SafeRecord) SafeDifference {
	lines := strings.TrimSuffix(record.Lines(), "\n")
	var renderedLines []string
	if lines != "" {
		renderedLines = strings.Split(lines, "\n")
	}
	return SafeDifference{
		Tag:        diffTagOf(record.Tag()),
		SourceSize: int(record.SourceSize()),
		TargetSize: int(record.TargetSize()),
		SourceHash: fmt.Sprintf("%x", record.SourceHash()),
		TargetHash: fmt.Sprintf("%x", record.TargetHash()),
		Lines:      renderedLines,
	}
}

func diffTagOf(tag diff.Tag) DiffTag {
	switch tag {
	case diff.TagText:
		return DiffTagText
	case diff.TagBinary:
		return DiffTagBinary
	case diff.TagSecret:
		return DiffTagSecret
	default:
		return DiffTagNone
	}
}

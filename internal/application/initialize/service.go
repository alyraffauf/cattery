package initialize

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/state"
)

// repositoryDirectoryMode is the creation mode of a missing repository root;
// the umask may narrow it and Cattery never rewrites existing permissions.
const repositoryDirectoryMode os.FileMode = 0o755

// Service performs one repository initialization against an acquired state
// store. Construction is side-effect-free: every filesystem and database
// effect happens inside Initialize.
type Service struct {
	home  string
	store *state.Store
}

// NewService constructs the initialization service bound to the dependencies.
func NewService(dependencies Dependencies) *Service {
	return &Service{home: dependencies.Home, store: dependencies.Store}
}

// Initialize creates the repository directory when missing and registers the
// canonical pair as the sole default of the canonical home. A non-directory
// entry and every overlap are rejected before creation, with the canonical
// form rechecked after creation. No manifest, marker, sample file, Git
// repository, or commit is ever created.
func (service *Service) Initialize(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := service.store.EnsureAcquired(); err != nil {
		return Result{}, failure.New(failure.Operational, "initialize: acquire state store", err)
	}
	environment, err := service.prepare(request)
	if err != nil {
		return Result{}, err
	}
	anchors := roots{
		repository:     environment.path,
		home:           environment.home,
		stateDirectory: environment.stateDirectory,
	}
	repository, err := ensureRoot(anchors)
	if err != nil {
		return Result{}, err
	}
	row, err := service.store.SetDefaultRepository(repository, environment.home)
	if err != nil {
		return Result{}, failure.New(failure.Operational, "initialize: register repository", err)
	}
	return Result{Repository: RegisteredRepository{
		RootPath: row.RootPath, HomePath: row.HomePath, IsDefault: row.IsDefault,
	}}, nil
}

// environment bundles the canonical inputs every initialization needs.
type environment struct {
	home           string
	stateDirectory string
	path           string
}

// prepare resolves the canonical home, protected state directory, and path.
func (service *Service) prepare(request Request) (environment, error) {
	home, err := canonicalHome(service.home)
	if err != nil {
		return environment{}, err
	}
	stateDirectory, err := service.stateDirectory()
	if err != nil {
		return environment{}, err
	}
	path, err := resolvePath(request.Path)
	if err != nil {
		return environment{}, err
	}
	return environment{home: home, stateDirectory: stateDirectory, path: path}, nil
}

// roots bundles the canonical identity anchors threaded through initialization.
type roots struct {
	repository     string
	home           string
	stateDirectory string
}

// ensureRoot returns the canonical repository root, creating it first when
// missing, after validating the Section 6.1 overlaps.
func ensureRoot(anchors roots) (string, error) {
	existing, err := existingDirectory(anchors.repository)
	if err != nil {
		return "", err
	}
	if existing {
		return existingRoot(anchors)
	}
	return createdRoot(anchors)
}

// existingRoot canonicalizes and validates an existing repository directory.
func existingRoot(anchors roots) (string, error) {
	return canonicalRepositoryRoot(anchors)
}

// createdRoot materializes a missing root, validating the Section 6.1 overlaps
// before MkdirAll and rechecking the canonical form after creation.
func createdRoot(anchors roots) (string, error) {
	canonical, err := canonicalRepositoryRoot(anchors)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(canonical, repositoryDirectoryMode); err != nil {
		return "", failure.New(failure.Operational, "initialize: create repository directory", err)
	}
	return recheckRoot(anchors)
}

// recheckRoot re-canonicalizes a just-created root and revalidates the overlaps before registration.
func recheckRoot(anchors roots) (string, error) {
	rechecked, err := canonicalRepositoryRoot(anchors)
	if err != nil {
		return "", err
	}
	if err := requireDirectory(rechecked); err != nil {
		return "", failure.New(failure.Operational, "initialize: recheck repository directory", err)
	}
	return rechecked, nil
}

// canonicalRepositoryRoot canonicalizes the repository path and rejects the
// Section 6.1 repository/home/state overlaps in one step.
func canonicalRepositoryRoot(anchors roots) (string, error) {
	canonical, err := canonicalPath(anchors.repository)
	if err != nil {
		return "", err
	}
	if err := checkOverlaps(canonical, anchors.home, anchors.stateDirectory); err != nil {
		return "", err
	}
	return canonical, nil
}

// canonicalHome resolves the home to canonical form, requiring an existing real directory (Section 6.2).
func canonicalHome(home string) (string, error) {
	canonical, err := pathsafe.CanonicalRoot(home)
	if err != nil {
		return "", failure.New(failure.Operational, "initialize: resolve home", err)
	}
	if err := requireDirectory(canonical); err != nil {
		return "", failure.New(failure.Operational, "initialize: home is not an existing directory", err)
	}
	return canonical, nil
}

// canonicalPath resolves path to canonical absolute form, wrapping resolver failures as operational.
func canonicalPath(path string) (string, error) {
	canonical, err := pathsafe.CanonicalRoot(path)
	if err != nil {
		return "", failure.New(failure.Operational, "initialize: canonicalize repository path", err)
	}
	return canonical, nil
}

// stateDirectory returns the canonical directory holding the store's database, the protected state tree for the Section 6.1 overlap checks.
func (service *Service) stateDirectory() (string, error) {
	database := service.store.Database()
	if database == nil {
		return "", failure.New(failure.Operational, "initialize: state store is not acquired", nil)
	}
	return filepath.Dir(database.Path()), nil
}

// resolvePath selects the requested path, defaulting an empty request to the current working directory.
func resolvePath(requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	working, err := os.Getwd()
	if err != nil {
		return "", failure.New(failure.Operational, "initialize: resolve working directory", err)
	}
	return working, nil
}

// existingDirectory reports whether path names an existing real directory. An
// existing non-directory is rejected as InvalidInput; a missing path returns
// false so the caller can create it.
func existingDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return false, failure.New(failure.InvalidInput, "initialize: repository path is not a directory", nil)
		}
		return true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, failure.New(failure.Operational, "initialize: inspect repository path", err)
	}
	return false, nil
}

// requireDirectory reports whether path names an existing real directory.
func requireDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return failure.New(failure.Operational, "initialize: inspect directory "+path, err)
	}
	if !info.IsDir() {
		return failure.New(failure.InvalidInput, "initialize: "+path+" is not a directory", nil)
	}
	return nil
}

// checkOverlaps rejects a canonical root that equals or contains home, or
// overlaps the state directory, evaluating each relation natively and with
// the portable NFC-plus-EqualFold equivalence. A strict descendant of home is
// legal.
func checkOverlaps(repository, home, stateDirectory string) error {
	if overlapsRoot(repository, home) {
		return failure.New(failure.InvalidInput,
			fmt.Sprintf("initialize: repository %q overlaps home %q", repository, home), nil)
	}
	if pathsafe.ProtectedTree(repository, stateDirectory) {
		return failure.New(failure.InvalidInput,
			fmt.Sprintf("initialize: repository %q overlaps the state directory %q", repository, stateDirectory), nil)
	}
	return nil
}

// overlapsRoot reports whether repository equals home or contains home,
// natively or under portable equivalence. A strict descendant root is legal,
// so the symmetric ProtectedTree check does not apply here.
func overlapsRoot(repository, home string) bool {
	if pathsafe.Equal(repository, home) || pathsafe.Contains(repository, home) {
		return true
	}
	repositorySegments := segmentsOf(repository)
	homeSegments := segmentsOf(home)
	return pathsafe.PathsEquivalent(repositorySegments, homeSegments) ||
		pathsafe.IsParentEquivalent(repositorySegments, homeSegments)
}

// segmentsOf returns the cleaned absolute path segments, dropping the leading
// separator so absolute paths compare by their real components.
func segmentsOf(path string) []string {
	raw := strings.Split(filepath.Clean(path), string(filepath.Separator))
	segments := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

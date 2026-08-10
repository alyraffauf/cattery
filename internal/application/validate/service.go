package validate

import (
	"context"
	"encoding/json"
	"os"
	"sort"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/repository"
	"github.com/alyraffauf/cattery/internal/selection"
)

// Service performs one repository validation against the injectable source
// and compiler ports. Construction is side-effect-free: repository scanning
// and compilation happen only inside Validate.
type Service struct {
	source         RepositorySource
	protectedTrees []string
	compiler       Compiler
}

// NewService constructs the validation service bound to the dependencies.
func NewService(dependencies Dependencies) *Service {
	return &Service{
		source:         dependencies.RepositorySource,
		protectedTrees: dependencies.ProtectedTrees,
		compiler:       dependencies.Compiler,
	}
}

// Validate resolves the canonical repository, compiles and validates the full
// Linux and Darwin plans, checks every secret's JSON storage shape, and
// reports counts of the selected scopes. Global validation covers every scope
// on both platforms before the selection filters the reported counts.
func (service *Service) Validate(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	identity, err := service.resolve(request.Repository)
	if err != nil {
		return Result{}, err
	}
	linux, darwin, err := service.fullPlans(identity)
	if err != nil {
		return Result{}, err
	}
	chosen, err := selection.CompiledOnly(linux.Groups(), request.Groups)
	if err != nil {
		return Result{}, failure.New(failure.InvalidInput, "validate: select groups", err)
	}
	if len(request.Groups) == 0 {
		return Result{Platforms: platformCounts(linux, darwin)}, nil
	}
	linux, darwin, err = service.compilePair(identity, chosen.Groups)
	if err != nil {
		return Result{}, err
	}
	return Result{Platforms: platformCounts(linux, darwin)}, nil
}

// fullPlans compiles both full platform plans and checks every secret's JSON
// storage shape, so an invalid unselected scope cannot hide.
func (service *Service) fullPlans(identity RepositoryIdentity) (deployment.Plan, deployment.Plan, error) {
	linux, darwin, err := service.compilePair(identity, nil)
	if err != nil {
		return deployment.Plan{}, deployment.Plan{}, err
	}
	if err := service.checkSecretShapes(linux, darwin); err != nil {
		return deployment.Plan{}, deployment.Plan{}, err
	}
	return linux, darwin, nil
}

// resolve maps the raw repository fields onto the selection request and
// resolves the canonical pair through the injected source.
func (service *Service) resolve(input RepositoryInput) (RepositoryIdentity, error) {
	identity, err := service.source.Resolve(repositoryRequest(input))
	if err != nil {
		return RepositoryIdentity{}, failure.New(failure.InvalidInput, "validate: resolve repository", err)
	}
	return identity, nil
}

// repositoryRequest mechanically copies the raw repository fields into the
// selection request shape.
func repositoryRequest(input RepositoryInput) selection.RepositoryRequest {
	return selection.RepositoryRequest{
		RawExplicit: input.RawExplicit,
		ExplicitSet: input.ExplicitSet,
		RawEnv:      input.RawEnv,
		EnvSet:      input.EnvSet,
		WorkingDir:  input.WorkingDir,
	}
}

// compilePair compiles and validates the platform pair, each restricted to
// the selection (nil selects everything). Compilation always validates every
// scope, selected or not.
func (service *Service) compilePair(identity RepositoryIdentity, selected []string) (deployment.Plan, deployment.Plan, error) {
	linux, err := service.compile(identity, deployment.LayerLinux, selected)
	if err != nil {
		return deployment.Plan{}, deployment.Plan{}, err
	}
	darwin, err := service.compile(identity, deployment.LayerDarwin, selected)
	if err != nil {
		return deployment.Plan{}, deployment.Plan{}, err
	}
	return linux, darwin, nil
}

// compile validates the entire repository for one platform and returns the
// plan restricted to the selection.
func (service *Service) compile(identity RepositoryIdentity, layer deployment.Layer, selected []string) (deployment.Plan, error) {
	plan, err := service.compiler.Compile(repository.CompileInput{
		Platform:       layer,
		RepositoryRoot: identity.Root,
		HomeRoot:       identity.Home,
		Protected:      service.protectedTrees,
		Selected:       selected,
	})
	if err != nil {
		return deployment.Plan{}, compileFailure("validate: compile plan", err)
	}
	return plan, nil
}

// checkSecretShapes rejects any empty or malformed JSON secret source in the
// full plans. This is storage-shape validation only: it never decrypts.
func (service *Service) checkSecretShapes(plans ...deployment.Plan) error {
	for _, plan := range plans {
		if err := service.checkPlanShapes(plan); err != nil {
			return err
		}
	}
	return nil
}

// checkPlanShapes requires every secret source of one plan to be valid.
func (service *Service) checkPlanShapes(plan deployment.Plan) error {
	for _, file := range plan.Files() {
		if file.Kind != deployment.FileSecret {
			continue
		}
		if err := checkSecretShape(file.SourceAbsolutePath); err != nil {
			return err
		}
	}
	return nil
}

// checkSecretShape requires one secret source to be nonempty valid JSON.
func checkSecretShape(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return failure.New(failure.Operational, "validate: read secret "+path, err)
	}
	if len(data) == 0 || !json.Valid(data) {
		return failure.New(failure.InvalidInput, "validate: secret "+path+" is not nonempty valid JSON", nil)
	}
	return nil
}

// platformCounts projects the two compiled plans into sorted platform records.
func platformCounts(linux, darwin deployment.Plan) []PlatformCount {
	records := []PlatformCount{platformRecord(darwin), platformRecord(linux)}
	sort.Slice(records, func(first, second int) bool {
		return records[first].Platform < records[second].Platform
	})
	return records
}

// platformRecord counts the selected scopes of one platform plan.
func platformRecord(plan deployment.Plan) PlatformCount {
	record := PlatformCount{
		Platform: plan.Platform(),
		Files:    len(plan.Files()),
		Aliases:  len(plan.Aliases()),
		Groups:   len(plan.Groups()),
	}
	for _, file := range plan.Files() {
		if file.Kind == deployment.FileSecret {
			record.Secrets++
		}
	}
	return record
}

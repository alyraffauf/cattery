package bootstrap

import (
	"io"

	"github.com/alyraffauf/cattery/internal/application/add"
	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/application/initialize"
	"github.com/alyraffauf/cattery/internal/application/inspect"
	applicationrepository "github.com/alyraffauf/cattery/internal/application/repository"
	"github.com/alyraffauf/cattery/internal/application/validate"
	"github.com/alyraffauf/cattery/internal/cli"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/selection"
	"github.com/alyraffauf/cattery/internal/state"
)

// Applications bundles the constructed application services of one build.
type Applications struct {
	Initialize *initialize.Service
	Validate   *validate.Service
	Inspect    *inspect.Service
	Apply      *apply.Service
	Add        *add.Service
}

// ApplicationsInput carries the adapters, home, platform, protected
// trees, and prompt streams of one application build.
type ApplicationsInput struct {
	Adapters   Adapters
	Home       string
	Platform   deployment.Layer
	Protected  []string
	Stdin      io.Reader
	Stderr     io.Writer
	IsTerminal func(int) bool
}

// shared bundles the adapter instances shared across the services.
type shared struct {
	compiler    compilerAdapter
	state       stateReaderAdapter
	baselines   baselineAdapter
	transitions transitionAdapter
	retirements retirementAdapter
	resolver    *selection.RepositoryResolver
	prompt      *cli.DecisionPrompt
}

// newShared constructs the shared adapters over one applications input.
func newShared(input ApplicationsInput) shared {
	stateReader := stateReaderAdapter{store: input.Adapters.Store}
	return shared{
		compiler:    compilerAdapter{},
		state:       stateReader,
		baselines:   baselineAdapter{store: input.Adapters.Store},
		transitions: transitionAdapter{store: input.Adapters.Store},
		retirements: retirementAdapter{store: input.Adapters.Store},
		resolver:    selection.NewRepositoryResolver(input.Home, input.Adapters.Store),
		prompt: cli.NewDecisionPrompt(cli.PromptInput{
			Stdin:      input.Stdin,
			Stderr:     input.Stderr,
			IsTerminal: input.IsTerminal,
		}),
	}
}

// repositorySourceOf adapts one resolver into the application identity.
func repositorySourceOf[T any](resolver *selection.RepositoryResolver, convert func(state.Repository) T) repositorySource[T] {
	return repositorySource[T]{resolver: resolver, convert: convert}
}

// BuildApplications wires the adapters into every application service
// through its purpose-named ports; no constructor opens, probes, or
// mutates anything.
func BuildApplications(input ApplicationsInput) Applications {
	shared := newShared(input)
	return Applications{
		Initialize: buildInitialize(input, shared),
		Validate:   buildValidate(input, shared),
		Inspect:    buildInspect(input, shared),
		Apply:      buildApply(input, shared),
		Add:        buildAdd(input, shared),
	}
}

// buildInitialize wires the repository initialization service.
func buildInitialize(input ApplicationsInput, shared shared) *initialize.Service {
	return initialize.NewService(initialize.Dependencies{
		Home:  input.Home,
		Store: input.Adapters.Store,
	})
}

// buildValidate wires the repository validation service.
func buildValidate(input ApplicationsInput, shared shared) *validate.Service {
	return validate.NewService(validate.Dependencies{
		RepositorySource: repositorySourceOf(shared.resolver, repositoryIdentity),
		Compiler:         shared.compiler,
		ProtectedTrees:   input.Protected,
	})
}

// buildInspect wires the inspection service.
func buildInspect(input ApplicationsInput, shared shared) *inspect.Service {
	return inspect.NewService(inspect.Dependencies{
		RepositorySource: repositorySourceOf(shared.resolver, repositoryIdentity),
		Compiler:         shared.compiler,
		State:            shared.state,
		Secrets:          input.Adapters.SOPS,
		ProtectedTrees:   input.Protected,
		Platform:         string(input.Platform),
	})
}

// buildApply wires the apply service with the prompt resolver.
func buildApply(input ApplicationsInput, shared shared) *apply.Service {
	return apply.NewService(apply.Dependencies{
		RepositorySource: repositorySourceOf(shared.resolver, repositoryIdentity),
		Compiler:         shared.compiler,
		State:            shared.state,
		Baselines:        shared.baselines,
		Transitions:      shared.transitions,
		Retirements:      shared.retirements,
		Client:           input.Adapters.SOPS,
		Secrets:          input.Adapters.SOPS,
		Replacer:         input.Adapters.Replacer,
		Hooks:            hookAdapter{},
		Probe:            &probeAdapter{},
		Resolver:         shared.prompt,
		ProtectedTrees:   input.Protected,
		Platform:         string(input.Platform),
	})
}

// buildAdd wires the add service with its secret execution ports.
func buildAdd(input ApplicationsInput, shared shared) *add.Service {
	return add.NewServiceWithWrites(add.Dependencies{
		RepositorySource: repositorySourceOf(shared.resolver, repositoryIdentity),
		Compiler:         shared.compiler,
		Writer:           input.Adapters.Replacer,
		Baselines:        shared.baselines,
	}, add.WriteDependencies{
		Secrets: input.Adapters.SOPS,
		HashKey: input.Adapters.Store,
	})
}

// repositoryIdentity projects the backend repository into the application contract.
func repositoryIdentity(repository state.Repository) applicationrepository.RepositoryIdentity {
	return applicationrepository.RepositoryIdentity{Root: repository.RootPath, Home: repository.HomePath}
}

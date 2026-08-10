package bootstrap

import (
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/alyraffauf/cattery/internal/cli"
	"github.com/alyraffauf/cattery/internal/deployment"
)

// BuildInput carries the process values of one application build.
type BuildInput struct {
	Streams     cli.Streams
	WorkingDir  string
	Environment []string
	IsTerminal  func(int) bool
	StateHome   string
	Now         func() time.Time
	Protected   []string
}

// Build composes one fresh opaque application: the runtime values, the
// adapters, the application services, and the CLI dependencies. Bootstrap
// never executes Cobra and never maps exits; version, help, and parse
// failures touch no backend.
func Build(input BuildInput) *cli.Application {
	logger := NewLoggerResources(input.Streams.Stderr)
	adapters := NewAdapters(input.StateHome, input.Now)
	services := BuildApplications(ApplicationsInput{
		Adapters:   adapters,
		Home:       envValue(input.Environment, "HOME"),
		Platform:   currentPlatform(),
		Protected:  input.Protected,
		Stdin:      input.Streams.Stdin,
		Stderr:     input.Streams.Stderr,
		IsTerminal: input.IsTerminal,
	})
	runtimeValues := cli.NewRuntime(cli.RuntimeInput{
		Streams:     input.Streams,
		WorkingDir:  input.WorkingDir,
		Environment: input.Environment,
		IsTerminal:  input.IsTerminal,
		SetVerbose: func(verbose bool) {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			logger.Level.Set(level)
		},
	})
	return cli.NewApplication(cli.Dependencies{
		Initialize: services.Initialize,
		Validate:   services.Validate,
		Version:    services.Version,
		Status:     services.Inspect,
		Diff:       services.Inspect,
		Add:        services.Add,
		Apply:      services.Apply,
	}, runtimeValues)
}

// envValue returns the value of one environment entry.
func envValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// currentPlatform derives the deployment layer from the runtime GOOS.
func currentPlatform() deployment.Layer {
	platform, err := deployment.ParseLayer(runtime.GOOS)
	if err != nil {
		return ""
	}
	return platform
}

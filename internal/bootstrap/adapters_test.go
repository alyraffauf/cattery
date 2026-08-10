package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/repository"
)

func TestBootstrapAdapters(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"construction creates nothing", testAdaptersSideEffects},
		{"two builds share nothing", testAdaptersIsolation},
		{"logger resources isolate", testAdaptersLogger},
		{"probe reports missing sops", testAdaptersProbe},
		{"compiler stays pure", testAdaptersCompiler},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// bytesBuffer is a minimal thread-safe writer for logger assertions.
type bytesBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *bytesBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, data...)
	return len(data), nil
}

// compileInput builds one bogus compile input.
func compileInput() repository.CompileInput {
	return repository.CompileInput{Platform: deployment.LayerLinux}
}

// kindIs reports whether err carries the given failure kind.
func kindIs(err error, want failure.Kind) bool {
	kind, ok := failure.HasKind(err)
	return ok && kind == want
}

// fixedClock returns one deterministic instant.
func fixedClock() func() time.Time {
	instant := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time { return instant }
}

func testAdaptersSideEffects(t *testing.T) {
	home := filepath.Join(t.TempDir(), "state")
	NewAdapters(home, fixedClock())
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("construction must create nothing, state home exists: %v", err)
	}
}

func testAdaptersIsolation(t *testing.T) {
	first := NewAdapters(filepath.Join(t.TempDir(), "state"), fixedClock())
	second := NewAdapters(filepath.Join(t.TempDir(), "state"), fixedClock())
	if first.Store == second.Store {
		t.Fatal("two builds must own distinct stores")
	}
	if first.Replacer == second.Replacer {
		t.Fatal("two builds must own distinct replacers")
	}
	if first.SOPS == second.SOPS {
		t.Fatal("two builds must own distinct sops clients")
	}
}

func testAdaptersLogger(t *testing.T) {
	stderr := &bytesBuffer{}
	first := NewLoggerResources(stderr)
	second := NewLoggerResources(stderr)
	first.Logger.Info("first")
	second.Logger.Info("second")
	if first.Level == second.Level || first.Logger == second.Logger {
		t.Fatal("two builds must own distinct logger resources")
	}
}

func testAdaptersProbe(t *testing.T) {
	t.Setenv("PATH", "")
	probe := &probeAdapter{}
	err := probe.Probe(context.Background())
	if err == nil || !kindIs(err, failure.Dependency) {
		t.Fatalf("probe error = %v, want a dependency failure", err)
	}
}

func testAdaptersCompiler(t *testing.T) {
	adapter := compilerAdapter{}
	if _, err := adapter.Compile(compileInput()); err == nil {
		t.Fatal("compiling a bogus input must fail")
	}
}

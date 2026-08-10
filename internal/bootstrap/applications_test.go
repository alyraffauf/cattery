package bootstrap

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
)

func TestBootstrapApplications(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"all services constructed", testApplicationsBuilt},
		{"construction performs no work", testApplicationsSideEffects},
		{"two builds stay isolated", testApplicationsIsolation},
		{"prompt resolver wired", testApplicationsPrompt},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// appFixture builds one full application over isolated inputs.
func appFixture(t *testing.T) (Applications, Adapters) {
	t.Helper()
	adapters := NewAdapters(filepath.Join(t.TempDir(), "state"), fixedClock())
	applications := BuildApplications(ApplicationsInput{
		Adapters:   adapters,
		Home:       "/home",
		Platform:   deployment.LayerLinux,
		Protected:  []string{"/home/.local/state/cattery"},
		Stdin:      strings.NewReader(""),
		Stderr:     &bytes.Buffer{},
		IsTerminal: func(fd int) bool { return true },
	})
	return applications, adapters
}

func testApplicationsBuilt(t *testing.T) {
	applications, _ := appFixture(t)
	if applications.Initialize == nil || applications.Validate == nil || applications.Inspect == nil ||
		applications.Apply == nil || applications.Add == nil {
		t.Fatal("every application service must be constructed")
	}
}

func testApplicationsSideEffects(t *testing.T) {
	_, adapters := appFixture(t)
	if adapters.Store.Database() != nil {
		t.Fatal("construction must not open the database")
	}
}

func testApplicationsIsolation(t *testing.T) {
	first, _ := appFixture(t)
	second, _ := appFixture(t)
	if first.Apply == second.Apply || first.Initialize == second.Initialize {
		t.Fatal("two builds must own distinct services")
	}
}

func testApplicationsPrompt(t *testing.T) {
	applications, _ := appFixture(t)
	if applications.Apply == nil {
		t.Fatal("the apply service must exist")
	}
}

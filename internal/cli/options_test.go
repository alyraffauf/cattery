package cli

import (
	"strings"
	"testing"
)

func TestCLIOptions(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"explicit presence", testOptionsPresence},
		{"interspersed groups", testOptionsInterspersed},
		{"defensive copies", testOptionsCopies},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testOptionsPresence(t *testing.T) {
	var zero Options
	if zero.RepositorySet {
		t.Fatal("zero Options must leave presence false")
	}
	options := Options{Repository: "repo", RepositorySet: true, Verbose: true}
	if !options.RepositorySet || options.Repository != "repo" || !options.Verbose {
		t.Fatalf("options = %+v, want the explicit values preserved", options)
	}
}

func testOptionsInterspersed(t *testing.T) {
	options := Options{Groups: []string{"first", "middle", "last"}, Repository: "repo", RepositorySet: true}
	if strings.Join(options.Groups, ",") != "first,middle,last" {
		t.Fatalf("groups = %v, want the raw order preserved", options.Groups)
	}
	if !options.RepositorySet {
		t.Fatal("a flag after group arguments must still set presence")
	}
}

func testOptionsCopies(t *testing.T) {
	options := Options{Groups: []string{"a", "b"}}
	copied := options.GroupsCopy()
	copied[0] = "mutated"
	if options.Groups[0] != "a" {
		t.Fatal("mutating a GroupsCopy must not reach the options")
	}
	if len(options.GroupsCopy()) != 2 {
		t.Fatal("GroupsCopy must preserve the argument count")
	}
}

package repository

import "testing"

func TestIgnoreMatcher(t *testing.T) {
	matcher, err := parseIgnoreMatcher("# repository-only files\nREADME.md\nhelpers/\n**/*.example\n")
	if err != nil {
		t.Fatalf("parse ignore matcher: %v", err)
	}
	scenarios := []struct {
		path        string
		isDirectory bool
		want        bool
	}{
		{"README.md", false, true},
		{"apps/README.md", false, true},
		{"helpers", true, true},
		{"apps/helpers/task", false, true},
		{".config/app.example", false, true},
		{".config/app/config", false, false},
	}
	for _, scenario := range scenarios {
		if got := matcher.ignores(scenario.path, scenario.isDirectory); got != scenario.want {
			t.Errorf("ignores(%q, %t) = %t, want %t", scenario.path, scenario.isDirectory, got, scenario.want)
		}
	}
}

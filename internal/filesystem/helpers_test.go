package filesystem

import "testing"

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustFail(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected failure, got success")
	}
}

func mustFreeze(t *testing.T, root, relative string) Precondition {
	t.Helper()
	precondition, err := Freeze(Destination{Root: root, Relative: relative})
	if err != nil {
		t.Fatalf("Freeze(%q): %v", relative, err)
	}
	return precondition
}

func mustCapture(t *testing.T, path string) TargetFacts {
	t.Helper()
	facts, err := CaptureTarget(path)
	if err != nil {
		t.Fatalf("CaptureTarget(%q): %v", path, err)
	}
	return facts
}

func mustRejectFreeze(t *testing.T, root, relative string) {
	t.Helper()
	if _, err := Freeze(Destination{Root: root, Relative: relative}); err == nil {
		t.Fatalf("Freeze(%q) must fail", relative)
	}
}

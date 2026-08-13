package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/failure"
)

func TestDecisionPrompt(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"each choice", testPromptChoices},
		{"invalid input reprompts", testPromptInvalid},
		{"secret restriction", testPromptSecret},
		{"diff displays automatically", testPromptDiff},
		{"non-terminal refusal", testPromptNonTTY},
		{"eof handling", testPromptEOF},
		{"empty is not destructive", testPromptDefault},
		{"final confirmation", testPromptConfirmation},
		{"writer errors", testPromptWriterError},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

// promptFixture builds one prompt over the given stdin lines.
func promptFixture(t *testing.T, answers []string, diff func(context.Context, string) (apply.SafeDifference, bool)) (*DecisionPrompt, *bytes.Buffer) {
	t.Helper()
	stderr := &bytes.Buffer{}
	input := ""
	if len(answers) > 0 {
		input = strings.Join(answers, "\n") + "\n"
	}
	prompt := NewDecisionPrompt(PromptInput{
		Stdin:      strings.NewReader(input),
		Stderr:     stderr,
		IsTerminal: func(fd int) bool { return true },
		Diff:       diff,
	})
	return prompt, stderr
}

// driftRequest freezes one drift decision request with the diff choice.
func driftRequest() apply.DecisionRequest {
	request, err := apply.NewDecisionRequest(apply.DecisionRequestInput{
		TargetPath: "a.conf",
		Choices:    []apply.DecisionChoice{apply.ChoiceOverwrite, apply.ChoiceSkip, apply.ChoiceAbort},
	})
	if err != nil {
		panic(err)
	}
	return request
}

// secretRequest freezes one secret decision request without diff.
func secretRequest() apply.DecisionRequest {
	request, err := apply.NewDecisionRequest(apply.DecisionRequestInput{
		TargetPath: "token",
		Choices:    []apply.DecisionChoice{apply.ChoiceOverwrite, apply.ChoiceSkip, apply.ChoiceAbort},
	})
	if err != nil {
		panic(err)
	}
	return request
}

func testPromptChoices(t *testing.T) {
	cases := []struct {
		answer string
		want   apply.DecisionChoice
	}{
		{"overwrite", apply.ChoiceOverwrite},
		{"skip", apply.ChoiceSkip},
		{"abort", apply.ChoiceAbort},
	}
	for _, scenario := range cases {
		prompt, _ := promptFixture(t, []string{scenario.answer}, nil)
		response, err := prompt.Resolve(context.Background(), driftRequest())
		if err != nil {
			t.Fatalf("%s: %v", scenario.answer, err)
		}
		if response.Choice != scenario.want {
			t.Fatalf("%s response = %v, want %v", scenario.answer, response.Choice, scenario.want)
		}
	}
}

func testPromptInvalid(t *testing.T) {
	prompt, stderr := promptFixture(t, []string{"maybe", "overwrite"}, nil)
	response, err := prompt.Resolve(context.Background(), driftRequest())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if response.Choice != apply.ChoiceOverwrite {
		t.Fatalf("response = %v, want overwrite after reprompt", response.Choice)
	}
	if !strings.Contains(stderr.String(), "I did not understand") {
		t.Fatalf("stderr = %q, want the invalid answer notice", stderr.String())
	}
}

func testPromptSecret(t *testing.T) {
	prompt, _ := promptFixture(t, []string{"diff", "skip"}, nil)
	response, err := prompt.Resolve(context.Background(), secretRequest())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if response.Choice != apply.ChoiceSkip {
		t.Fatalf("response = %v, want skip after diff is rejected", response.Choice)
	}
}

func testPromptDiff(t *testing.T) {
	provider := func(ctx context.Context, target string) (apply.SafeDifference, bool) {
		return apply.SafeDifference{Lines: []string{"-old", "+new"}}, true
	}
	prompt, stderr := promptFixture(t, []string{"r"}, provider)
	response, err := prompt.Resolve(context.Background(), driftRequest())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if response.Choice != apply.ChoiceOverwrite {
		t.Fatalf("response = %v, want overwrite after the diff", response.Choice)
	}
	if !strings.Contains(stderr.String(), "-old") || !strings.Contains(stderr.String(), "+new") {
		t.Fatalf("stderr = %q, want the safe difference displayed", stderr.String())
	}
}

func testPromptNonTTY(t *testing.T) {
	prompt := NewDecisionPrompt(PromptInput{
		Stdin:      strings.NewReader("overwrite\n"),
		Stderr:     &bytes.Buffer{},
		IsTerminal: func(fd int) bool { return false },
	})
	_, err := prompt.Resolve(context.Background(), driftRequest())
	if err == nil || !kindIs(err, failure.Difference) {
		t.Fatalf("non-terminal error = %v, want an unresolved difference", err)
	}
}

func testPromptEOF(t *testing.T) {
	prompt, _ := promptFixture(t, nil, nil)
	_, err := prompt.Resolve(context.Background(), driftRequest())
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("eof error = %v, want an invalid input failure", err)
	}
}

func testPromptDefault(t *testing.T) {
	prompt, _ := promptFixture(t, []string{""}, nil)
	_, err := prompt.Resolve(context.Background(), driftRequest())
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("blank input must not choose a destructive default: %v", err)
	}
}

func testPromptConfirmation(t *testing.T) {
	prompt, stderr := promptFixture(t, []string{"y"}, nil)
	confirmed, err := prompt.Confirm(context.Background(), []apply.Resolution{{Request: driftRequest(), Choice: apply.ChoiceSkip}})
	if err != nil || !confirmed {
		t.Fatalf("confirmation = %t, %v", confirmed, err)
	}
	if !strings.Contains(stderr.String(), "Skip     ~/a.conf") {
		t.Fatalf("summary = %q", stderr.String())
	}
}

func testPromptWriterError(t *testing.T) {
	prompt := NewDecisionPrompt(PromptInput{
		Stdin:      strings.NewReader("overwrite\n"),
		Stderr:     failingWriter{},
		IsTerminal: func(fd int) bool { return true },
	})
	_, err := prompt.Resolve(context.Background(), driftRequest())
	if err == nil {
		t.Fatal("a writer failure must surface")
	}
}

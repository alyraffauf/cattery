package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/failure"
)

// PromptInput carries the streams, terminal predicate, and optional safe
// difference provider of one decision prompt.
type PromptInput struct {
	Stdin      io.Reader
	Stderr     io.Writer
	IsTerminal func(fd int) bool
	Diff       func(context.Context, string) (apply.SafeDifference, bool)
}

// DecisionPrompt is the interactive resolver of one decision request: it
// renders the allowed choices, reads one answer, and maps it to a response,
// re-prompting on invalid input (PLAN.md Section 11.5). It imports only the
// apply DTOs and failure categories; no Cobra command or backend adapter.
type DecisionPrompt struct {
	stdin      io.Reader
	stderr     io.Writer
	isTerminal func(fd int) bool
	diff       func(context.Context, string) (apply.SafeDifference, bool)
}

// NewDecisionPrompt builds the interactive resolver over the given input.
func NewDecisionPrompt(input PromptInput) *DecisionPrompt {
	return &DecisionPrompt{
		stdin:      input.Stdin,
		stderr:     input.Stderr,
		isTerminal: input.IsTerminal,
		diff:       input.Diff,
	}
}

// Resolve asks one decision request until a valid final answer arrives.
func (p *DecisionPrompt) Resolve(ctx context.Context, request apply.DecisionRequest) (apply.DecisionResponse, error) {
	if p.isTerminal == nil || !p.isTerminal(0) {
		return apply.DecisionResponse{}, failure.New(failure.Difference, "cli: decisions require an interactive terminal", nil)
	}
	scanner := bufio.NewScanner(p.stdin)
	path := "$HOME/" + displayPath(request.TargetPath())
	for {
		if err := p.renderPrompt(path, request.Choices()); err != nil {
			return apply.DecisionResponse{}, err
		}
		answer, err := readAnswer(scanner)
		if err != nil {
			return apply.DecisionResponse{}, failure.New(failure.InvalidInput, "cli: EOF before a valid answer", err)
		}
		response, done, err := p.answer(ctx, request, answer)
		if err != nil {
			return apply.DecisionResponse{}, err
		}
		if done {
			return response, nil
		}
	}
}

// renderPrompt writes the choice line of one request to stderr.
func (p *DecisionPrompt) renderPrompt(path string, choices []apply.DecisionChoice) error {
	_, err := fmt.Fprintf(p.stderr, "%s: %s [%s] ", path, strings.Join(choiceNames(choices), " "), choices[0])
	return err
}

// choiceNames projects the allowed choices into stable lowercase words.
func choiceNames(choices []apply.DecisionChoice) []string {
	names := make([]string, 0, len(choices))
	for _, choice := range choices {
		names = append(names, string(choice))
	}
	return names
}

// readAnswer reads one trimmed answer line.
func readAnswer(scanner *bufio.Scanner) (string, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// answer maps one raw answer to a response, displaying the safe difference
// and re-prompting on diff or invalid input.
func (p *DecisionPrompt) answer(ctx context.Context, request apply.DecisionRequest, answer string) (apply.DecisionResponse, bool, error) {
	if answer == "" {
		return apply.DecisionResponse{Choice: request.Choices()[0]}, true, nil
	}
	choice := apply.DecisionChoice(answer)
	for _, allowed := range request.Choices() {
		if choice != allowed {
			continue
		}
		if choice != apply.ChoiceDiff {
			return apply.DecisionResponse{Choice: choice}, true, nil
		}
		if err := p.renderDifference(ctx, request.TargetPath()); err != nil {
			return apply.DecisionResponse{}, false, err
		}
		return apply.DecisionResponse{}, false, nil
	}
	_, err := fmt.Fprintf(p.stderr, "invalid answer %q\n", answer)
	return apply.DecisionResponse{}, false, err
}

// renderDifference displays the safe difference of one target on stderr.
func (p *DecisionPrompt) renderDifference(ctx context.Context, target string) error {
	if p.diff == nil {
		_, err := fmt.Fprintf(p.stderr, "no difference available for %s\n", target)
		return err
	}
	difference, ok := p.diff(ctx, target)
	if !ok {
		_, err := fmt.Fprintf(p.stderr, "no difference available for %s\n", target)
		return err
	}
	_, err := fmt.Fprintf(p.stderr, "--- %s ---\n%s", target, strings.Join(difference.LinesCopy(), "\n"))
	return err
}

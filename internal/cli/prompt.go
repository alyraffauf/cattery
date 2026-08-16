package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alyraffauf/cattery/internal/application/apply"
	"github.com/alyraffauf/cattery/internal/failure"
	"golang.org/x/term"
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
// re-prompting on invalid input. It imports only the
// apply DTOs and failure categories; no Cobra command or backend adapter.
type DecisionPrompt struct {
	stderr     io.Writer
	isTerminal func(fd int) bool
	diff       func(context.Context, string) (apply.SafeDifference, bool)
	scanner    *bufio.Scanner
}

// NewDecisionPrompt builds the interactive resolver over the given input,
// defaulting the terminal predicate to the x/term binding.
func NewDecisionPrompt(input PromptInput) *DecisionPrompt {
	isTerminal := input.IsTerminal
	if isTerminal == nil {
		isTerminal = term.IsTerminal
	}
	return &DecisionPrompt{
		stderr:     input.Stderr,
		isTerminal: isTerminal,
		diff:       input.Diff,
		scanner:    bufio.NewScanner(input.Stdin),
	}
}

// Resolve asks one decision request until a valid final answer arrives.
func (p *DecisionPrompt) Resolve(ctx context.Context, request apply.DecisionRequest) (apply.DecisionResponse, error) {
	return p.resolve(ctx, request, p.diff)
}

// ResolveWithDifference resolves one request with the apply candidate's safe
// difference provider. This keeps target reads bound to the frozen evaluation.
func (p *DecisionPrompt) ResolveWithDifference(ctx context.Context, request apply.DecisionRequest, difference apply.DifferenceProvider) (apply.DecisionResponse, error) {
	return p.resolve(ctx, request, difference)
}

func (p *DecisionPrompt) resolve(ctx context.Context, request apply.DecisionRequest, difference apply.DifferenceProvider) (apply.DecisionResponse, error) {
	if p.isTerminal == nil || !p.isTerminal(0) {
		return apply.DecisionResponse{}, failure.New(failure.Difference, "cli: decisions require an interactive terminal", nil)
	}
	path := "$HOME/" + displayPath(request.TargetPath())
	if err := p.renderContext(ctx, path, request, difference); err != nil {
		return apply.DecisionResponse{}, err
	}
	for {
		if err := p.renderPrompt(path, request.Choices()); err != nil {
			return apply.DecisionResponse{}, err
		}
		answer, err := readAnswer(p.scanner)
		if err != nil {
			return apply.DecisionResponse{}, failure.New(failure.InvalidInput, "cli: EOF before a valid answer", err)
		}
		response, done, err := p.answer(promptAnswer{
			context: ctx, request: request, answer: answer, difference: difference,
		})
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
	_, err := fmt.Fprintf(p.stderr, "\n  [R] Replace the local file with the repository version\n  [S] Skip this file and keep the local version\n  [A] Abort without changing anything\n\nChoice for %s: ", path)
	return err
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
type promptAnswer struct {
	context    context.Context
	request    apply.DecisionRequest
	answer     string
	difference apply.DifferenceProvider
}

func (p *DecisionPrompt) answer(input promptAnswer) (apply.DecisionResponse, bool, error) {
	if input.answer == "" {
		_, err := fmt.Fprintln(p.stderr, "Choose r, s, or a.")
		return apply.DecisionResponse{}, false, err
	}
	choice := shortChoice(input.answer)
	for _, allowed := range input.request.Choices() {
		if choice != allowed {
			continue
		}
		return apply.DecisionResponse{Choice: choice}, true, nil
	}
	_, err := fmt.Fprintf(p.stderr, "I did not understand %q. Choose r, s, or a.\n", input.answer)
	return apply.DecisionResponse{}, false, err
}

func shortChoice(answer string) apply.DecisionChoice {
	switch strings.ToLower(answer) {
	case "r", "repository", "overwrite":
		return apply.ChoiceOverwrite
	case "s", "skip":
		return apply.ChoiceSkip
	case "a", "abort":
		return apply.ChoiceAbort
	}
	return apply.DecisionChoice(answer)
}

func (p *DecisionPrompt) renderContext(ctx context.Context, path string, request apply.DecisionRequest, difference apply.DifferenceProvider) error {
	if _, err := fmt.Fprintf(p.stderr, "Conflict — %s\n\nCattery cannot safely determine whether the local version should be replaced.\n", path); err != nil {
		return err
	}
	switch request.Kind() {
	case "secret":
		_, err := fmt.Fprintln(p.stderr, "Encrypted secret content differs; its plaintext is not shown.")
		return err
	case "alias":
		if request.ExpectedLink() != "" {
			if _, err := fmt.Fprintf(p.stderr, "Expected link: %s\n", request.ExpectedLink()); err != nil {
				return err
			}
		}
		if request.CurrentLink() != "" {
			_, err := fmt.Fprintf(p.stderr, "Current link: %s\n", request.CurrentLink())
			return err
		}
		return nil
	default:
		return p.renderDifference(ctx, request.TargetPath(), difference)
	}
}

// Confirm presents the final review for an interactive resolution session.
func (p *DecisionPrompt) Confirm(ctx context.Context, resolutions []apply.Resolution) (bool, error) {
	if p.isTerminal == nil || !p.isTerminal(0) {
		return false, failure.New(failure.Difference, "cli: confirmation requires an interactive terminal", nil)
	}
	if _, err := fmt.Fprintln(p.stderr, "Review selected changes:"); err != nil {
		return false, err
	}
	for _, resolution := range resolutions {
		if _, err := fmt.Fprintf(p.stderr, "  %-8s ~/%s\n", resolutionAction(resolution.Choice), displayPath(resolution.Request.TargetPath())); err != nil {
			return false, err
		}
	}
	if _, err := fmt.Fprint(p.stderr, "\nApply these changes? [y/N] "); err != nil {
		return false, err
	}
	answer, err := readAnswer(p.scanner)
	if err != nil {
		return false, failure.New(failure.Difference, "cli: confirmation cancelled", err)
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// renderDifference displays the safe difference of one target on stderr.
func (p *DecisionPrompt) renderDifference(ctx context.Context, target string, difference apply.DifferenceProvider) error {
	if difference == nil {
		_, err := fmt.Fprintf(p.stderr, "A safe difference is not available for %s.\n", target)
		return err
	}
	safeDifference, ok := difference(ctx, target)
	if !ok {
		_, err := fmt.Fprintf(p.stderr, "A safe difference is not available for %s.\n", target)
		return err
	}
	_, err := fmt.Fprintf(p.stderr, "--- %s ---\n%s", target, strings.Join(safeDifference.LinesCopy(), "\n"))
	return err
}

func resolutionAction(choice apply.DecisionChoice) string {
	switch choice {
	case apply.ChoiceOverwrite:
		return "Replace"
	case apply.ChoiceSkip:
		return "Skip"
	default:
		return "Abort"
	}
}

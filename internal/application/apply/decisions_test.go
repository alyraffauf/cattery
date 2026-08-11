package apply

import (
	"context"
	"fmt"
	"testing"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
)

func TestApplyDecisionCollection(t *testing.T) {
	scenarios := []struct {
		name string
		run  func(*testing.T)
	}{
		{"abort stops the apply", testDecisionAbort},
		{"skip is collected", testDecisionSkip},
		{"overwrite is collected", testDecisionOverwrite},
		{"force chooses repository without resolver", testDecisionForce},
		{"diff provider receives the candidate", testDecisionDifferenceProvider},
		{"invalid response", testDecisionInvalid},
		{"resolver errors propagate", testDecisionResolverError},
		{"collection order", testDecisionOrder},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, scenario.run)
	}
}

func testDecisionForce(t *testing.T) {
	service, candidates := decisionFixture(t, "a.conf")
	resolver := &resolverFake{err: fmt.Errorf("resolver must not run")}
	service.resolver = resolver
	collected, err := service.collectDecisions(context.Background(), Request{Force: true}, candidates)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(resolver.requests) != 0 || collected.All()[0].response.Choice != ChoiceOverwrite {
		t.Fatalf("force must synthesize one repository choice without prompting: %+v", collected.All())
	}
}

// resolverFake returns queued responses and records the requests.
type resolverFake struct {
	responses       []DecisionResponse
	requests        []string
	err             error
	differenceCalls int
}

func (resolver *resolverFake) Resolve(ctx context.Context, request DecisionRequest) (DecisionResponse, error) {
	resolver.requests = append(resolver.requests, request.TargetPath())
	if resolver.err != nil {
		return DecisionResponse{}, resolver.err
	}
	if len(resolver.responses) == 0 {
		return DecisionResponse{}, fmt.Errorf("resolver exhausted")
	}
	response := resolver.responses[0]
	resolver.responses = resolver.responses[1:]
	return response, nil
}

func (resolver *resolverFake) ResolveWithDifference(ctx context.Context, request DecisionRequest, difference DifferenceProvider) (DecisionResponse, error) {
	resolver.differenceCalls++
	if _, ok := difference(ctx, request.TargetPath()); !ok {
		return DecisionResponse{}, fmt.Errorf("difference unavailable")
	}
	return resolver.Resolve(ctx, request)
}

// decisionFixture evaluates one drifting target per name over an empty
// state, so every target requires an explicit decision, and returns the
// service and the candidates.
func decisionFixture(t *testing.T, targets ...string) (*Service, Candidates) {
	t.Helper()
	repo := t.TempDir()
	home := t.TempDir()
	files := make([]deployment.ManagedFile, 0, len(targets))
	for _, name := range targets {
		files = append(files, ordinarySource(t, fileSpec{Repo: repo, Target: name, Relative: "files/" + name}, []byte("source "+name)))
		writeTarget(t, targetPath(home, name), []byte("target "+name))
	}
	service := evalFixture(t, evalInput{repo: repo, home: home, plan: evalPlan(t, repo, files...)})
	candidates, err := service.Evaluate(context.Background(), evalRequest())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return service, candidates
}

func testDecisionAbort(t *testing.T) {
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceAbort}}}
	service, candidates := decisionFixture(t, "a.conf")
	service.resolver = resolver
	_, err := service.CollectDecisions(context.Background(), candidates)
	if err == nil || !kindIs(err, failure.Difference) {
		t.Fatalf("abort error = %v, want a difference failure", err)
	}
	if len(resolver.requests) != 1 || resolver.requests[0] != "a.conf" {
		t.Fatalf("requests = %v, want one a.conf request", resolver.requests)
	}
}

func testDecisionSkip(t *testing.T) {
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceSkip}}}
	service, candidates := decisionFixture(t, "a.conf")
	service.resolver = resolver
	collected, err := service.CollectDecisions(context.Background(), candidates)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	decisions := collected.All()
	if len(decisions) != 1 || decisions[0].response.Choice != ChoiceSkip {
		t.Fatalf("decisions = %+v, want one skip", decisions)
	}
}

func testDecisionOverwrite(t *testing.T) {
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceOverwrite}}}
	service, candidates := decisionFixture(t, "a.conf")
	service.resolver = resolver
	collected, err := service.CollectDecisions(context.Background(), candidates)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if collected.All()[0].response.Choice != ChoiceOverwrite {
		t.Fatalf("decision = %+v, want overwrite", collected.All()[0])
	}
}

func testDecisionDifferenceProvider(t *testing.T) {
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceSkip}}}
	service, candidates := decisionFixture(t, "a.conf")
	service.resolver = resolver
	if _, err := service.CollectDecisions(context.Background(), candidates); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if resolver.differenceCalls != 1 {
		t.Fatalf("difference calls = %d, want one", resolver.differenceCalls)
	}
}

func testDecisionInvalid(t *testing.T) {
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ""}}}
	service, candidates := decisionFixture(t, "a.conf")
	service.resolver = resolver
	_, err := service.CollectDecisions(context.Background(), candidates)
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("invalid response error = %v, want an invalid input failure", err)
	}
}

func testDecisionResolverError(t *testing.T) {
	resolver := &resolverFake{err: failure.New(failure.InvalidInput, "cli: EOF before a valid answer", nil)}
	service, candidates := decisionFixture(t, "a.conf")
	service.resolver = resolver
	_, err := service.CollectDecisions(context.Background(), candidates)
	if err == nil || !kindIs(err, failure.InvalidInput) {
		t.Fatalf("resolver error = %v, want an invalid input failure", err)
	}
}

func testDecisionOrder(t *testing.T) {
	resolver := &resolverFake{responses: []DecisionResponse{{Choice: ChoiceOverwrite}, {Choice: ChoiceSkip}}}
	service, candidates := decisionFixture(t, "b.conf", "a.conf")
	service.resolver = resolver
	collected, err := service.CollectDecisions(context.Background(), candidates)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(resolver.requests) != 2 || resolver.requests[0] != "a.conf" || resolver.requests[1] != "b.conf" {
		t.Fatalf("requests = %v, want a.conf then b.conf", resolver.requests)
	}
	decisions := collected.All()
	if decisions[0].response.Choice != ChoiceOverwrite || decisions[1].response.Choice != ChoiceSkip {
		t.Fatalf("decisions = %+v, want overwrite then skip", decisions)
	}
}

package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/secretlifecycle"
	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
)

func TestSecretsCommandMapsUnionSelectorsAndFlags(t *testing.T) {
	service := &secretsServiceFake{reencryptResult: lifecycleResult("planned")}
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{
		Streams: Streams{Stdout: stdout}, WorkingDir: "/work",
		Environment: []string{"CATTERY_REPO=environment"},
	})
	options := Options{}
	command := newSecretsCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	command.SetArgs([]string{"reencrypt", "apps", "--source", "_secrets/root", "--source", "apps/_linux/_secrets/token", "--dry-run"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	request := service.reencryptRequests[0]
	if len(request.Groups) != 1 || request.Groups[0] != "apps" || len(request.Sources) != 2 || !request.DryRun || request.Yes {
		t.Fatalf("request = %+v", request)
	}
	if request.Repository.RawEnv != "environment" || !request.Repository.EnvSet {
		t.Fatalf("repository = %+v", request.Repository)
	}
}

func TestSecretsRenderingIsSafeAndPreservesDifference(t *testing.T) {
	service := &secretsServiceFake{
		reencryptResult: lifecycleResult("planned"),
		reencryptError:  failure.New(failure.Difference, "secrets reencrypt: changes pending", nil),
	}
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}})
	command := newSecretsCommand(service, runtime, &Options{})
	command.SetArgs([]string{"reencrypt"})
	err := command.Execute()
	if !kindIs(err, failure.Difference) {
		t.Fatalf("error = %v", err)
	}
	want := "Secret operation complete — 1 source\n\n  Ready    ~/.config/app/token\n           Source: apps/_secrets/token (apps, linux)\n\nSecret plaintext is never shown.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSecretsListOmitsStatusAndPlaintext(t *testing.T) {
	service := &secretsServiceFake{listResult: lifecycleResult("")}
	stdout := &bytes.Buffer{}
	command := newSecretsCommand(service, NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}}), &Options{})
	command.SetArgs([]string{"list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	want := "Encrypted sources — 1 source\n\n  Secret   ~/.config/app/token\n           Source: apps/_secrets/token (apps, linux)\n\nSecret plaintext is never shown.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

type secretsServiceFake struct {
	listRequests      []secretlifecycle.Request
	verifyRequests    []secretlifecycle.Request
	reencryptRequests []secretlifecycle.Request
	listResult        secretlifecycle.Result
	verifyResult      secretlifecycle.Result
	reencryptResult   secretlifecycle.Result
	listError         error
	verifyError       error
	reencryptError    error
}

func (service *secretsServiceFake) List(_ context.Context, request secretlifecycle.Request) (secretlifecycle.Result, error) {
	service.listRequests = append(service.listRequests, request)
	return service.listResult, service.listError
}

func (service *secretsServiceFake) Verify(_ context.Context, request secretlifecycle.Request) (secretlifecycle.Result, error) {
	service.verifyRequests = append(service.verifyRequests, request)
	return service.verifyResult, service.verifyError
}

func (service *secretsServiceFake) Reencrypt(_ context.Context, request secretlifecycle.Request) (secretlifecycle.Result, error) {
	service.reencryptRequests = append(service.reencryptRequests, request)
	return service.reencryptResult, service.reencryptError
}

func lifecycleResult(status string) secretlifecycle.Result {
	return secretlifecycle.Result{Items: []secretlifecycle.Item{{
		Source: "apps/_secrets/token", Target: ".config/app/token", Group: "apps",
		Layer: deployment.LayerLinux, Kind: deployment.FileSecret, Status: status,
	}}}
}

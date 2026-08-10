package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/alyraffauf/cattery/internal/application/forget"
)

func TestForgetCommandMapsFlagsAndRendersPlan(t *testing.T) {
	service := &forgetServiceFake{result: forget.Result{Items: []forget.Item{{Target: ".config/nvim/init.lua", Source: "nvim/.config/nvim/init.lua", Status: "planned"}}}}
	stdout := &bytes.Buffer{}
	runtime := NewRuntime(RuntimeInput{Streams: Streams{Stdout: stdout}, WorkingDir: "/work", Environment: []string{"CATTERY_REPO=environment"}})
	options := Options{}
	command := newForgetCommand(service, runtime, &options)
	bindSharedFlags(command, &options)
	command.SetArgs([]string{"--dry-run", ".config/nvim"})
	if err := command.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	request := service.requests[0]
	if request.Directory != ".config/nvim" || !request.DryRun || request.Yes {
		t.Fatalf("request = %#v", request)
	}
	if got := stdout.String(); got != "$HOME/.config/nvim/init.lua planned nvim/.config/nvim/init.lua\n" {
		t.Fatalf("stdout = %q", got)
	}
}

type forgetServiceFake struct {
	requests []forget.Request
	result   forget.Result
	err      error
}

func (fake *forgetServiceFake) Forget(ctx context.Context, request forget.Request) (forget.Result, error) {
	fake.requests = append(fake.requests, request)
	return fake.result, fake.err
}

package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/alyraffauf/cattery/internal/application/secretlifecycle"
	"github.com/spf13/cobra"
)

// SecretsService is the lifecycle role used by the secrets command family.
type SecretsService interface {
	List(context.Context, secretlifecycle.Request) (secretlifecycle.Result, error)
	Verify(context.Context, secretlifecycle.Request) (secretlifecycle.Result, error)
	Reencrypt(context.Context, secretlifecycle.Request) (secretlifecycle.Result, error)
}

func newSecretsCommand(service SecretsService, runtime Runtime, options *Options) *cobra.Command {
	command := &cobra.Command{Use: "secrets", Short: "Inspect and rotate encrypted sources"}
	command.AddCommand(
		newSecretsListCommand(service, runtime, options),
		newSecretsVerifyCommand(service, runtime, options),
		newSecretsReencryptCommand(service, runtime, options),
	)
	return command
}

func newSecretsListCommand(service SecretsService, runtime Runtime, options *Options) *cobra.Command {
	return newSecretsLeaf(secretsLeafSpec{
		use: "list [GROUP ...]", short: "List managed encrypted sources", runtime: runtime, options: options, inventory: true,
		call: func(ctx context.Context, request secretlifecycle.Request) (secretlifecycle.Result, error) {
			return service.List(ctx, request)
		},
	})
}

func newSecretsVerifyCommand(service SecretsService, runtime Runtime, options *Options) *cobra.Command {
	return newSecretsLeaf(secretsLeafSpec{
		use: "verify [GROUP ...]", short: "Verify managed encrypted sources", runtime: runtime, options: options,
		call: func(ctx context.Context, request secretlifecycle.Request) (secretlifecycle.Result, error) {
			return service.Verify(ctx, request)
		},
	})
}

func newSecretsReencryptCommand(service SecretsService, runtime Runtime, options *Options) *cobra.Command {
	return newSecretsLeaf(secretsLeafSpec{
		use: "reencrypt [GROUP ...]", short: "Re-encrypt sources with current SOPS rules", runtime: runtime, options: options, mutation: true,
		call: func(ctx context.Context, request secretlifecycle.Request) (secretlifecycle.Result, error) {
			return service.Reencrypt(ctx, request)
		},
	})
}

type secretsCall func(context.Context, secretlifecycle.Request) (secretlifecycle.Result, error)

type secretsLeafSpec struct {
	use       string
	short     string
	runtime   Runtime
	options   *Options
	call      secretsCall
	mutation  bool
	inventory bool
}

func newSecretsLeaf(spec secretsLeafSpec) *cobra.Command {
	command := &cobra.Command{
		Use: spec.use, Short: spec.short, Args: cobra.ArbitraryArgs,
		RunE: func(command *cobra.Command, groups []string) error {
			sources, _ := command.Flags().GetStringArray("source")
			dryRun, _ := command.Flags().GetBool("dry-run")
			yes, _ := command.Flags().GetBool("yes")
			result, err := spec.call(command.Context(), secretlifecycle.Request{
				Repository: secretsRepository(*spec.options, command, spec.runtime),
				Groups:     append([]string(nil), groups...), Sources: sources, DryRun: dryRun, Yes: yes,
			})
			if renderErr := renderSecrets(spec.runtime.Stdout(), result, spec.inventory); renderErr != nil {
				return renderErr
			}
			return err
		},
	}
	command.Flags().StringArray("source", nil, "select an exact repository-relative encrypted source")
	if spec.mutation {
		command.Flags().Bool("dry-run", false, "preview changes without replacing encrypted sources")
		command.Flags().Bool("yes", false, "replace encrypted sources")
	}
	return command
}

func secretsRepository(options Options, command *cobra.Command, runtime Runtime) secretlifecycle.RepositoryInput {
	options.RepositorySet = options.RepositorySet || command.Flags().Changed("repo")
	environment, environmentSet := runtime.EnvValue("CATTERY_REPO")
	return secretlifecycle.RepositoryInput{
		RawExplicit: options.Repository, ExplicitSet: options.RepositorySet,
		RawEnv: environment, EnvSet: environmentSet, WorkingDir: runtime.WorkingDir(),
	}
}

func renderSecrets(writer io.Writer, result secretlifecycle.Result, inventory bool) error {
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(writer, "No encrypted sources found.")
		return err
	}
	title := "Encrypted sources"
	if !inventory {
		title = "Secret operation complete"
	}
	if _, err := fmt.Fprintf(writer, "%s — %d %s\n", title, len(result.Items), pluralize(len(result.Items), "source")); err != nil {
		return err
	}
	for _, item := range result.Items {
		group := item.Group
		if group == "" {
			group = "root"
		}
		if inventory {
			if _, err := fmt.Fprintf(writer, "\n  Secret   ~/%s\n           Source: %s (%s, %s)\n",
				displayPath(item.Target), displayPath(item.Source), displayPath(group), item.Layer); err != nil {
				return err
			}
			continue
		}
		action := "Updated"
		if item.Status == "planned" {
			action = "Ready"
		}
		if _, err := fmt.Fprintf(writer, "\n  %-8s ~/%s\n           Source: %s (%s, %s)\n",
			action, displayPath(item.Target), displayPath(item.Source), displayPath(group), item.Layer); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "\nSecret plaintext is never shown.")
	return err
}

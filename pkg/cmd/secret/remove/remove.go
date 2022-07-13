package remove

import (
	"fmt"
	"net/http"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/config"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmd/secret/shared"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/spf13/cobra"
)

type RemoveOptions struct {
	HttpClient func() (*http.Client, error)
	IO         *iostreams.IOStreams
	Config     func() (config.Config, error)
	BaseRepo   func() (ghrepo.Interface, error)

	SecretName  string
	OrgName     string
	EnvName     string
	UserSecrets bool
	Application string
}

func NewCmdRemove(f *cmdutil.Factory, runF func(*RemoveOptions) error) *cobra.Command {
	opts := &RemoveOptions{
		IO:         f.IOStreams,
		Config:     f.Config,
		HttpClient: f.HttpClient,
	}

	cmd := &cobra.Command{
		Use:   "remove <secret-name>",
		Short: "Remove secrets",
		Long: heredoc.Doc(`
			Remove a secret on one of the following levels:
			- repository (default): available to Actions runs or Dependabot in a repository
			- environment: available to Actions runs for a deployment environment in a repository
			- organization: available to Actions runs or Dependabot within an organization
			- user: available to Codespaces for your user
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// support `-R, --repo` override
			opts.BaseRepo = f.BaseRepo

			if err := cmdutil.MutuallyExclusive("specify only one of `--org`, `--env`, or `--user`", opts.OrgName != "", opts.EnvName != "", opts.UserSecrets); err != nil {
				return err
			}

			opts.SecretName = args[0]

			if runF != nil {
				return runF(opts)
			}

			return removeRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.OrgName, "org", "o", "", "Remove a secret for an organization")
	cmd.Flags().StringVarP(&opts.EnvName, "env", "e", "", "Remove a secret for an environment")
	cmd.Flags().BoolVarP(&opts.UserSecrets, "user", "u", false, "Remove a secret for your user")
	cmdutil.StringEnumFlag(cmd, &opts.Application, "app", "a", "", []string{shared.Actions, shared.Codespaces, shared.Dependabot}, "Remove a secret for a specific application")

	return cmd
}

func removeRun(opts *RemoveOptions) error {
	c, err := opts.HttpClient()
	if err != nil {
		return fmt.Errorf("could not create http client: %w", err)
	}
	client := api.NewClientFromHTTP(c)

	orgName := opts.OrgName
	envName := opts.EnvName

	secretEntity, err := shared.GetSecretEntity(orgName, envName, opts.UserSecrets)
	if err != nil {
		return err
	}

	secretApp, err := shared.GetSecretApp(opts.Application, secretEntity)
	if err != nil {
		return err
	}

	if !shared.IsSupportedSecretEntity(secretApp, secretEntity) {
		return fmt.Errorf("%s secrets are not supported for %s", secretEntity, secretApp)
	}

	var baseRepo ghrepo.Interface
	if secretEntity == shared.Repository || secretEntity == shared.Environment {
		baseRepo, err = opts.BaseRepo()
		if err != nil {
			return fmt.Errorf("could not determine base repo: %w", err)
		}
	}

	var path string
	switch secretEntity {
	case shared.Organization:
		path = fmt.Sprintf("orgs/%s/%s/secrets/%s", orgName, secretApp, opts.SecretName)
	case shared.Environment:
		path = fmt.Sprintf("repos/%s/environments/%s/secrets/%s", ghrepo.FullName(baseRepo), envName, opts.SecretName)
	case shared.User:
		path = fmt.Sprintf("user/codespaces/secrets/%s", opts.SecretName)
	case shared.Repository:
		path = fmt.Sprintf("repos/%s/%s/secrets/%s", ghrepo.FullName(baseRepo), secretApp, opts.SecretName)
	}

	cfg, err := opts.Config()
	if err != nil {
		return err
	}

	host, err := cfg.DefaultHost()
	if err != nil {
		return err
	}

	err = client.REST(host, "DELETE", path, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to delete secret %s: %w", opts.SecretName, err)
	}

	if opts.IO.IsStdoutTTY() {
		var target string
		switch secretEntity {
		case shared.Organization:
			target = orgName
		case shared.User:
			target = "your user"
		case shared.Repository, shared.Environment:
			target = ghrepo.FullName(baseRepo)
		}

		cs := opts.IO.ColorScheme()
		if envName != "" {
			fmt.Fprintf(opts.IO.Out, "%s Removed secret %s from %s environment on %s\n", cs.SuccessIconWithColor(cs.Red), opts.SecretName, envName, target)
		} else {
			fmt.Fprintf(opts.IO.Out, "%s Removed %s secret %s from %s\n", cs.SuccessIconWithColor(cs.Red), secretApp.Title(), opts.SecretName, target)
		}
	}

	return nil
}

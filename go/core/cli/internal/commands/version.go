package commands

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/kagent-dev/kagent/go/api/client"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

func clientVersionInfo() map[string]string {
	return map[string]string{
		"kagent_version": version.Version,
		"git_commit":     version.GitCommit,
		"build_date":     version.BuildDate,
	}
}

func runVersion(out io.Writer, clientSet *client.APIClientSet) error {
	versionInfo := clientVersionInfo()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	serverVersion, err := clientSet.Version.GetVersion(ctx)
	if err != nil {
		versionInfo["backend_version"] = "unknown"
	} else {
		versionInfo["backend_version"] = serverVersion.GetKagentVersion()
	}
	return json.NewEncoder(out).Encode(versionInfo)
}

// NewVersionCmd constructs the kagent version command.
func NewVersionCmd() *cobra.Command {
	var clientOnly bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the kagent version",
		Long:  `Print the kagent version. With --client, print only the CLI version without contacting the server.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if clientOnly {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(clientVersionInfo())
			}
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			// The CLI version prints whether or not the server answers; an
			// unreachable server reports its version as "unknown".
			session, _ := connection.OpenAPI(cmd.Context(), options)
			if session == nil {
				clientSet, err := options.APIClient()
				if err != nil {
					return err
				}
				defer clientSet.Close() //nolint:errcheck
				return runVersion(cmd.OutOrStdout(), clientSet)
			}
			defer session.Close() //nolint:errcheck
			return runVersion(cmd.OutOrStdout(), session.API)
		},
	}
	cmd.Flags().BoolVar(&clientOnly, "client", false, "Print only the CLI version without contacting the server")
	return cmd
}

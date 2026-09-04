package commands

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/api/client"
	"github.com/kagent-dev/kagent/go/core/cli/internal/connection"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/spf13/cobra"
)

func runVersion(clientSet *client.ClientSet) {
	versionInfo := map[string]string{
		"kagent_version": version.Version,
		"git_commit":     version.GitCommit,
		"build_date":     version.BuildDate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	serverVersion, err := clientSet.Version.GetVersion(ctx)
	if err != nil {
		versionInfo["backend_version"] = "unknown"
	} else {
		versionInfo["backend_version"] = serverVersion.GetKagentVersion()
	}

	json.NewEncoder(os.Stdout).Encode(versionInfo) //nolint:errcheck
}

// NewVersionCmd constructs the kagent version command.
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the kagent version",
		Long:  `Print the kagent version`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := connection.OptionsFromCommand(cmd)
			if err != nil {
				return err
			}
			// The CLI version prints whether or not the server answers; an
			// unreachable server reports its version as "unknown".
			session, _ := connection.Open(cmd.Context(), options)
			if session == nil {
				clientSet := options.Client()
				defer clientSet.Close() //nolint:errcheck
				runVersion(clientSet)
				return nil
			}
			defer session.Close() //nolint:errcheck
			runVersion(session.Client)
			return nil
		},
	}
}

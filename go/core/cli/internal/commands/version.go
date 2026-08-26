package commands

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/api/client"
	"github.com/kagent-dev/kagent/go/core/internal/version"
)

func VersionCmd(clientSet *client.ClientSet) {
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
		versionInfo["backend_version"] = serverVersion.KAgentVersion
	}

	json.NewEncoder(os.Stdout).Encode(versionInfo) //nolint:errcheck
}

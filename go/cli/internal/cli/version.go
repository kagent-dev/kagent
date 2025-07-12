package cli

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/kagent-dev/kagent/go/internal/version"

	"github.com/kagent-dev/kagent/go/cli/internal/config"
	"github.com/kagent-dev/kagent/go/pkg/client"
)

func VersionCmd(cfg *config.Config) {
	versionInfo := map[string]string{
		"kagent_version": version.Version,
		"git_commit":     version.GitCommit,
		"build_date":     version.BuildDate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	client := client.New(cfg.APIURL)
	version, err := client.Version.GetVersion(ctx)
	if err != nil {
		versionInfo["backend_version"] = "unknown"
	} else {
		versionInfo["backend_version"] = version.KAgentVersion
	}

	json.NewEncoder(os.Stdout).Encode(versionInfo)
}

package cli

import (
	"context"
	"encoding/json"
	"github.com/kagent-dev/kagent/go/internal/version"
	"os"
	"time"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func VersionCmd(cfg *config.Config) {
	versionInfo := map[string]string{
		"kagent_version": version.Version,
		"git_commit":     version.GitCommit,
		"build_date":     version.BuildDate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	client := autogen_client.New(cfg.APIURL)
	version, err := client.GetVersion(ctx)
	if err != nil {
		versionInfo["backend_version"] = "unknown"
	} else {
		versionInfo["backend_version"] = version
	}

	json.NewEncoder(os.Stdout).Encode(versionInfo)
}

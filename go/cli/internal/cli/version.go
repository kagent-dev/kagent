package cli

import (
	"context"
	"encoding/json"
	"os"
	"time"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

var (
	// These variables should be set during build time using -ldflags
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

func VersionCmd(cfg *config.Config) {
	versionInfo := map[string]string{
		"kagent_version": Version,
		"git_commit":     GitCommit,
		"build_date":     BuildDate,
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

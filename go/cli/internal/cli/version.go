package cli

import (
	"fmt"
	"os"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

var (
	// These variables should be set during build time using -ldflags
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

func VersionCmd() {
	fmt.Fprintf(os.Stdout, "kagent version %s\n", Version)
	fmt.Fprintf(os.Stdout, "git commit: %s\n", GitCommit)
	fmt.Fprintf(os.Stdout, "build date: %s\n", BuildDate)

	// Get backend version
	cfg, err := config.Get()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning: could not load config")
		return
	}

	client := autogen_client.New(cfg.APIURL)
	version, err := client.GetVersion()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning: Could not fetch backend version")
	} else {
		fmt.Fprintf(os.Stdout, "backend version: %s\n", version)
	}
}

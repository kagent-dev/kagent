package cli

import (
	"github.com/abiosoft/ishell/v2"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

var (
	// These variables should be set during build time using -ldflags
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

func VersionCmd(c *ishell.Context) {
	c.Printf("kagent version %s\n", Version)
	c.Printf("git commit: %s\n", GitCommit)
	c.Printf("build date: %s\n", BuildDate)

	// Get backend version
	cfg, err := config.Get()
	if err != nil {
		c.Println("Warning: could not load config")
		return
	}

	client := autogen_client.New(cfg.APIURL)
	version, err := client.GetVersion()
	if err != nil {
		c.Println("Warning: Could not fetch backend version")
	} else {
		c.Printf("backend version: %s\n", version)
	}
}

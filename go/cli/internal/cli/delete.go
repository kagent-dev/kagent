package cli

import (
	"strconv"

	"github.com/abiosoft/ishell/v2"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func DeleteCmd(c *ishell.Context) {
	if len(c.Args) < 2 {
		c.Println("Usage: delete [resource_type] id")
		return
	}

	c.Println(c.Args)

	resourceType := c.Args[0]
	id := c.Args[1]

	cfg, err := config.Get()
	if err != nil {
		c.Printf("Failed to get config: %v\n", err)
		return
	}

	client := autogen_client.New(cfg.APIURL)

	switch resourceType {
	case "team":
		teamID, err := strconv.Atoi(id)
		if err != nil {
			c.Printf("Invalid team ID: %v\n", err)
			return
		}
		if err := client.DeleteTeam(teamID, cfg.UserID); err != nil {
			c.Printf("Error deleting team: %v\n", err)
			return
		}
	default:
		c.Println("Invalid resource type. Valid resource types are: team")
	}
}

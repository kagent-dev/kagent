package cli

import (
	"encoding/json"
	"os"

	"github.com/abiosoft/ishell/v2"
	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/cli/internal/config"
)

func CreateCmd(c *ishell.Context) {
	if len(c.Args) < 2 {
		c.Println("Usage: create [resource_type] [file]")
		return
	}

	resourceType := c.Args[0]
	fileName := c.Args[1]

	f, err := os.ReadFile(fileName)
	if err != nil {
		c.Printf("Error opening file %s: %v\n", fileName, err)
		return
	}

	cfg, err := config.Get()
	if err != nil {
		c.Printf("Failed to get config: %v\n", err)
		return
	}

	client := autogen_client.New(cfg.APIURL)

	switch resourceType {
	case "team":
		var cmp api.Component
		if err := json.Unmarshal(f, &cmp); err != nil {
			c.Printf("Error unmarshalling team: %v\n", err)
			return
		}

		if cmp.Label == "" {
			c.Println("Team label is required")
			return
		}
		existingTeam, err := client.GetTeam(cmp.Label, cfg.UserID)
		if err != nil {
			c.Printf("Error getting team: %v\n", err)
			return
		}

		var team *autogen_client.Team

		if existingTeam != nil {
			team = existingTeam
			team.Component = &cmp
			team.CreatedAt = ""
			team.UpdatedAt = ""
			// Update the existing team
			c.Printf("A team with the name %s already exists\n", cmp.Label)
			c.Println("Updating team")
		} else {
			team = &autogen_client.Team{
				Component: &cmp,
				BaseObject: autogen_client.BaseObject{
					UserID: cfg.UserID,
				},
			}
		}

		req := autogen_client.ValidationRequest{
			Component: &cmp,
		}
		// call client validate
		resp, err := client.Validate(&req)
		if err != nil {
			c.Printf("Error validating component: %v\n", err)
			return
		}

		if !resp.IsValid {
			c.Println("Component is invalid")
			for _, err := range resp.Errors {
				c.Printf("Error: %s\n", err.Error)
			}
			for _, err := range resp.Warnings {
				c.Printf("Warning: %s\n", err.Error)
			}
			return
		}
		if err := client.CreateTeam(team); err != nil {
			c.Printf("Error creating team: %v\n", err)
			return
		}
	default:
		c.Println("Invalid resource type. Valid resource types are: team")
	}

	c.Println("Successfully created resource")
}

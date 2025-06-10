package client

import (
	"fmt"
)

func (c *client) ListTools(userID string) ([]*Tool, error) {
	var tools []*Tool
	err := c.doRequest("GET", fmt.Sprintf("/tools/?user_id=%s", userID), nil, &tools)
	return tools, err
}

func (c *client) GetTool(provider string, userID string) (*Tool, error) {
	allTools, err := c.ListTools(userID)
	if err != nil {
		return nil, err
	}

	for _, tool := range allTools {
		if tool.Component.Provider == provider {
			return tool, nil
		}
	}

	return nil, nil
}

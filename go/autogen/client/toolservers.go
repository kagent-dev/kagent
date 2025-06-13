package client

import (
	"context"
	"fmt"
)

func (c *client) CreateToolServer(toolServer *ToolServer, userID string) (*ToolServer, error) {
	var server ToolServer
	err := c.doRequest(
		context.Background(),
		"POST",
		fmt.Sprintf("/toolservers/?user_id=%s", userID),
		toolServer,
		&server,
	)
	return &server, err
}

func (c *client) ListToolServers(userID string) ([]*ToolServer, error) {
	var toolServers []*ToolServer
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/toolservers/?user_id=%s", userID), nil, &toolServers)
	return toolServers, err
}

func (c *client) GetToolServer(serverID int, userID string) (*ToolServer, error) {
	var toolServer *ToolServer
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/toolservers/%d?user_id=%s", serverID, userID), nil, &toolServer)
	return toolServer, err
}

func (c *client) GetToolServerByLabel(toolServerLabel, userID string) (*ToolServer, error) {
	allServers, err := c.ListToolServers(userID)
	if err != nil {
		return nil, err
	}

	for _, server := range allServers {
		if server.Component.Label == toolServerLabel {
			return server, nil
		}
	}

	return nil, fmt.Errorf("tool server with label %s not found", toolServerLabel)
}

func (c *client) DeleteToolServer(serverID *int, userID string) error {
	return c.doRequest(context.Background(), "DELETE", fmt.Sprintf("/toolservers/%d?user_id=%s", *serverID, userID), nil, nil)
}

func (c *client) RefreshTools(serverID *int, userID string) error {
	return c.doRequest(context.Background(), "POST", fmt.Sprintf("/toolservers/%d/refresh?user_id=%s", *serverID, userID), nil, nil)
}

func (c *client) ListToolsForServer(serverID *int, userID string) ([]*Tool, error) {
	var tools []*Tool
	err := c.doRequest(context.Background(), "GET", fmt.Sprintf("/toolservers/%d/tools?user_id=%s", *serverID, userID), nil, &tools)
	return tools, err
}

// RefreshToolServer refreshes tools for a specific server
func (c *client) RefreshToolServer(serverID int, userID string) error {
	return c.doRequest(
		context.Background(),
		"POST",
		fmt.Sprintf("/toolservers/%d/refresh?user_id=%s", serverID, userID),
		nil,
		nil,
	)
}

// CreateToolServer creates a new server
func (c *client) UpdateToolServer(server *ToolServer, userID string) error {
	return c.doRequest(context.Background(), "PUT", fmt.Sprintf(
		"/toolservers/%v?user_id=%s",
		server.Id,
		userID,
	), server, server)
}

package client

import (
	"fmt"
)

// ListToolServers fetches all tool servers
func (c *Client) ListToolServers() ([]*ToolServer, error) {
	var servers []*ToolServer
	err := c.doRequest(
		"GET",
		"/toolservers",
		nil,
		&servers,
	)
	return servers, err
}

func (c *Client) GetToolServer(toolServerLabel string) (*ToolServer, error) {
	allServers, err := c.ListToolServers()
	if err != nil {
		return nil, err
	}

	for _, server := range allServers {
		if server.Component != nil &&
			server.Component.Label != nil && *server.Component.Label == toolServerLabel {
			return server, nil
		}
	}

	return nil, fmt.Errorf("tool server with label %s not found", toolServerLabel)
}

// RefreshToolServer refreshes tools for a specific server
func (c *Client) RefreshToolServer(serverID int) error {
	return c.doRequest(
		"POST",
		fmt.Sprintf("/toolservers/%d/refresh", serverID),
		nil,
		nil,
	)
}

// DeleteToolServer deletes a server
func (c *Client) DeleteToolServer(serverID int) error {
	return c.doRequest(
		"DELETE",
		fmt.Sprintf("/toolservers/%d", serverID),
		nil,
		nil,
	)
}

// CreateToolServer creates a new server
func (c *Client) CreateToolServer(server *ToolServerConfig) error {
	return c.doRequest(
		"POST",
		"/toolservers",
		server,
		server,
	)
}

// CreateToolServer creates a new server
func (c *Client) UpdateToolServer(server *ToolServerConfig) error {
	return c.doRequest("PUT", fmt.Sprintf(
		"/toolservers/%v",
		server.Id,
	), server, server)
}

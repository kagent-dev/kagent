package client

import (
	"fmt"
)

// ListToolServers fetches all tool servers
func (c *Client) ListToolServers(userID string) ([]*ToolServer, error) {
	var servers []*ToolServer
	err := c.doRequest(
		"GET",
		"/toolservers?user_id="+userID,
		nil,
		&servers,
	)
	return servers, err
}

func (c *Client) GetToolServer(toolServerLabel, userID string) (*ToolServer, error) {
	allServers, err := c.ListToolServers(userID)
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
func (c *Client) RefreshToolServer(serverID int, userID string) error {
	return c.doRequest(
		"POST",
		fmt.Sprintf("/toolservers/%d/refresh?user_id=%s", serverID, userID),
		nil,
		nil,
	)
}

// DeleteToolServer deletes a server
func (c *Client) DeleteToolServer(serverID int, userID string) error {
	return c.doRequest(
		"DELETE",
		fmt.Sprintf("/toolservers/%d?user_id=%s", serverID, userID),
		nil,
		nil,
	)
}

// CreateToolServer creates a new server
func (c *Client) CreateToolServer(server *ToolServerConfig, userID string) error {
	return c.doRequest(
		"POST",
		"/toolservers?user_id="+userID,
		server,
		server,
	)
}

// CreateToolServer creates a new server
func (c *Client) UpdateToolServer(server *ToolServerConfig, userID string) error {
	return c.doRequest("PUT", fmt.Sprintf(
		"/toolservers/%v?user_id=%s",
		server.Id,
		userID,
	), server, server)
}

package client

import (
	"fmt"
)

func (c *Client) CreateToolServer(toolServer *ToolServer) (ToolServer, error) {
	var server ToolServer
	err := c.doRequest("POST", "/toolservers/", toolServer, &server)
	return server, err
}

func (c *Client) ListToolServers(userID string) ([]*ToolServer, error) {
	var toolServers []*ToolServer
	err := c.doRequest("GET", fmt.Sprintf("/toolservers/?user_id=%s", userID), nil, &toolServers)
	return toolServers, err
}

func (c *Client) GetToolServer(serverID int, userID string) (*ToolServer, error) {
	var toolServer *ToolServer
	err := c.doRequest("GET", fmt.Sprintf("/toolservers/%d?user_id=%s", serverID, userID), nil, &toolServer)
	return toolServer, err
}

func (c *Client) DeleteToolServer(serverID *int, userID string) error {
	return c.doRequest("DELETE", fmt.Sprintf("/toolservers/%d?user_id=%s", *serverID, userID), nil, nil)
}

func (c *Client) RefreshTools(serverID *int, userID string) error {
	return c.doRequest("POST", fmt.Sprintf("/toolservers/%d/refresh?user_id=%s", *serverID, userID), nil, nil)
}

func (c *Client) ListToolsForServer(serverID *int, userID string) ([]*Tool, error) {
	var tools []*Tool
	err := c.doRequest("GET", fmt.Sprintf("/toolservers/%d/tools?user_id=%s", *serverID, userID), nil, &tools)
	return tools, err
}

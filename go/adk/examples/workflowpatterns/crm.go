package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// crmURLEnv names the environment variable the mcp_tool YAML reads its
// server URL from.
const crmURLEnv = "PATTERNS_CRM_URL"

var crmOnce = sync.OnceValue(func() string {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "crm", Version: "v1"}, nil)
	type lookupIn struct {
		CustomerID string `json:"customer_id"`
	}
	type customer struct {
		ID   string `json:"id"`
		Plan string `json:"plan"`
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "lookup_customer", Description: "Looks up a customer by id."},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in lookupIn) (*mcpsdk.CallToolResult, customer, error) {
			return nil, customer{ID: in.CustomerID, Plan: "enterprise"}, nil
		})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
	return httptest.NewServer(handler).URL
})

// crmURL starts the demo CRM MCP server on loopback once per process and
// publishes its URL to the environment for the YAML document.
func crmURL() (string, error) {
	url := crmOnce()
	return url, os.Setenv(crmURLEnv, url)
}

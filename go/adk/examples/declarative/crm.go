package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// crmURLEnv names the variable that agents.yaml reads the CRM MCP server URL
// from (mcp_servers[].url_from.env).
const crmURLEnv = "CRM_MCP_URL"

type lookupArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"customer id such as c-42"`
}

type customer struct {
	ID      string `json:"id"`
	Plan    string `json:"plan"`
	Balance int    `json:"balance"`
}

var customerID = regexp.MustCompile(`^c-[0-9]+$`)

// newCRMServer is the demo remote MCP server: a CRM that exposes
// lookup_customer over streamable HTTP. In a cluster this is any MCP server
// the document's mcp_servers entry points at.
func newCRMServer() http.Handler {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "demo-crm", Version: "v1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "lookup_customer", Description: "Looks up a customer by id."},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in lookupArgs) (*mcpsdk.CallToolResult, customer, error) {
			if !customerID.MatchString(in.CustomerID) {
				return nil, customer{}, fmt.Errorf("unknown customer %q", in.CustomerID)
			}
			return nil, customer{ID: in.CustomerID, Plan: "pro", Balance: 120}, nil
		})
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil)
}

// serveDemoCRM serves the demo CRM on a loopback port and returns its URL.
func serveDemoCRM() (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("failed to listen for the demo CRM: %w", err)
	}
	srv := &http.Server{Handler: newCRMServer(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String(), stop, nil
}

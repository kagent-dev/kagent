package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// urlElicitationError ends a tool call on a URL elicitation without
// answering the server, so the URL stays valid while the person opens it.
type urlElicitationError struct {
	request apia2a.URLElicitationRequest
}

func (e *urlElicitationError) Error() string {
	return fmt.Sprintf("MCP server %s asked for a URL elicitation", e.request.Server)
}

// newElicitingClient returns the MCP client of one server, which declares
// form and URL elicitation. A form is declined: what the person typed into it
// would reach the model's context.
//
// A URL elicitation ends the tool call with a urlElicitationError, and
// urlElicitationTool asks the person instead. The MCP SDK calls this handler
// for a server on MCP 2026-07-28, which asks through the tool result. A server
// on 2025-11-25 that fails the call with -32042 is read by
// urlElicitationRequired. An elicitation/create request that a 2025-11-25
// server sends while the call runs gets the error back: the call cannot pause
// while that request is open.
func newElicitingClient(serverURL string, options *mcpsdk.ClientOptions) *mcpsdk.Client {
	if options == nil {
		options = &mcpsdk.ClientOptions{}
	}
	if options.Capabilities == nil {
		options.Capabilities = &mcpsdk.ClientCapabilities{}
	}
	options.Capabilities.Elicitation = &mcpsdk.ElicitationCapabilities{Form: &mcpsdk.FormElicitationCapabilities{}, URL: &mcpsdk.URLElicitationCapabilities{}}
	options.ElicitationHandler = elicitationHandler(serverURL)
	return mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent-adk"}, options)
}

func elicitationHandler(serverURL string) func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
	return func(_ context.Context, request *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		if request.Params.Mode != "url" || apia2a.ValidateElicitationURL(request.Params.URL) != nil {
			return &mcpsdk.ElicitResult{Action: "decline"}, nil
		}
		return nil, &urlElicitationError{request: urlElicitationRequest(serverName(request.Session, serverURL), request.Params)}
	}
}

func urlElicitationRequest(server string, params *mcpsdk.ElicitParams) apia2a.URLElicitationRequest {
	return apia2a.URLElicitationRequest{
		Type: apia2a.HITLTypeURLElicitationRequest, Server: server,
		Message: params.Message, URL: params.URL, ElicitationID: params.ElicitationID,
	}
}

// serverName names an MCP server for the person: the name it reported, or
// its host.
func serverName(session *mcpsdk.ClientSession, serverURL string) string {
	if session != nil {
		if result := session.InitializeResult(); result != nil && result.ServerInfo != nil && result.ServerInfo.Name != "" {
			return result.ServerInfo.Name
		}
	}
	if parsed, err := url.Parse(serverURL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return serverURL
}

// urlElicitationRequired reads the URL elicitation of a -32042 error, the
// answer of a server on MCP 2025-11-25.
func urlElicitationRequired(err error, serverURL string) *apia2a.URLElicitationRequest {
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != mcpsdk.CodeURLElicitationRequired || len(rpcErr.Data) == 0 {
		return nil
	}
	var data struct {
		Elicitations []*mcpsdk.ElicitParams `json:"elicitations"`
	}
	if json.Unmarshal(rpcErr.Data, &data) != nil {
		return nil
	}
	for _, elicitation := range data.Elicitations {
		if elicitation != nil && elicitation.Mode == "url" && apia2a.ValidateElicitationURL(elicitation.URL) == nil {
			request := urlElicitationRequest(serverName(nil, serverURL), elicitation)
			return &request
		}
	}
	return nil
}

// urlElicitationToolset turns a URL elicitation of its server during a tool
// call into a confirmation of that call that carries it: the run pauses until
// the person completed the interaction at the URL, then the confirmed call
// runs again.
type urlElicitationToolset struct {
	inner     tool.Toolset
	serverURL string
}

func (s *urlElicitationToolset) Name() string { return s.inner.Name() }

func (s *urlElicitationToolset) Tools(ctx adkagent.ReadonlyContext) ([]tool.Tool, error) {
	tools, err := s.inner.Tools(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := make([]tool.Tool, len(tools))
	for i, inner := range tools {
		if fn, ok := inner.(functionTool); ok {
			wrapped[i] = &urlElicitationTool{functionTool: fn, serverURL: s.serverURL}
		} else {
			wrapped[i] = inner
		}
	}
	return wrapped, nil
}

type urlElicitationTool struct {
	functionTool
	serverURL string
}

// ProcessRequest lets the inner tool declare itself, then registers the
// wrapper in its place so that ADK runs the wrapper.
func (t *urlElicitationTool) ProcessRequest(ctx adkagent.Context, req *model.LLMRequest) error {
	if err := t.functionTool.ProcessRequest(ctx, req); err != nil {
		return err
	}
	if _, ok := req.Tools[t.Name()]; ok {
		req.Tools[t.Name()] = t
	}
	return nil
}

func (t *urlElicitationTool) Run(ctx adkagent.Context, args any) (map[string]any, error) {
	result, err := t.functionTool.Run(ctx, args)
	request := t.urlElicitation(err)
	if request == nil {
		return result, err
	}
	payload, err := confirmationPayload(request)
	if err != nil {
		return nil, err
	}
	if err := ctx.RequestConfirmation(request.Message, payload); err != nil {
		return nil, err
	}
	ctx.Actions().SkipSummarization = true
	return nil, fmt.Errorf("error tool %q %w", t.Name(), tool.ErrConfirmationRequired)
}

func (t *urlElicitationTool) urlElicitation(err error) *apia2a.URLElicitationRequest {
	var elicitation *urlElicitationError
	if errors.As(err, &elicitation) {
		return &elicitation.request
	}
	return urlElicitationRequired(err, t.serverURL)
}

// confirmationPayload is the URL elicitation request as the map ADK keeps in
// a confirmation payload.
func confirmationPayload(request *apia2a.URLElicitationRequest) (map[string]any, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

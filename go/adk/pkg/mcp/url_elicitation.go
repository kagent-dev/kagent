package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// urlElicitationError ends a tool call on a URL elicitation without
// answering the server, so the URL stays valid while the person opens it.
// continuation is set when the server asked through an input_required result.
type urlElicitationError struct {
	request      apia2a.URLElicitationRequest
	continuation *urlElicitationContinuation
}

func (e *urlElicitationError) Error() string {
	return fmt.Sprintf("MCP server %s asked for a URL elicitation", e.request.Server)
}

// newElicitingClient returns the MCP client of one server, which declares
// form and URL elicitation. A form is declined: what the person typed into it
// would reach the model's context.
//
// A URL elicitation ends the tool call with a urlElicitationError, and
// urlElicitationTool asks the person instead. A server on MCP 2026-07-28 asks
// through an input_required tool result, which inputRequiredMiddleware answers
// in place of the SDK's multi round-trip middleware, so that the request state
// survives the pause. A server on 2025-11-25 that fails the call with -32042
// is read by urlElicitationRequired. An elicitation/create request that a
// 2025-11-25 server sends while the call runs gets the error back: the call
// cannot pause while that request is open.
func newElicitingClient(serverURL string, options *mcpsdk.ClientOptions) *mcpsdk.Client {
	if options == nil {
		options = &mcpsdk.ClientOptions{}
	}
	if options.Capabilities == nil {
		options.Capabilities = &mcpsdk.ClientCapabilities{}
	}
	options.Capabilities.Elicitation = &mcpsdk.ElicitationCapabilities{Form: &mcpsdk.FormElicitationCapabilities{}, URL: &mcpsdk.URLElicitationCapabilities{}}
	options.ElicitationHandler = elicitationHandler(serverURL)
	options.MultiRoundTrip = &mcpsdk.MultiRoundTripOptions{Disabled: true}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "kagent-adk"}, options)
	client.AddSendingMiddleware(inputRequiredMiddleware(serverURL))
	return client
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

// maxInputRequiredRounds bounds the input_required results one tool call
// answers before it fails, as the SDK's multi round-trip middleware does.
const maxInputRequiredRounds = 10

// urlElicitationContinuation is what a server on MCP 2026-07-28 needs back when
// a paused tool call runs again: the request state of its input_required
// result and the answers to its input requests, with the URL elicitation
// accepted. It is only used once the person confirmed the call.
type urlElicitationContinuation struct {
	RequestState   string                          `json:"request_state,omitempty"`
	InputResponses map[string]*mcpsdk.ElicitResult `json:"input_responses"`
}

type continuationKey struct{}

// apply returns call with the continuation's request state and answers.
func (c *urlElicitationContinuation) apply(call *mcpsdk.ClientRequest[mcpsdk.Params], params *mcpsdk.CallToolParams) *mcpsdk.ClientRequest[mcpsdk.Params] {
	retry := *params
	retry.RequestState = c.RequestState
	retry.InputResponses = make(mcpsdk.InputResponseMap, len(c.InputResponses))
	for id, response := range c.InputResponses {
		retry.InputResponses[id] = response
	}
	return &mcpsdk.ClientRequest[mcpsdk.Params]{Session: call.Session, Params: &retry}
}

// inputRequiredMiddleware answers the input_required results of a tool call.
// Forms are declined and the call is retried. The first safe URL elicitation
// ends the call with a urlElicitationError that carries the continuation. A
// call whose context carries a continuation sends it with its first attempt.
func inputRequiredMiddleware(serverURL string) mcpsdk.Middleware {
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, request mcpsdk.Request) (mcpsdk.Result, error) {
			call, ok := request.(*mcpsdk.ClientRequest[mcpsdk.Params])
			if !ok {
				return next(ctx, method, request)
			}
			params, ok := call.Params.(*mcpsdk.CallToolParams)
			if method != "tools/call" || !ok {
				return next(ctx, method, request)
			}
			if continuation, ok := ctx.Value(continuationKey{}).(*urlElicitationContinuation); ok {
				request = continuation.apply(call, params)
			}
			for round := 1; ; round++ {
				result, err := next(ctx, method, request)
				if err != nil {
					return result, err
				}
				toolResult, ok := result.(*mcpsdk.CallToolResult)
				if !ok || toolResult.InputRequests == nil {
					return result, nil
				}
				if round == maxInputRequiredRounds {
					return nil, fmt.Errorf("MCP tool %q still requires input after %d rounds", params.Name, round)
				}
				continuation, elicitation, err := answerInputRequests(toolResult)
				if err != nil {
					return nil, err
				}
				if elicitation != nil {
					return nil, &urlElicitationError{
						request:      urlElicitationRequest(serverName(call.Session, serverURL), elicitation),
						continuation: continuation,
					}
				}
				request = continuation.apply(call, params)
			}
		}
	}
}

// answerInputRequests answers the input requests of an input_required result.
// The first safe URL elicitation, by request ID, is accepted and returned: the
// call pauses on it. Every other elicitation is declined.
func answerInputRequests(result *mcpsdk.CallToolResult) (*urlElicitationContinuation, *mcpsdk.ElicitParams, error) {
	continuation := &urlElicitationContinuation{
		RequestState:   result.RequestState,
		InputResponses: make(map[string]*mcpsdk.ElicitResult, len(result.InputRequests)),
	}
	var elicitation *mcpsdk.ElicitParams
	for _, id := range slices.Sorted(maps.Keys(result.InputRequests)) {
		params, ok := result.InputRequests[id].(*mcpsdk.ElicitParams)
		if !ok {
			return nil, nil, fmt.Errorf("MCP server sent input request %q of unsupported type %T", id, result.InputRequests[id])
		}
		if elicitation == nil && params.Mode == "url" && apia2a.ValidateElicitationURL(params.URL) == nil {
			elicitation = params
			continuation.InputResponses[id] = &mcpsdk.ElicitResult{Action: "accept"}
			continue
		}
		continuation.InputResponses[id] = &mcpsdk.ElicitResult{Action: "decline"}
	}
	return continuation, elicitation, nil
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
	if confirmation := ctx.ToolConfirmation(); confirmation != nil {
		continuation, err := takeContinuation(ctx)
		if err != nil {
			return nil, err
		}
		if continuation != nil && confirmation.Confirmed {
			ctx = valuesContext{Context: ctx, values: context.WithValue(ctx, continuationKey{}, continuation)}
		}
	}
	result, err := t.functionTool.Run(ctx, args)
	request, continuation := t.urlElicitation(err)
	if request == nil {
		return result, err
	}
	if continuation != nil {
		if err := storeContinuation(ctx, continuation); err != nil {
			return nil, err
		}
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

// continuationStateKey is the session state key of the continuation of a
// paused call. Session state stays in the runtime, unlike the confirmation
// payload, which the A2A client receives.
func continuationStateKey(callID string) string {
	return "kagent:mcp_url_elicitation:" + callID
}

func storeContinuation(ctx adkagent.Context, continuation *urlElicitationContinuation) error {
	encoded, err := json.Marshal(continuation)
	if err != nil {
		return err
	}
	return ctx.State().Set(continuationStateKey(ctx.FunctionCallID()), string(encoded))
}

// takeContinuation returns the continuation stored for the call, if any, and
// removes it.
func takeContinuation(ctx adkagent.Context) (*urlElicitationContinuation, error) {
	key := continuationStateKey(ctx.FunctionCallID())
	value, err := ctx.State().Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	encoded, ok := value.(string)
	if !ok || encoded == "" {
		return nil, nil
	}
	if err := ctx.State().Set(key, ""); err != nil {
		return nil, err
	}
	var continuation urlElicitationContinuation
	if err := json.Unmarshal([]byte(encoded), &continuation); err != nil {
		return nil, fmt.Errorf("decode the URL elicitation continuation: %w", err)
	}
	return &continuation, nil
}

func (t *urlElicitationTool) urlElicitation(err error) (*apia2a.URLElicitationRequest, *urlElicitationContinuation) {
	var elicitation *urlElicitationError
	if errors.As(err, &elicitation) {
		return &elicitation.request, elicitation.continuation
	}
	return urlElicitationRequired(err, t.serverURL), nil
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

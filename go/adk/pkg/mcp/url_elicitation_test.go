package mcp

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// urlElicitationToolContext is the part of a tool context the URL elicitation path uses.
type urlElicitationToolContext struct {
	adkagent.Context
	ctx          context.Context
	confirmation *toolconfirmation.ToolConfirmation
	actions      session.EventActions
	state        mapState
	hint         string
	payload      any
}

type mapState map[string]any

func (s mapState) Get(key string) (any, error) {
	value, ok := s[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return value, nil
}
func (s mapState) Set(key string, value any) error { s[key] = value; return nil }
func (s mapState) All() iter.Seq2[string, any]     { return maps.All(s) }

func newToolContext(t *testing.T, state mapState, confirmation *toolconfirmation.ToolConfirmation) *urlElicitationToolContext {
	return &urlElicitationToolContext{ctx: t.Context(), state: state, confirmation: confirmation}
}

func (c *urlElicitationToolContext) State() session.State { return c.state }

func (c *urlElicitationToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return c.confirmation
}
func (c *urlElicitationToolContext) Actions() *session.EventActions { return &c.actions }
func (c *urlElicitationToolContext) FunctionCallID() string         { return "call-1" }
func (c *urlElicitationToolContext) RequestConfirmation(hint string, payload any) error {
	c.hint, c.payload = hint, payload
	return nil
}

func (c *urlElicitationToolContext) Deadline() (time.Time, bool) { return c.ctx.Deadline() }
func (c *urlElicitationToolContext) Done() <-chan struct{}       { return c.ctx.Done() }
func (c *urlElicitationToolContext) Err() error                  { return c.ctx.Err() }
func (c *urlElicitationToolContext) Value(key any) any           { return c.ctx.Value(key) }

const signInURL = "https://gateway.example/oauth/proxy/start?s=1"

// serve serves server over streamable HTTP for the test.
func serve(t *testing.T, server *mcpsdk.Server, stateless bool) string {
	t.Helper()
	httpServer := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{Stateless: stateless}))
	t.Cleanup(func() {
		// A stateful session keeps the client's stream open.
		httpServer.CloseClientConnections()
		httpServer.Close()
	})
	return httpServer.URL
}

// continuationServer serves the tool whoami on MCP 2026-07-28 from a
// stateless server that keeps no record of a sign-in: it answers only a call
// that returns the request state it issued with the sign-in accepted and its
// consent form declined. Any other call gets a new sign-in.
func continuationServer(t *testing.T, calls *atomic.Int32) string {
	t.Helper()
	var mu sync.Mutex
	issued := map[string]bool{}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "gateway", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "whoami", Description: "Names the person"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		n := calls.Add(1)
		mu.Lock()
		defer mu.Unlock()
		params := request.Params
		signIn, _ := params.InputResponses["sign_in"].(*mcpsdk.ElicitResult)
		consent, _ := params.InputResponses["consent"].(*mcpsdk.ElicitResult)
		if issued[params.RequestState] && signIn != nil && signIn.Action == "accept" && consent != nil && consent.Action == "decline" {
			delete(issued, params.RequestState)
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "jane"}}}, nil, nil
		}
		state := fmt.Sprintf("state-%d", n)
		issued[state] = true
		return &mcpsdk.CallToolResult{
			RequestState: state,
			InputRequests: mcpsdk.InputRequestMap{
				"sign_in": &mcpsdk.ElicitParams{Mode: "url", Message: "Sign in to github", URL: signInURL, ElicitationID: state},
				"consent": &mcpsdk.ElicitParams{Mode: "form", Message: "Your token?", RequestedSchema: map[string]any{"type": "object"}},
			},
		}, nil, nil
	})
	return serve(t, server, true)
}

// twoSignInServer serves the tool whoami on MCP 2026-07-28 behind two
// sign-ins, and asks again for each one a retry leaves unanswered. It records
// the input responses of every call.
func twoSignInServer(t *testing.T, received *[]mcpsdk.InputResponseMap) string {
	t.Helper()
	var mu sync.Mutex
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "gateway", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "whoami", Description: "Names the person"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		mu.Lock()
		defer mu.Unlock()
		*received = append(*received, request.Params.InputResponses)
		open := mcpsdk.InputRequestMap{}
		for id, link := range map[string]string{"github": signInURL, "jira": "https://gateway.example/oauth/proxy/start?s=2"} {
			if response, ok := request.Params.InputResponses[id].(*mcpsdk.ElicitResult); !ok || response.Action != "accept" {
				open[id] = &mcpsdk.ElicitParams{Mode: "url", Message: "Sign in to " + id, URL: link, ElicitationID: id}
			}
		}
		if len(open) == 0 {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "jane"}}}, nil, nil
		}
		return &mcpsdk.CallToolResult{RequestState: "state", InputRequests: open}, nil, nil
	})
	return serve(t, server, true)
}

// legacyServer serves the tool whoami on MCP 2025-11-25, which fails the call
// with -32042 until the person signed in.
func legacyServer(t *testing.T, signedIn *atomic.Bool, calls *atomic.Int32) string {
	t.Helper()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "gateway", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "whoami", Description: "Names the person"}, func(_ context.Context, request *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
		calls.Add(1)
		require.Less(t, request.ProtocolVersion(), "2026-07-28")
		if signedIn.Load() {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "jane"}}}, nil, nil
		}
		return nil, nil, mcpsdk.URLElicitationRequiredError([]*mcpsdk.ElicitParams{{Mode: "url", Message: "Sign in to github", URL: signInURL, ElicitationID: "e1"}})
	})
	return serve(t, server, false)
}

func whoamiTool(t *testing.T, serverURL string, ctx adkagent.ReadonlyContext) *urlElicitationTool {
	t.Helper()
	toolset, err := mcptoolset.New(mcptoolset.Config{
		Transport: &mcpsdk.StreamableClientTransport{Endpoint: serverURL},
		Client:    newElicitingClient(serverURL, nil),
	})
	require.NoError(t, err)
	wrapped := &urlElicitationToolset{inner: toolset, serverURL: serverURL}
	tools, err := wrapped.Tools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	whoami, ok := tools[0].(*urlElicitationTool)
	require.True(t, ok, "tool = %T", tools[0])
	return whoami
}

func requireSignInPause(t *testing.T, ctx *urlElicitationToolContext, err error) {
	t.Helper()
	require.ErrorIs(t, err, tool.ErrConfirmationRequired)
	payload, ok := ctx.payload.(map[string]any)
	require.True(t, ok, "payload = %#v", ctx.payload)
	require.Equal(t, apia2a.HITLTypeURLElicitationRequest, payload["type"])
	require.Equal(t, signInURL, payload["url"])
	require.Equal(t, "Sign in to github", payload["message"])
	require.NotEmpty(t, payload["server"])
	require.Equal(t, "Sign in to github", ctx.hint)
	require.True(t, ctx.actions.SkipSummarization)
}

func TestURLElicitationTool(t *testing.T) {
	t.Run("Given a stateless server on MCP 2026-07-28, when the person confirms the paused call, assert that the call returns the server's request state and completes", func(t *testing.T) {
		var calls atomic.Int32
		serverURL := continuationServer(t, &calls)
		state := mapState{}
		whoami := whoamiTool(t, serverURL, newToolContext(t, state, nil))

		paused := newToolContext(t, state, nil)
		_, err := whoami.Run(paused, map[string]any{})
		requireSignInPause(t, paused, err)
		require.Equal(t, int32(1), calls.Load(), "the server is not answered before the person opens the URL")
		payload := paused.payload.(map[string]any)
		require.Equal(t, "state-1", payload["elicitation_id"])
		require.Equal(t, "gateway", payload["server"], "the server's reported name")
		require.NotContains(t, fmt.Sprint(payload), "request_state", "the request state stays in the runtime")
		require.Contains(t, fmt.Sprint(state), "state-1")

		result, err := whoami.Run(newToolContext(t, state, &toolconfirmation.ToolConfirmation{Confirmed: true}), map[string]any{})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"output": "jane"}, result)
		require.Equal(t, int32(2), calls.Load(), "the confirmed call runs once")
		for key, value := range state {
			require.Empty(t, value, "the continuation %s is removed once used", key)
		}
	})

	t.Run("Given a stored continuation the server no longer accepts, when the person confirms the call, assert that it fails instead of pausing again", func(t *testing.T) {
		var calls atomic.Int32
		serverURL := continuationServer(t, &calls)
		state := mapState{}
		whoami := whoamiTool(t, serverURL, newToolContext(t, state, nil))

		_, err := whoami.Run(newToolContext(t, state, nil), map[string]any{})
		require.ErrorIs(t, err, tool.ErrConfirmationRequired)
		for key := range state {
			state[key] = `{"request_state":"forged","input_responses":{"sign_in":{"action":"accept"},"consent":{"action":"decline"}}}`
		}

		again := newToolContext(t, state, &toolconfirmation.ToolConfirmation{Confirmed: true})
		_, err = whoami.Run(again, map[string]any{})
		require.ErrorContains(t, err, "cannot pause again")
		require.NotErrorIs(t, err, tool.ErrConfirmationRequired)
		require.Nil(t, again.payload, "no confirmation is requested")
		require.Equal(t, int32(2), calls.Load())
		for key, value := range state {
			require.Empty(t, value, "the continuation %s is removed", key)
		}
	})

	t.Run("Given a server that asks for two sign-ins at once, when the person confirms the first, assert that the retry leaves the second unanswered and the call fails instead of pausing again", func(t *testing.T) {
		var received []mcpsdk.InputResponseMap
		serverURL := twoSignInServer(t, &received)
		state := mapState{}
		whoami := whoamiTool(t, serverURL, newToolContext(t, state, nil))

		paused := newToolContext(t, state, nil)
		_, err := whoami.Run(paused, map[string]any{})
		require.ErrorIs(t, err, tool.ErrConfirmationRequired)
		require.Equal(t, "github", paused.payload.(map[string]any)["elicitation_id"])

		_, err = whoami.Run(newToolContext(t, state, &toolconfirmation.ToolConfirmation{Confirmed: true}), map[string]any{})
		require.ErrorContains(t, err, "cannot pause again")
		require.Len(t, received, 2)
		retry := received[1]
		require.Len(t, retry, 1, "only the sign-in the person confirmed is answered")
		github, ok := retry["github"].(*mcpsdk.ElicitResult)
		require.True(t, ok, "retry = %#v", retry)
		require.Equal(t, "accept", github.Action)
	})

	t.Run("Given a paused call, when the person rejects it, assert that the server is not called and the continuation is removed", func(t *testing.T) {
		var calls atomic.Int32
		serverURL := continuationServer(t, &calls)
		state := mapState{}
		whoami := whoamiTool(t, serverURL, newToolContext(t, state, nil))

		_, err := whoami.Run(newToolContext(t, state, nil), map[string]any{})
		require.ErrorIs(t, err, tool.ErrConfirmationRequired)

		_, err = whoami.Run(newToolContext(t, state, &toolconfirmation.ToolConfirmation{Confirmed: false}), map[string]any{})
		require.ErrorIs(t, err, tool.ErrConfirmationRejected)
		require.Equal(t, int32(1), calls.Load())
		for key, value := range state {
			require.Empty(t, value, "the continuation %s is removed", key)
		}
	})

	t.Run("Given a server on MCP 2025-11-25 that fails with -32042, when the person confirms after signing in, assert that the call runs again", func(t *testing.T) {
		var signedIn atomic.Bool
		var calls atomic.Int32
		serverURL := legacyServer(t, &signedIn, &calls)
		state := mapState{}
		whoami := whoamiTool(t, serverURL, newToolContext(t, state, nil))

		paused := newToolContext(t, state, nil)
		_, err := whoami.Run(paused, map[string]any{})
		requireSignInPause(t, paused, err)
		require.Equal(t, int32(1), calls.Load())
		require.Empty(t, state, "-32042 carries no request state")

		signedIn.Store(true)
		result, err := whoami.Run(newToolContext(t, state, &toolconfirmation.ToolConfirmation{Confirmed: true}), map[string]any{})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"output": "jane"}, result)
		require.Equal(t, int32(2), calls.Load())
	})
}

func TestAnswerInputRequests(t *testing.T) {
	t.Run("Given URL, unsafe URL and form requests, when a result asks for them, assert that the first safe URL is accepted and the other safe URL gets no response", func(t *testing.T) {
		continuation, elicitation, err := answerInputRequests(&mcpsdk.CallToolResult{
			RequestState: "s",
			InputRequests: mcpsdk.InputRequestMap{
				"a": &mcpsdk.ElicitParams{Mode: "url", Message: "Unsafe", URL: "javascript:alert(1)"},
				"b": &mcpsdk.ElicitParams{Mode: "url", Message: "First", URL: "https://gateway.example/b"},
				"c": &mcpsdk.ElicitParams{Mode: "url", Message: "Second", URL: "https://gateway.example/c"},
				"d": &mcpsdk.ElicitParams{Message: "Form"},
			},
		})
		require.NoError(t, err)
		require.Equal(t, "First", elicitation.Message)
		require.Equal(t, "s", continuation.RequestState)
		actions := map[string]string{}
		for id, response := range continuation.InputResponses {
			actions[id] = response.Action
		}
		require.Equal(t, map[string]string{"a": "decline", "b": "accept", "d": "decline"}, actions)

		_, _, err = answerInputRequests(&mcpsdk.CallToolResult{InputRequests: mcpsdk.InputRequestMap{"other": unsupportedInputRequest{}}})
		require.True(t, err != nil && strings.Contains(err.Error(), "unsupported"), "err = %v", err)
	})
}

func TestElicitationHandlerRelaysOnlyASafeURL(t *testing.T) {
	handler := elicitationHandler("https://gateway.example/mcp")
	for name, params := range map[string]*mcpsdk.ElicitParams{
		"form":       {Mode: "form", Message: "Your token?", RequestedSchema: map[string]any{"type": "object"}},
		"javascript": {Mode: "url", Message: "Sign in", URL: "javascript:alert(1)", ElicitationID: "e1"},
		"plain http": {Mode: "url", Message: "Sign in", URL: "http://gateway.example/s", ElicitationID: "e1"},
	} {
		result, err := handler(t.Context(), &mcpsdk.ElicitRequest{Params: params})
		require.NoError(t, err, name)
		require.Equal(t, "decline", result.Action, name)
	}

	_, err := handler(t.Context(), &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{
		Mode: "url", Message: "Sign in", URL: "https://gateway.example/s", ElicitationID: "e1",
	}})
	var elicitation *urlElicitationError
	require.ErrorAs(t, err, &elicitation)
	require.Equal(t, apia2a.URLElicitationRequest{
		Type: apia2a.HITLTypeURLElicitationRequest, Server: "gateway.example",
		Message: "Sign in", URL: "https://gateway.example/s", ElicitationID: "e1",
	}, elicitation.request)
}

// unsupportedInputRequest is an input request kind other than an elicitation.
type unsupportedInputRequest struct{ mcpsdk.InputRequest }

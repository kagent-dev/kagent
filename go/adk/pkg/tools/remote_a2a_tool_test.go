package tools

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/a2asrv"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// stubToolContext is a minimal tool.Context implementation for unit tests.
// It embeds a context.Context so A2A CallContext values are propagated correctly.
type stubToolContext struct {
	context.Context
	userID string
}

func (s *stubToolContext) UserID() string                 { return s.userID }
func (s *stubToolContext) FunctionCallID() string         { return "" }
func (s *stubToolContext) Actions() *session.EventActions { return nil }
func (s *stubToolContext) SearchMemory(_ context.Context, _ string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (s *stubToolContext) ToolConfirmation() *toolconfirmation.ToolConfirmation { return nil }
func (s *stubToolContext) RequestConfirmation(_ string, _ any) error            { return nil }
func (s *stubToolContext) UserContent() *genai.Content                          { return nil }
func (s *stubToolContext) InvocationID() string                                 { return "" }
func (s *stubToolContext) AgentName() string                                    { return "" }
func (s *stubToolContext) ReadonlyState() session.ReadonlyState                 { return nil }
func (s *stubToolContext) AppName() string                                      { return "" }
func (s *stubToolContext) SessionID() string                                    { return "" }
func (s *stubToolContext) Branch() string                                       { return "" }
func (s *stubToolContext) Artifacts() agent.Artifacts                           { return nil }
func (s *stubToolContext) State() session.State                                 { return nil }

// TestBuildSendContext_AllowedHeadersAndUserIDPropagated verifies that x-user-id is always
// forwarded and that only headers listed in allowedHeaders pass through to the sub-agent.
func TestBuildSendContext_AllowedHeadersAndUserIDPropagated(t *testing.T) {
	incomingCtx, _ := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(map[string][]string{
		"authorization":     {"Bearer token123"},
		"x-internal-secret": {"secret"},
	}))
	toolCtx := &stubToolContext{Context: incomingCtx, userID: "user-1"}
	state := &remoteA2AState{allowedHeaders: []string{"Authorization"}}

	sendCtx := state.buildSendContext(toolCtx)

	callCtx, ok := a2asrv.CallContextFrom(sendCtx)
	if !ok {
		t.Fatal("expected CallContext in send context")
	}
	meta := callCtx.RequestMeta()

	if vals, ok := meta.Get("x-user-id"); !ok || len(vals) == 0 || vals[0] != "user-1" {
		t.Errorf("x-user-id: got %v, want [user-1]", vals)
	}
	if vals, ok := meta.Get("authorization"); !ok || len(vals) == 0 || vals[0] != "Bearer token123" {
		t.Errorf("authorization: got %v, want [Bearer token123]", vals)
	}
	if _, ok := meta.Get("x-internal-secret"); ok {
		t.Error("x-internal-secret should not be forwarded")
	}
}

// TestBuildSendContext_NoAllowedHeaders_OnlyUserIDForwarded verifies that when no
// allowedHeaders are configured, incoming request headers are not forwarded — only x-user-id.
func TestBuildSendContext_NoAllowedHeaders_OnlyUserIDForwarded(t *testing.T) {
	incomingCtx, _ := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(map[string][]string{
		"authorization": {"Bearer token123"},
	}))
	toolCtx := &stubToolContext{Context: incomingCtx, userID: "user-1"}
	state := &remoteA2AState{allowedHeaders: nil}

	sendCtx := state.buildSendContext(toolCtx)

	callCtx, ok := a2asrv.CallContextFrom(sendCtx)
	if !ok {
		t.Fatal("expected CallContext in send context")
	}
	meta := callCtx.RequestMeta()

	if vals, ok := meta.Get("x-user-id"); !ok || len(vals) == 0 || vals[0] != "user-1" {
		t.Errorf("x-user-id: got %v, want [user-1]", vals)
	}
	if _, ok := meta.Get("authorization"); ok {
		t.Error("authorization should not be forwarded when allowedHeaders is not configured")
	}
}

package driver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	approvalMCPPath   = "/mcp"
	ApprovalToolName  = "approve"
	approvalQueueSize = 16
)

// ApprovalBroker exposes Claude's native permission-prompt MCP tool and turns
// its synchronous calls into runtime-neutral approval requests.
type ApprovalBroker struct {
	protected map[string]struct{}
	maxBody   int64
	token     string
	requests  chan *PendingApprovalRequest
	listener  net.Listener
	server    *http.Server

	mu     sync.Mutex
	active map[string]struct{}
}

// PendingApprovalRequest retains the MCP call that Claude is synchronously
// waiting on. Exactly one decision resolves it.
type PendingApprovalRequest struct {
	request  runtime.ApprovalRequest
	input    map[string]any
	decision chan runtime.ApprovalDecision
	done     chan struct{}
}

type permissionPromptInput struct {
	ToolName  string         `json:"tool_name" jsonschema:"the tool requesting permission"`
	Input     map[string]any `json:"input" jsonschema:"the original tool input"`
	ToolUseID string         `json:"tool_use_id" jsonschema:"the unique Claude tool call ID"`
}

type permissionPromptOutput struct {
	Behavior     string         `json:"behavior"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	Message      string         `json:"message,omitempty"`
}

// NewApprovalBroker binds an authenticated MCP endpoint on loopback only.
func NewApprovalBroker(protectedServers []string, maxBodyBytes int) (*ApprovalBroker, error) {
	if len(protectedServers) == 0 || maxBodyBytes <= 0 {
		return nil, fmt.Errorf("protected servers and a positive MCP body limit are required")
	}
	protected := make(map[string]struct{}, len(protectedServers))
	for _, name := range protectedServers {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("protected Claude MCP server name is required")
		}
		protected[name] = struct{}{}
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate approval broker token: %w", err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for Claude permission MCP calls: %w", err)
	}
	broker := &ApprovalBroker{
		protected: protected, maxBody: int64(maxBodyBytes), token: hex.EncodeToString(tokenBytes),
		requests: make(chan *PendingApprovalRequest, approvalQueueSize), listener: listener,
		active: make(map[string]struct{}),
	}
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "kagent-approval", Version: "1"}, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: ApprovalToolName, Description: "Resolve a kagent human approval request for a Claude tool call",
	}, broker.handle)
	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer }, nil,
	)
	broker.server = &http.Server{
		Handler:           broker.authorizeAndLimit(mcpHandler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = broker.server.Serve(listener) }()
	return broker, nil
}

// Requests yields validated protected calls in arrival order.
func (b *ApprovalBroker) Requests() <-chan *PendingApprovalRequest { return b.requests }

// URL is the loopback streamable-HTTP MCP endpoint passed only to Claude.
func (b *ApprovalBroker) URL() string {
	return "http://" + b.listener.Addr().String() + approvalMCPPath
}

// Headers returns a fresh set of credentials for the private MCP endpoint.
func (b *ApprovalBroker) Headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + b.token}
}

// SettingsJSON asks only for tools belonging to protected MCP servers. Claude
// resolves those asks through the permission-prompt tool; bypass mode handles
// every other tool without a static allowlist.
func (b *ApprovalBroker) SettingsJSON() ([]byte, error) {
	ask := make([]string, 0, len(b.protected))
	for name := range b.protected {
		ask = append(ask, "mcp__"+name+"__*")
	}
	slices.Sort(ask)
	settings := struct {
		Permissions struct {
			Ask []string `json:"ask"`
		} `json:"permissions"`
	}{}
	settings.Permissions.Ask = ask
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("encode Claude approval settings: %w", err)
	}
	return raw, nil
}

// Close stops the private listener. Pending process ownership is retained by
// the runtime.PendingTurn returned from ProcessDriver.Run.
func (b *ApprovalBroker) Close() error { return b.server.Close() }

func (b *ApprovalBroker) authorizeAndLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		want, got := "Bearer "+b.token, request.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, b.maxBody)
		next.ServeHTTP(response, request)
	})
}

func (b *ApprovalBroker) handle(ctx context.Context, _ *mcp.CallToolRequest, input *permissionPromptInput) (*mcp.CallToolResult, any, error) {
	if input == nil || input.ToolUseID == "" || !b.protects(input.ToolName) {
		return nil, nil, fmt.Errorf("invalid Claude permission request")
	}
	if input.Input == nil {
		input.Input = map[string]any{}
	}
	requestID, err := newRequestID()
	if err != nil {
		return nil, nil, err
	}
	pending := &PendingApprovalRequest{
		request: runtime.ApprovalRequest{
			ID: requestID, CallID: input.ToolUseID, Name: input.ToolName, Args: input.Input,
			Hint: "Claude requires approval before calling " + input.ToolName + ".",
		},
		input: input.Input, decision: make(chan runtime.ApprovalDecision, 1), done: make(chan struct{}),
	}
	if !b.register(input.ToolUseID) {
		return nil, nil, fmt.Errorf("duplicate Claude permission request")
	}
	defer func() {
		b.unregister(input.ToolUseID)
		close(pending.done)
	}()
	select {
	case b.requests <- pending:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	default:
		return nil, nil, fmt.Errorf("approval queue is full")
	}

	// Wait for the approval decision
	var output permissionPromptOutput
	select {
	case decision := <-pending.decision:
		if decision.Approved {
			output = permissionPromptOutput{Behavior: "allow", UpdatedInput: pending.input}
		} else {
			output = permissionPromptOutput{Behavior: "deny", Message: boundedRejectionReason(decision.RejectionReason)}
		}
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("encode Claude permission decision: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
}

func (b *ApprovalBroker) protects(toolName string) bool {
	for server := range b.protected {
		if strings.HasPrefix(toolName, "mcp__"+server+"__") && len(toolName) > len("mcp__"+server+"__") {
			return true
		}
	}
	return false
}

func (b *ApprovalBroker) register(toolUseID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.active[toolUseID]; exists {
		return false
	}
	b.active[toolUseID] = struct{}{}
	return true
}

func (b *ApprovalBroker) unregister(toolUseID string) {
	b.mu.Lock()
	delete(b.active, toolUseID)
	b.mu.Unlock()
}

func (p *PendingApprovalRequest) approvalRequest() *runtime.ApprovalRequest {
	request := p.request
	return &request
}

func (p *PendingApprovalRequest) waiting() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *PendingApprovalRequest) resolve(decision runtime.ApprovalDecision) error {
	select {
	case <-p.done:
		return fmt.Errorf("Claude permission request is no longer waiting")
	default:
	}
	select {
	case p.decision <- decision:
		return nil
	case <-p.done:
		return fmt.Errorf("Claude permission request is no longer waiting")
	}
}

func boundedRejectionReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 512 {
		return "The tool call was not approved."
	}
	return reason
}

func newRequestID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate approval request ID: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

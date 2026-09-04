package driver

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/harness/runtime"
)

const (
	approvalHookPath       = "/hooks/pre-tool-use"
	approvalQueueSize      = 16
	approvalTimeoutSeconds = 86400
)

var unattendedBuiltinTools = []string{
	"Agent", "Bash", "Edit", "Glob", "Grep", "NotebookEdit", "Read", "Task",
	"TaskCreate", "TaskGet", "TaskList", "TaskOutput", "TaskStop", "TaskUpdate",
	"TodoWrite", "WebFetch", "WebSearch", "Write",
}

// ApprovalBroker turns synchronous loopback PreToolUse requests into the
// runtime-neutral approval requests consumed by ProcessDriver.
type ApprovalBroker struct {
	protected map[string]struct{}
	maxBody   int64
	token     string
	requests  chan *PendingHookRequest
	listener  net.Listener
	server    *http.Server

	mu     sync.Mutex
	active map[string]struct{}
}

// PendingHookRequest retains the HTTP request that Claude is synchronously
// waiting on. Exactly one decision resolves it.
type PendingHookRequest struct {
	request   runtime.ApprovalRequest
	sessionID string
	decision  chan runtime.ApprovalDecision
	done      chan struct{}
}

type preToolUseInput struct {
	SessionID     string         `json:"session_id"`
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	ToolUseID     string         `json:"tool_use_id"`
}

type hookOutput struct {
	Specific hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// NewApprovalBroker binds an authenticated HTTP endpoint on loopback only.
func NewApprovalBroker(protectedServers []string, maxBodyBytes int) (*ApprovalBroker, error) {
	if len(protectedServers) == 0 || maxBodyBytes <= 0 {
		return nil, fmt.Errorf("protected servers and a positive hook body limit are required")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate approval broker token: %w", err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for Claude approval hooks: %w", err)
	}
	protected := make(map[string]struct{}, len(protectedServers))
	for _, name := range protectedServers {
		if strings.TrimSpace(name) == "" {
			_ = listener.Close()
			return nil, fmt.Errorf("protected Claude MCP server name is required")
		}
		protected[name] = struct{}{}
	}
	broker := &ApprovalBroker{
		protected: protected, maxBody: int64(maxBodyBytes), token: hex.EncodeToString(tokenBytes),
		requests: make(chan *PendingHookRequest, approvalQueueSize), listener: listener,
		active: make(map[string]struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(approvalHookPath, broker.handle)
	broker.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = broker.server.Serve(listener) }()
	return broker, nil
}

// Requests yields validated protected calls in arrival order.
func (b *ApprovalBroker) Requests() <-chan *PendingHookRequest { return b.requests }

// SettingsJSON returns the only settings loaded by approval-enabled sessions.
// Protected MCP tools are deliberately absent from permissions.allow: a valid
// hook response is therefore required to override dontAsk's fail-closed denial.
func (b *ApprovalBroker) SettingsJSON(unprotectedServers []string) ([]byte, error) {
	allow := append([]string(nil), unattendedBuiltinTools...)
	for _, name := range unprotectedServers {
		allow = append(allow, "mcp__"+name+"__*")
	}
	slices.Sort(allow)

	type hook struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Timeout int               `json:"timeout"`
		Headers map[string]string `json:"headers"`
	}
	type matcher struct {
		Matcher string `json:"matcher"`
		Hooks   []hook `json:"hooks"`
	}
	matchers := make([]matcher, 0, len(b.protected))
	for name := range b.protected {
		matchers = append(matchers, matcher{
			Matcher: "mcp__" + regexp.QuoteMeta(name) + "__.*",
			Hooks: []hook{{
				Type: "http", URL: "http://" + b.listener.Addr().String() + approvalHookPath,
				Timeout: approvalTimeoutSeconds,
				Headers: map[string]string{"Authorization": "Bearer " + b.token},
			}},
		})
	}
	slices.SortFunc(matchers, func(left, right matcher) int { return strings.Compare(left.Matcher, right.Matcher) })
	settings := struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
		Hooks map[string][]matcher `json:"hooks"`
	}{Hooks: map[string][]matcher{"PreToolUse": matchers}}
	settings.Permissions.Allow = allow
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("encode Claude approval settings: %w", err)
	}
	return raw, nil
}

// Close stops the private listener. A running ProcessDriver normally owns it
// for the Actor's lifetime; this is primarily used for failed construction.
func (b *ApprovalBroker) Close() error { return b.server.Close() }

func (b *ApprovalBroker) handle(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	want, got := "Bearer "+b.token, request.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input preToolUseInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, b.maxBody))
	if err := decoder.Decode(&input); err != nil {
		http.Error(response, "invalid hook input", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(response, "invalid trailing hook input", http.StatusBadRequest)
		return
	}
	if input.HookEventName != "PreToolUse" || input.SessionID == "" || input.ToolUseID == "" || !b.protects(input.ToolName) {
		http.Error(response, "invalid hook request", http.StatusBadRequest)
		return
	}
	if input.ToolInput == nil {
		input.ToolInput = map[string]any{}
	}
	requestID, err := newRequestID()
	if err != nil {
		http.Error(response, "cannot create approval request", http.StatusInternalServerError)
		return
	}
	pending := &PendingHookRequest{
		request: runtime.ApprovalRequest{
			ID: requestID, CallID: input.ToolUseID, Name: input.ToolName, Args: input.ToolInput,
			Hint: "Claude requires approval before calling " + input.ToolName + ".",
		},
		sessionID: input.SessionID, decision: make(chan runtime.ApprovalDecision, 1), done: make(chan struct{}),
	}
	if !b.register(input.SessionID, input.ToolUseID) {
		http.Error(response, "duplicate hook request", http.StatusConflict)
		return
	}
	defer func() {
		b.unregister(input.SessionID, input.ToolUseID)
		close(pending.done)
	}()
	select {
	case b.requests <- pending:
	case <-request.Context().Done():
		return
	default:
		http.Error(response, "approval queue is full", http.StatusServiceUnavailable)
		return
	}

	select {
	case decision := <-pending.decision:
		output := hookOutput{Specific: hookSpecificOutput{HookEventName: "PreToolUse"}}
		if decision.Approved {
			output.Specific.PermissionDecision = "allow"
		} else {
			output.Specific.PermissionDecision = "deny"
			output.Specific.PermissionDecisionReason = boundedRejectionReason(decision.RejectionReason)
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(output)
	case <-request.Context().Done():
	}
}

func (b *ApprovalBroker) protects(toolName string) bool {
	for server := range b.protected {
		if strings.HasPrefix(toolName, "mcp__"+server+"__") && len(toolName) > len("mcp__"+server+"__") {
			return true
		}
	}
	return false
}

func (b *ApprovalBroker) register(sessionID, toolUseID string) bool {
	key := sessionID + "\x00" + toolUseID
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.active[key]; exists {
		return false
	}
	b.active[key] = struct{}{}
	return true
}

func (b *ApprovalBroker) unregister(sessionID, toolUseID string) {
	b.mu.Lock()
	delete(b.active, sessionID+"\x00"+toolUseID)
	b.mu.Unlock()
}

func (p *PendingHookRequest) approvalRequest() *runtime.ApprovalRequest {
	request := p.request
	return &request
}

func (p *PendingHookRequest) waiting() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *PendingHookRequest) resolve(decision runtime.ApprovalDecision) error {
	select {
	case <-p.done:
		return fmt.Errorf("Claude approval hook is no longer waiting")
	default:
	}
	select {
	case p.decision <- decision:
		return nil
	case <-p.done:
		return fmt.Errorf("Claude approval hook is no longer waiting")
	}
}

func boundedRejectionReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "The tool call was not approved."
	}
	if len(reason) > 512 {
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

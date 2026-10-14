package driver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const ApprovalToolName = "approve"

// ApprovalBroker hosts the stateless permission fallback and durable PreToolUse
// decisions. Its address and token are regenerated when the harness restarts.
type ApprovalBroker struct {
	protected map[string]struct{}
	directory string
	maxBody   int64
	token     string
	listener  net.Listener
	server    *http.Server
	mu        sync.Mutex
}

type permissionPromptInput struct {
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
}

type permissionPromptOutput struct {
	Behavior     string         `json:"behavior"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	Message      string         `json:"message,omitempty"`
}

type hookInput struct {
	SessionID string         `json:"session_id"`
	Event     string         `json:"hook_event_name"`
	ToolID    string         `json:"tool_use_id"`
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"tool_input"`
}

type hookOutput struct {
	Continue   *bool  `json:"continue,omitempty"`
	StopReason string `json:"stopReason,omitempty"`
	Specific   struct {
		Event        string         `json:"hookEventName"`
		Decision     string         `json:"permissionDecision,omitempty"`
		Reason       string         `json:"permissionDecisionReason,omitempty"`
		UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	} `json:"hookSpecificOutput"`
}

func NewApprovalBroker(protectedServers []string, directory string, maxBodyBytes int) (*ApprovalBroker, error) {
	if directory == "" || maxBodyBytes <= 0 {
		return nil, fmt.Errorf("durable approval directory and positive body limit are required")
	}
	protected := make(map[string]struct{}, len(protectedServers))
	for _, name := range protectedServers {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("protected Claude MCP server name is required")
		}
		protected[name] = struct{}{}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for Claude hooks: %w", err)
	}
	b := &ApprovalBroker{protected: protected, directory: directory, maxBody: int64(maxBodyBytes), token: uuid.NewString(), listener: listener}
	server := mcp.NewServer(&mcp.Implementation{Name: "kagent-deferred-input", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: ApprovalToolName, Description: "Permission host for durable kagent input"}, b.handlePermission)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	mux.HandleFunc("/hook", b.handleHook)
	mux.HandleFunc("/failure", b.handleFailure)
	b.server = &http.Server{Handler: b.authorizeAndLimit(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = b.server.Serve(listener) }()
	return b, nil
}

// handlePermission honors explicit ask rules after the hook allowed the call.
// An unmatched request (including ignored parallel defer) is denied immediately.
func (b *ApprovalBroker) handlePermission(ctx context.Context, _ *mcp.CallToolRequest, input *permissionPromptInput) (*mcp.CallToolResult, any, error) {
	output, err := b.permissionDecision(input)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "failed to resolve claude permission", "error", err)
		output = permissionPromptOutput{Behavior: "deny", Message: "Durable approval could not be resolved."}
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
}

func (b *ApprovalBroker) permissionDecision(input *permissionPromptInput) (permissionPromptOutput, error) {
	output := permissionPromptOutput{Behavior: "deny", Message: "This call could not be durably deferred; retry it as a single tool call."}
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := loadDeferred(b.directory, int(b.maxBody))
	if err != nil {
		return output, err
	}
	if r == nil || r.Phase != phaseExecuting || input == nil || !b.protects(input.ToolName) || input.ToolUseID != r.Tool.ID || input.ToolName != r.Tool.Name {
		return output, nil
	}
	decision, updated, err := r.hookDecision()
	if err != nil {
		return output, err
	}
	if !equalArguments(input.Input, r.Tool.Input) && !equalArguments(input.Input, updated) {
		return output, nil
	}
	r.Phase = phaseReleased
	if err := saveDeferred(b.directory, int(b.maxBody), r); err != nil {
		return output, err
	}
	output.Behavior, output.UpdatedInput, output.Message = decision, updated, ""
	if r.Decision != nil && !r.Decision.Approved {
		output.Message = r.Decision.RejectionReason
	}
	return output, nil
}

func (b *ApprovalBroker) URL() string { return "http://" + b.listener.Addr().String() + "/mcp" }
func (b *ApprovalBroker) Headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + b.token}
}
func (b *ApprovalBroker) Close() error { return b.server.Close() }

// SettingsJSON installs only compiler-owned hooks, with permission asks as a
// fail-closed fallback if defer is unavailable or a hook cannot be reached.
func (b *ApprovalBroker) SettingsJSON() ([]byte, error) {
	type hook struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Timeout int               `json:"timeout"`
	}
	type matcher struct {
		Matcher string `json:"matcher"`
		Hooks   []hook `json:"hooks"`
	}
	var settings struct {
		Permissions struct {
			Ask []string `json:"ask"`
		} `json:"permissions"`
		Hooks struct {
			PreToolUse         []matcher `json:"PreToolUse"`
			PostToolUse        []matcher `json:"PostToolUse"`
			PostToolUseFailure []matcher `json:"PostToolUseFailure"`
		} `json:"hooks"`
	}
	settings.Permissions.Ask = []string{"AskUserQuestion"}
	for server := range b.protected {
		settings.Permissions.Ask = append(settings.Permissions.Ask, "mcp__"+server+"__*")
	}
	slices.Sort(settings.Permissions.Ask)
	// One universal hook leaves ordinary tools unchanged, but can block every
	// tool during cancellation, including tools outside the approval boundary.
	settings.Hooks.PreToolUse = []matcher{{Matcher: "*", Hooks: []hook{{Type: "http", URL: "http://" + b.listener.Addr().String() + "/hook", Headers: b.Headers(), Timeout: 10}}}}
	settings.Hooks.PostToolUseFailure = []matcher{{Matcher: "*", Hooks: []hook{{Type: "http", URL: "http://" + b.listener.Addr().String() + "/failure", Headers: b.Headers(), Timeout: 10}}}}
	settings.Hooks.PostToolUse = settings.Hooks.PostToolUseFailure

	return json.Marshal(settings)
}

func (b *ApprovalBroker) authorizeAndLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte("Bearer "+b.token), []byte(r.Header.Get("Authorization"))) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, b.maxBody)
		next.ServeHTTP(w, r)
	})
}

func (b *ApprovalBroker) protects(name string) bool {
	if name == "AskUserQuestion" {
		return true
	}
	for server := range b.protected {
		prefix := "mcp__" + server + "__"
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return true
		}
	}
	return false
}

func (b *ApprovalBroker) handleHook(w http.ResponseWriter, r *http.Request) {
	output := hookOutput{}
	output.Specific.Event, output.Specific.Decision = "PreToolUse", "deny"
	// Errors are expressed as a successful hook denial: HTTP errors are
	// nonblocking in Claude's hook protocol.
	if err := b.resolveHook(r, &output); err != nil {
		logging.FromContext(r.Context()).ErrorContext(r.Context(), "failed to resolve claude hook", "error", err)
		output.Specific.Reason = "Durable approval could not be resolved."
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(output)
}

func (b *ApprovalBroker) resolveHook(r *http.Request, output *hookOutput) error {
	var input hookInput
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("Claude hook contains trailing data")
	}
	if r.Method != http.MethodPost || input.Event != "PreToolUse" || input.SessionID == "" || input.ToolID == "" || input.ToolName == "" {
		return fmt.Errorf("invalid Claude hook")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	record, err := loadDeferred(b.directory, int(b.maxBody))
	if err != nil {
		return err
	}
	if record != nil && record.Canceling && record.Phase != phaseCanceled && record.SessionID == input.SessionID && (record.Tool.ID != input.ToolID || record.Tool.Name != input.ToolName || !equalArguments(record.Tool.Input, input.Input)) {
		stop := false
		output.Continue = &stop
		output.StopReason = "The task was canceled."
		output.Specific.Reason = output.StopReason
		return nil
	}
	if !b.protects(input.ToolName) {
		output.Specific.Decision = ""
		return nil
	}
	if record != nil && (record.Phase == phaseComplete || record.Phase == phaseCanceled) && record.SessionID == input.SessionID && record.Tool.ID == input.ToolID {
		output.Specific.Reason = "The deferred call is already resolved."
		if record.Phase == phaseCanceled {
			stop := false
			output.Continue = &stop
			output.StopReason = "The task was canceled."
		}
		return nil
	}
	if record == nil || record.Phase == phaseComplete || record.Phase == phaseCanceled {
		output.Specific.Decision = "defer"
		return nil
	}
	if record.SessionID != input.SessionID || record.Tool.ID != input.ToolID || record.Tool.Name != input.ToolName || !equalArguments(record.Tool.Input, input.Input) {
		if record.Phase == phaseExecuting || record.Phase == phaseReleased {
			output.Specific.Decision = "defer"
			return nil
		} // A subsequent input within the same turn.
		return fmt.Errorf("Claude deferred tool changed")
	}
	if record.Phase == phaseWaiting {
		output.Specific.Decision = "defer"
		return nil
	}
	if record.Phase != phaseResuming {
		return fmt.Errorf("Claude decision has already been released")
	}
	decision, updated, err := record.hookDecision()
	if err != nil {
		return err
	}
	record.Phase = phaseExecuting
	if err := saveDeferred(b.directory, int(b.maxBody), record); err != nil {
		return err
	}
	if record.Canceling {
		stop := false
		output.Continue = &stop
		output.StopReason = "The task was canceled."
	}
	output.Specific.Decision, output.Specific.UpdatedInput = decision, updated
	if record.Decision != nil && !record.Decision.Approved {
		output.Specific.Reason = record.Decision.RejectionReason
	}
	return nil
}

// handleFailure stops the native loop after either a denied or an already
// executing canceled tool settles. PreToolUse also blocks subsequent tools.
func (b *ApprovalBroker) handleFailure(w http.ResponseWriter, r *http.Request) {
	var input hookInput
	var output struct {
		Continue   *bool  `json:"continue,omitempty"`
		StopReason string `json:"stopReason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err == nil && (input.Event == "PostToolUseFailure" || input.Event == "PostToolUse") {
		b.mu.Lock()
		record, err := loadDeferred(b.directory, int(b.maxBody))
		if err == nil && record != nil && record.Canceling && record.SessionID == input.SessionID && record.Phase != phaseCanceled {
			stop := false
			output.Continue = &stop
			output.StopReason = "The task was canceled."
		}
		b.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(output)
}

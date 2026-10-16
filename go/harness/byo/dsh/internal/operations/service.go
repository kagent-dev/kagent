package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// Config is a trusted deployment binding for one conversation/security scope.
// Tokens and credentials belong in mounted files, never in model arguments.
type Config struct {
	Scope              string        `json:"scope"`
	Listen             string        `json:"listen"`
	DatabaseURLFile    string        `json:"database_url_file"`
	AgentTokenFile     string        `json:"agent_token_file"`
	ApproverTokenFile  string        `json:"approver_token_file"`
	Approver           string        `json:"approver"`
	UpstreamURL        string        `json:"upstream_url"`
	UpstreamTokenFile  string        `json:"upstream_token_file,omitempty"`
	TargetRevision     string        `json:"target_revision"`
	Tools              []string      `json:"tools"`
	ApprovalTTLSeconds int           `json:"approval_ttl_seconds"`
	CallTimeoutSeconds int           `json:"call_timeout_seconds"`
	Continuation       *Continuation `json:"continuation,omitempty"`
}

// Continuation is set by the operator, not supplied by the requesting model.
type Continuation struct {
	URL       string `json:"url"`
	ContextID string `json:"context_id"`
	TokenFile string `json:"token_file,omitempty"`
}

func (c Config) Validate() error {
	if c.Scope == "" || c.TargetRevision == "" || c.Approver == "" || len(c.Tools) == 0 {
		return errors.New("scope, target_revision, approver and tools are required")
	}
	if c.ApprovalTTLSeconds < 1 || c.ApprovalTTLSeconds > 604800 || c.CallTimeoutSeconds < 1 || c.CallTimeoutSeconds > 600 {
		return errors.New("approval TTL must be 1..604800 seconds and call timeout 1..600 seconds")
	}
	for _, name := range c.Tools {
		if strings.TrimSpace(name) == "" {
			return errors.New("tool names cannot be empty")
		}
	}
	if err := validateURL(c.UpstreamURL); err != nil {
		return err
	}
	if c.Continuation != nil {
		if c.Continuation.ContextID == "" {
			return errors.New("continuation context_id is required")
		}
		if err := validateURL(c.Continuation.URL); err != nil {
			return err
		}
	}
	return nil
}

func validateURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("endpoint must be an absolute HTTP(S) URL without userinfo or fragment")
	}
	return nil
}

func binding(c Config) string {
	tools := slices.Clone(c.Tools)
	slices.Sort(tools)
	// Include routing, identity revision and tool allowlist. A deployment change
	// must not silently redirect an already approved request to another target.
	value, _ := json.Marshal(struct {
		Scope, URL, Revision string
		Tools                []string
		Continuation         *Continuation
	}{c.Scope, c.UpstreamURL, c.TargetRevision, tools, c.Continuation})
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// Caller is implemented by the remote MCP transport. A call error means its
// effects are unknown, not proof that it is safe to retry.
type Caller interface {
	Call(context.Context, string, json.RawMessage) (*mcp.CallToolResult, error)
	ListTools(context.Context) ([]*mcp.Tool, error)
}
type Notifier interface {
	Notify(context.Context, Operation) error
}

type Service struct {
	store    *Store
	config   Config
	binding  string
	caller   Caller
	notifier Notifier
}

func New(store *Store, config Config, caller Caller, notifier Notifier) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if store == nil || caller == nil || store.scope != config.Scope {
		return nil, errors.New("store, caller and matching scope are required")
	}
	return &Service{store: store, config: config, binding: binding(config), caller: caller, notifier: notifier}, nil
}

type SubmitRequest struct {
	RequestKey string          `json:"request_key" jsonschema:"Stable business request key. Reuse it for every retry of the same action. A different key is a new action and requires a new approval."`
	Tool       string          `json:"tool" jsonschema:"Exact allowlisted upstream tool name."`
	Arguments  json.RawMessage `json:"arguments" jsonschema:"Frozen upstream tool arguments as a JSON object."`
}

func (s *Service) Submit(ctx context.Context, req SubmitRequest) (Operation, error) {
	if strings.TrimSpace(req.RequestKey) == "" || len(req.RequestKey) > 200 || len(req.Arguments) > 65536 || !slices.Contains(s.config.Tools, req.Tool) {
		return Operation{}, errors.New("invalid request key, arguments size or unconfigured tool")
	}
	var args map[string]any
	decoder := json.NewDecoder(bytes.NewReader(req.Arguments))
	decoder.UseNumber()
	if err := decoder.Decode(&args); err != nil || args == nil || !json.Valid(req.Arguments) {
		return Operation{}, errors.New("arguments must be a JSON object")
	}
	canonical, err := json.Marshal(args)
	if err != nil {
		return Operation{}, err
	}
	encoded, _ := json.Marshal(struct {
		Binding, Key, Tool string
		Arguments          json.RawMessage
	}{s.binding, req.RequestKey, req.Tool, canonical})
	sum := sha256.Sum256(encoded)
	return s.store.submit(ctx, Operation{RequestKey: req.RequestKey, RequestDigest: hex.EncodeToString(sum[:]), Binding: s.binding, Tool: req.Tool, Arguments: canonical}, time.Duration(s.config.ApprovalTTLSeconds)*time.Second)
}

func (s *Service) Get(ctx context.Context, id string) (Operation, error) { return s.store.Get(ctx, id) }

// Tick handles at most one execution and one notification. Concurrent replicas
// use DB claims. Network calls never run while holding a database transaction.
func (s *Service) Tick(ctx context.Context) error {
	if err := s.store.expire(ctx); err != nil {
		return fmt.Errorf("failed to expire operations: %w", err)
	}
	o, err := s.store.claim(ctx, s.binding)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("failed to claim operation: %w", err)
	}
	if err == nil {
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(s.config.CallTimeoutSeconds)*time.Second)
		result, callErr := s.caller.Call(callCtx, o.Tool, o.Arguments)
		cancel()
		if callErr != nil || result == nil {
			// Do not log remote errors: upstream messages may contain credentials
			// or submitted payloads. The durable state exposes the uncertainty.
			logging.FromContext(ctx).WarnContext(ctx, "operation outcome unknown", "operation_id", o.ID)
		} else {
			state := Succeeded
			if result.IsError {
				state = Failed
			}
			data, err := json.Marshal(result)
			if err != nil {
				return fmt.Errorf("failed to encode operation result: %w", err)
			}
			if err := s.store.finish(ctx, o.ID, state, data); err != nil {
				return fmt.Errorf("failed to save operation result: %w", err)
			}
		}
	}
	if s.notifier == nil {
		return nil
	}
	o, err = s.store.claimNotification(ctx, s.binding)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.notifier.Notify(notifyCtx, o); err != nil {
		if errors.Is(err, errNotAccepted) {
			return s.store.retryNotification(ctx, o.ID)
		}
		// An accepted message with a lost response must not start another task.
		logging.FromContext(ctx).WarnContext(ctx, "operation notification delivery unknown", "operation_id", o.ID)
		return nil
	}
	return s.store.notified(ctx, o.ID)
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logging.FromContext(ctx).ErrorContext(ctx, "operation worker failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// MCPCaller intentionally opens one bounded session per operation. It never
// retries tools/call and refuses redirects before credentials can be forwarded.
type MCPCaller struct {
	url    string
	client *http.Client
}

var _ Caller = (*MCPCaller)(nil)

func NewMCPCaller(endpoint, token string, timeout time.Duration) *MCPCaller {
	return &MCPCaller{url: endpoint, client: authenticatedClient(token, timeout)}
}

func (m *MCPCaller) Call(ctx context.Context, tool string, args json.RawMessage) (*mcp.CallToolResult, error) {
	session, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closeMCP(ctx, session)
	return session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
}

func (m *MCPCaller) connect(ctx context.Context) (*mcp.ClientSession, error) {
	return mcp.NewClient(&mcp.Implementation{Name: "dsh-approved-operations", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: m.url, HTTPClient: m.client, DisableStandaloneSSE: true}, nil)
}

func closeMCP(ctx context.Context, session *mcp.ClientSession) {
	if err := session.Close(); err != nil {
		logging.FromContext(ctx).WarnContext(ctx, "operation mcp session cleanup failed")
	}
}

func (m *MCPCaller) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	session, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer closeMCP(ctx, session)
	var tools []*mcp.Tool
	cursor := ""
	seen := map[string]bool{}
	for range 100 {
		page, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		tools = append(tools, page.Tools...)
		if len(tools) > 1000 {
			return nil, errors.New("upstream tool catalog exceeds 1000 tools")
		}
		if page.NextCursor == "" {
			return tools, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("upstream tool catalog repeated cursor")
		}
		cursor = page.NextCursor
		seen[cursor] = true
	}
	return nil, errors.New("upstream tool catalog exceeds 100 pages")
}

type bearerTransport struct{ token string }

var _ http.RoundTripper = bearerTransport{}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}
func authenticatedClient(token string, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: bearerTransport{token: token}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are disabled") }}
}

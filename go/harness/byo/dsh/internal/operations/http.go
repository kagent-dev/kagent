package operations

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler exposes separate agent and approver capabilities. An agent token
// cannot decide an operation, and no approval tool is registered with MCP.
func (s *Service) Handler(agentToken, approverToken string) (http.Handler, error) {
	if len(agentToken) < 32 || len(approverToken) < 32 || agentToken == approverToken || strings.ContainsAny(agentToken+approverToken, "\r\n") {
		return nil, errors.New("distinct agent and approver tokens of at least 32 bytes are required")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "dsh-approval-operations", Version: "1"}, &mcp.ServerOptions{Instructions: "Submit an operation once with a stable business request_key, then report awaiting approval and end the turn. Submission is not execution. Never invent approval or submit another key to retry the same action. Use get_operation in a later turn. outcome_unknown means verify effects with the operator; do not resubmit. Configured tools: " + strings.Join(s.config.Tools, ", ")})
	server.AddTool(&mcp.Tool{Name: "list_operation_tools", Description: "Read the names, descriptions and input schemas of permitted business tools before submitting an operation. This never executes a business tool.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Duration(s.config.CallTimeoutSeconds)*time.Second)
		defer cancel()
		catalog, err := s.caller.ListTools(ctx)
		if err != nil {
			return operationResult(Operation{}, err)
		}
		allowed := []*mcp.Tool{}
		for _, tool := range catalog {
			if slices.Contains(s.config.Tools, tool.Name) {
				allowed = append(allowed, tool)
			}
		}
		data, err := json.Marshal(allowed)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	})
	server.AddTool(&mcp.Tool{Name: "request_operation", Description: "Persist an operation for external human approval. Returns immediately; does not execute the requested tool.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"request_key": map[string]any{"type": "string"}, "tool": map[string]any{"type": "string", "enum": s.config.Tools}, "arguments": map[string]any{"type": "object"}}, "required": []string{"request_key", "tool", "arguments"}, "additionalProperties": false}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var req SubmitRequest
		if err := json.Unmarshal(r.Params.Arguments, &req); err != nil {
			return nil, err
		}
		o, err := s.Submit(ctx, req)
		return operationResult(o, err)
	})
	server.AddTool(&mcp.Tool{Name: "get_operation", Description: "Read the saved request, approval state and authoritative result. Querying never executes or approves an operation.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"operation_id": map[string]any{"type": "string"}}, "required": []string{"operation_id"}, "additionalProperties": false}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var req struct {
			ID string `json:"operation_id"`
		}
		if err := json.Unmarshal(r.Params.Arguments, &req); err != nil {
			return nil, err
		}
		o, err := s.Get(ctx, req.ID)
		return operationResult(o, err)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", requireToken(agentToken, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})))
	admin := http.NewServeMux()
	admin.HandleFunc("GET /operations", func(w http.ResponseWriter, r *http.Request) {
		o, err := s.store.list(r.Context())
		writeResponse(w, o, err)
	})
	admin.HandleFunc("GET /operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		o, err := s.Get(r.Context(), r.PathValue("id"))
		writeResponse(w, o, err)
	})
	admin.HandleFunc("POST /operations/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Digest   string `json:"request_digest"`
			Decision string `json:"decision"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, "invalid decision", http.StatusBadRequest)
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF || (req.Decision != Approved && req.Decision != Rejected) || req.Digest == "" {
			http.Error(w, "invalid decision", http.StatusBadRequest)
			return
		}
		o, err := s.store.decide(r.Context(), r.PathValue("id"), req.Digest, req.Decision, s.config.Approver)
		writeResponse(w, o, err)
	})
	mux.Handle("/operations", requireToken(approverToken, admin))
	mux.Handle("/operations/", requireToken(approverToken, admin))
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.db.PingContext(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(204)
	})
	return http.MaxBytesHandler(mux, 128<<10), nil
}

func operationResult(o Operation, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: publicError(err)}}}, nil
	}
	data, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
}

func publicError(err error) string {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return err.Error()
	}
	return "operation request failed; verify the input or consult the service operator"
}

func writeResponse(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = 404
		}
		if errors.Is(err, ErrConflict) {
			status = 409
		}
		http.Error(w, publicError(err), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// Marshal before writing headers so an encoding failure remains an error.
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "response encoding failed", 500)
		return
	}
	_, _ = w.Write(data)
}

func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

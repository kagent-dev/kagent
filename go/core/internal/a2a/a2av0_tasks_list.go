package a2a

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	a2alegacy "github.com/a2aproject/a2a-go/a2a"
	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// v0MethodTasksList is a kagent compatibility extension: the A2A 0.3 wire
// predates the task-query methods, so a2av0.NewJSONRPCHandler returns
// method-not-found for it. We intercept it here and serve it from the task
// store with the legacy lowercase TaskState encoding. tasks/get is left to the
// underlying v0 handler, which already routes it to the store-backed GetTask.
const v0MethodTasksList = "tasks/list"

// JSON-RPC 2.0 error codes mirrored from a2a-go's internal mapping.
const (
	codeTaskNotFound  = -32001
	codeInvalidParams = -32602
	codeInternalError = -32603
)

type v0RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type v0RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *v0RPCError     `json:"error,omitempty"`
}

type v0RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// v0ListTasksParams mirrors the v1 ListTasks params but carries the legacy
// lowercase TaskState string for the status filter.
type v0ListTasksParams struct {
	ContextID            string     `json:"contextId,omitempty"`
	Status               string     `json:"status,omitempty"`
	PageSize             int        `json:"pageSize,omitempty"`
	PageToken            string     `json:"pageToken,omitempty"`
	HistoryLength        *int       `json:"historyLength,omitempty"`
	StatusTimestampAfter *time.Time `json:"statusTimestampAfter,omitempty"`
	IncludeArtifacts     bool       `json:"includeArtifacts,omitempty"`
}

type v0ListTasksResult struct {
	Tasks         []*a2alegacy.Task `json:"tasks"`
	PageSize      int               `json:"pageSize"`
	TotalSize     int               `json:"totalSize"`
	NextPageToken string            `json:"nextPageToken"`
}

// v0TasksListInterceptor serves tasks/list from handler and delegates every
// other request to next unchanged.
type v0TasksListInterceptor struct {
	next    http.Handler
	handler a2asrv.RequestHandler
}

func newV0TasksListInterceptor(next http.Handler, handler a2asrv.RequestHandler) http.Handler {
	return &v0TasksListInterceptor{next: next, handler: handler}
}

func (h *v0TasksListInterceptor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.next.ServeHTTP(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.next.ServeHTTP(w, r)
		return
	}
	// Restore the body so the delegate can read it when this is not tasks/list.
	r.Body = io.NopCloser(bytes.NewReader(body))

	var rpcReq v0RPCRequest
	if err := json.Unmarshal(body, &rpcReq); err != nil || rpcReq.Method != v0MethodTasksList {
		h.next.ServeHTTP(w, r)
		return
	}

	h.serveTasksList(w, r, &rpcReq)
}

func (h *v0TasksListInterceptor) serveTasksList(w http.ResponseWriter, r *http.Request, rpcReq *v0RPCRequest) {
	w.Header().Set("Content-Type", "application/json")

	var params v0ListTasksParams
	if len(rpcReq.Params) > 0 {
		if err := json.Unmarshal(rpcReq.Params, &params); err != nil {
			writeV0Error(w, rpcReq.ID, codeInvalidParams, "invalid params")
			return
		}
	}

	listReq := &a2atype.ListTasksRequest{
		ContextID:            params.ContextID,
		PageSize:             params.PageSize,
		PageToken:            params.PageToken,
		HistoryLength:        params.HistoryLength,
		StatusTimestampAfter: params.StatusTimestampAfter,
		IncludeArtifacts:     params.IncludeArtifacts,
	}
	if params.Status != "" {
		listReq.Status = a2av0.ToV1TaskState(a2alegacy.TaskState(params.Status))
	}

	resp, err := h.handler.ListTasks(r.Context(), listReq)
	if err != nil {
		writeV0Error(w, rpcReq.ID, v0ErrorCode(err), err.Error())
		return
	}

	result := v0ListTasksResult{
		Tasks:         make([]*a2alegacy.Task, 0, len(resp.Tasks)),
		PageSize:      resp.PageSize,
		TotalSize:     resp.TotalSize,
		NextPageToken: resp.NextPageToken,
	}
	for _, t := range resp.Tasks {
		result.Tasks = append(result.Tasks, a2av0.FromV1Task(t))
	}

	writeV0Response(w, v0RPCResponse{JSONRPC: "2.0", ID: rpcReq.ID, Result: result})
}

func v0ErrorCode(err error) int {
	switch {
	case errors.Is(err, a2atype.ErrInvalidParams):
		return codeInvalidParams
	case errors.Is(err, a2atype.ErrTaskNotFound):
		return codeTaskNotFound
	default:
		return codeInternalError
	}
}

func writeV0Error(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeV0Response(w, v0RPCResponse{JSONRPC: "2.0", ID: id, Error: &v0RPCError{Code: code, Message: message}})
}

func writeV0Response(w http.ResponseWriter, resp v0RPCResponse) {
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

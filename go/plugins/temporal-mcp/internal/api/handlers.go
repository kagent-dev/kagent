package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/kagent-dev/kagent/go/plugins/temporal-mcp/internal/temporal"
)

// writeJSON encodes v as JSON with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError sends a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// WorkflowsHandler handles GET /api/workflows (list).
func WorkflowsHandler(tc temporal.WorkflowClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		filter := temporal.WorkflowFilter{
			Status:    r.URL.Query().Get("status"),
			AgentName: r.URL.Query().Get("agent"),
		}
		if ps := r.URL.Query().Get("page_size"); ps != "" {
			if n, err := strconv.Atoi(ps); err == nil && n > 0 {
				filter.PageSize = n
			}
		}

		workflows, err := tc.ListWorkflows(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": workflows})
	}
}

// WorkflowHandler handles /api/workflows/{id}, /api/workflows/{id}/cancel, /api/workflows/{id}/signal.
func WorkflowHandler(tc temporal.WorkflowClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract workflow ID and suffix from path
		path := strings.TrimPrefix(r.URL.Path, "/api/workflows/")
		if path == "" || path == r.URL.Path {
			http.NotFound(w, r)
			return
		}

		var workflowID, suffix string
		if idx := strings.Index(path, "/"); idx >= 0 {
			workflowID = path[:idx]
			suffix = path[idx:]
		} else {
			workflowID = path
		}

		switch {
		case suffix == "/cancel" && r.Method == http.MethodPost:
			if err := tc.CancelWorkflow(r.Context(), workflowID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"canceled": true})

		case suffix == "/signal" && r.Method == http.MethodPost:
			var body struct {
				SignalName string      `json:"signal_name"`
				Data       interface{} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			if body.SignalName == "" {
				writeError(w, http.StatusBadRequest, "signal_name is required")
				return
			}
			if err := tc.SignalWorkflow(r.Context(), workflowID, body.SignalName, body.Data); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"signaled": true})

		case suffix == "" && r.Method == http.MethodGet:
			detail, err := tc.GetWorkflow(r.Context(), workflowID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"data": detail})

		default:
			http.NotFound(w, r)
		}
	}
}

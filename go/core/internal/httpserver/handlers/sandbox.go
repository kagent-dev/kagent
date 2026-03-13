package handlers

import (
	"context"
	"net/http"
	"time"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/controller/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Maximum time to wait for a sandbox to become ready.
	sandboxCreateTimeout = 5 * time.Minute
)

// SandboxHandler handles sandbox lifecycle requests for sessions.
type SandboxHandler struct {
	*Base
	Provider sandbox.SandboxProvider
}

// NewSandboxHandler creates a new SandboxHandler.
func NewSandboxHandler(base *Base, provider sandbox.SandboxProvider) *SandboxHandler {
	return &SandboxHandler{Base: base, Provider: provider}
}

// HandleCreateSandbox handles POST /api/sessions/{session_id}/sandbox.
// It provisions (or returns an existing) sandbox for the given session.
func (h *SandboxHandler) HandleCreateSandbox(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sandbox-handler").WithValues("operation", "create")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session_id from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	var req api.CreateSandboxRequest
	if err := DecodeJSONBody(r, &req); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	// Verify session exists
	userID, err := getUserIDOrAgentUser(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	_, err = h.DatabaseService.GetSession(r.Context(), sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	opts := sandbox.CreateSandboxOptions{
		AgentName: req.AgentName,
		Namespace: req.Namespace,
		WorkspaceRef: sandbox.WorkspaceRef{
			APIGroup:  req.Workspace.APIGroup,
			Kind:      req.Workspace.Kind,
			Name:      req.Workspace.Name,
			Namespace: req.Workspace.Namespace,
		},
	}

	opts.SessionID = sessionID

	ctx, cancel := context.WithTimeout(r.Context(), sandboxCreateTimeout)
	defer cancel()

	ep, err := h.Provider.GetOrCreate(ctx, opts)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create sandbox", err))
		return
	}

	resp := api.SandboxResponse{
		SandboxID: ep.ID,
		MCPUrl:    ep.MCPUrl,
		Protocol:  ep.Protocol,
		Headers:   ep.Headers,
		Ready:     ep.Ready,
	}

	log.Info("Sandbox ready for session", "sandbox_id", ep.ID, "mcp_url", ep.MCPUrl)
	RespondWithJSON(w, http.StatusOK, resp)
}

// HandleGetSandboxStatus handles GET /api/sessions/{session_id}/sandbox.
// It returns the current sandbox state for a session.
func (h *SandboxHandler) HandleGetSandboxStatus(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sandbox-handler").WithValues("operation", "status")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session_id from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	ep, err := h.Provider.Get(r.Context(), sessionID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to get sandbox status", err))
		return
	}
	if ep == nil {
		w.RespondWithError(errors.NewNotFoundError("No sandbox found for session", nil))
		return
	}

	resp := api.SandboxResponse{
		SandboxID: ep.ID,
		MCPUrl:    ep.MCPUrl,
		Protocol:  ep.Protocol,
		Headers:   ep.Headers,
		Ready:     ep.Ready,
	}

	log.Info("Sandbox status retrieved", "sandbox_id", ep.ID)
	RespondWithJSON(w, http.StatusOK, resp)
}

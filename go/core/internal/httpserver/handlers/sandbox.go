package handlers

import (
	"net/http"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/controller/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// SandboxHandler handles sandbox lifecycle requests for sessions.
type SandboxHandler struct {
	*Base
	Manager *sandbox.SandboxManager
}

// NewSandboxHandler creates a new SandboxHandler.
func NewSandboxHandler(base *Base, manager *sandbox.SandboxManager) *SandboxHandler {
	return &SandboxHandler{Base: base, Manager: manager}
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

	ep, err := h.Manager.GetOrCreateSandbox(r.Context(), sessionID, opts)
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

	status := http.StatusOK
	if !ep.Ready {
		status = http.StatusAccepted
		w.Header().Set("Location", r.URL.String())
	}

	log.Info("Sandbox created/retrieved for session", "sandbox_id", ep.ID, "ready", ep.Ready)
	RespondWithJSON(w, status, resp)
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

	ep := h.Manager.GetSandbox(sessionID)
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

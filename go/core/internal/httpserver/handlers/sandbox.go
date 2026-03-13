package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	api "github.com/kagent-dev/kagent/go/core/internal/controller/sandbox"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	httpapi "github.com/kagent-dev/kagent/go/api/httpapi"
)

const (
	// Maximum time to wait for a sandbox to become ready.
	sandboxCreateTimeout = 5 * time.Minute
)

// SandboxHandler handles sandbox lifecycle requests for sessions.
type SandboxHandler struct {
	*Base
	Provider api.SandboxProvider
}

// NewSandboxHandler creates a new SandboxHandler.
func NewSandboxHandler(base *Base, provider api.SandboxProvider) *SandboxHandler {
	return &SandboxHandler{Base: base, Provider: provider}
}

// sandboxTemplateName returns the deterministic name for an agent's auto-generated
// SandboxTemplate. Mirrors the function in the translator package.
func sandboxTemplateName(agentName string) string {
	name := agentName + "-sandbox"
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

// HandleCreateSandbox handles POST /api/sessions/{session_id}/sandbox.
// It resolves the workspace from the session's agent CRD and provisions
// (or returns an existing) sandbox for the given session.
func (h *SandboxHandler) HandleCreateSandbox(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("sandbox-handler").WithValues("operation", "create")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session_id from path", err))
		return
	}
	log = log.WithValues("session_id", sessionID)

	// Verify session exists and get the agent reference.
	userID, err := getUserIDOrAgentUser(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	session, err := h.DatabaseService.GetSession(r.Context(), sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewNotFoundError("Session not found", err))
		return
	}

	if session.AgentID == nil || *session.AgentID == "" {
		w.RespondWithError(errors.NewBadRequestError("Session has no agent reference", nil))
		return
	}

	// Convert the DB agent ID (e.g. "ns__NS__name") back to namespace/name.
	k8sRef := utils.ConvertToKubernetesIdentifier(*session.AgentID)
	parts := strings.SplitN(k8sRef, "/", 2)
	if len(parts) != 2 {
		w.RespondWithError(errors.NewBadRequestError(
			fmt.Sprintf("Invalid agent reference format: %s", k8sRef), nil))
		return
	}
	agentNamespace, agentName := parts[0], parts[1]

	// Fetch the Agent CRD to read workspace config.
	var agent v1alpha2.Agent
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{
		Namespace: agentNamespace,
		Name:      agentName,
	}, &agent); err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to fetch agent CRD", err))
		return
	}

	// Validate workspace is enabled on this agent.
	if agent.Spec.Type != v1alpha2.AgentType_Declarative ||
		agent.Spec.Declarative == nil ||
		agent.Spec.Declarative.Workspace == nil ||
		!agent.Spec.Declarative.Workspace.Enabled {
		w.RespondWithError(errors.NewBadRequestError("Workspace is not enabled for this agent", nil))
		return
	}

	// Determine the sandbox template name.
	ws := agent.Spec.Declarative.Workspace
	templateName := ws.TemplateRef
	if templateName == "" {
		templateName = sandboxTemplateName(agent.Name)
	}

	opts := api.CreateSandboxOptions{
		SessionID: sessionID,
		AgentName: agentName,
		Namespace: agentNamespace,
		WorkspaceRef: api.WorkspaceRef{
			APIGroup:  "extensions.agents.x-k8s.io",
			Kind:      "SandboxTemplate",
			Name:      templateName,
			Namespace: agentNamespace,
		},
	}

	ctx, cancel := context.WithTimeout(r.Context(), sandboxCreateTimeout)
	defer cancel()

	ep, err := h.Provider.GetOrCreate(ctx, opts)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create sandbox", err))
		return
	}

	resp := httpapi.SandboxResponse{
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

	resp := httpapi.SandboxResponse{
		SandboxID: ep.ID,
		MCPUrl:    ep.MCPUrl,
		Protocol:  ep.Protocol,
		Headers:   ep.Headers,
		Ready:     ep.Ready,
	}

	log.Info("Sandbox status retrieved", "sandbox_id", ep.ID)
	RespondWithJSON(w, http.StatusOK, resp)
}

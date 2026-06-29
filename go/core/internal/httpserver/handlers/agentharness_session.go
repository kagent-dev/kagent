package handlers

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// AgentHarnessSessionActorResponse is returned when a per-session actor is
// provisioned or suspended.
type AgentHarnessSessionActorResponse struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	SessionID string `json:"sessionId"`
	ActorID   string `json:"actorId,omitempty"`
	// State is the actor lifecycle state ("running", "suspended", "missing").
	State string `json:"state,omitempty"`
}

// HandleEnsureAgentHarnessSessionActor provisions (creates + resumes) the
// substrate actor for a single AgentHarness chat session. The UI calls this when
// a new chat is started so the actor is warm before the /acp WebSocket connects;
// provisioning could alternatively be deferred to the first message, in which
// case the gateway lazily creates the actor on connect.
func (h *Handlers) HandleEnsureAgentHarnessSessionActor(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agentharness-session-actor").WithValues("operation", "ensure")

	ah, sessionID, apiErr := h.loadAgentHarnessSession(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	res, err := h.AgentHarnessSessionActor.EnsureSessionActor(r.Context(), ah, sessionID)
	if err != nil {
		log.Error(err, "ensure session actor", "session", sessionID)
		w.RespondWithError(errors.NewInternalServerError("Failed to provision session actor", err))
		return
	}

	data := api.NewResponse(AgentHarnessSessionActorResponse{
		Namespace: ah.Namespace,
		Name:      ah.Name,
		SessionID: sessionID,
		ActorID:   res.Handle.ID,
	}, "Session actor ready", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleSuspendAgentHarnessSessionActor checkpoints and frees the substrate
// actor for a single AgentHarness chat session. The actor is resumed
// automatically on the next /acp connection.
func (h *Handlers) HandleSuspendAgentHarnessSessionActor(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agentharness-session-actor").WithValues("operation", "suspend")

	ah, sessionID, apiErr := h.loadAgentHarnessSession(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	if err := h.AgentHarnessSessionActor.SuspendSessionActor(r.Context(), ah, sessionID); err != nil {
		log.Error(err, "suspend session actor", "session", sessionID)
		w.RespondWithError(errors.NewInternalServerError("Failed to suspend session actor", err))
		return
	}

	data := api.NewResponse(AgentHarnessSessionActorResponse{
		Namespace: ah.Namespace,
		Name:      ah.Name,
		SessionID: sessionID,
	}, "Session actor suspended", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// HandleGetAgentHarnessSessionActor reports the lifecycle state of a single
// AgentHarness chat session actor ("running", "suspended", or "missing"). The UI
// uses it to render the per-session status indicator in the sidebar.
func (h *Handlers) HandleGetAgentHarnessSessionActor(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("agentharness-session-actor").WithValues("operation", "status")

	ah, sessionID, apiErr := h.loadAgentHarnessSession(r)
	if apiErr != nil {
		w.RespondWithError(apiErr)
		return
	}

	state, err := h.AgentHarnessSessionActor.GetSessionActorState(r.Context(), ah, sessionID)
	if err != nil {
		log.Error(err, "get session actor state", "session", sessionID)
		w.RespondWithError(errors.NewInternalServerError("Failed to read session actor state", err))
		return
	}

	data := api.NewResponse(AgentHarnessSessionActorResponse{
		Namespace: ah.Namespace,
		Name:      ah.Name,
		SessionID: sessionID,
		State:     string(state),
	}, "Session actor state", false)
	RespondWithJSON(w, http.StatusOK, data)
}

// loadAgentHarnessSession validates the request, loads the AgentHarness and
// returns it together with the session id.
func (h *Handlers) loadAgentHarnessSession(r *http.Request) (*v1alpha2.AgentHarness, string, *errors.APIError) {
	if h.AgentHarnessSessionActor == nil {
		return nil, "", errors.NewNotImplementedError("substrate session actor backend is not configured", nil)
	}

	vars := mux.Vars(r)
	namespace := strings.TrimSpace(vars["namespace"])
	name := strings.TrimSpace(vars["name"])
	sessionID := strings.TrimSpace(vars["session_id"])
	if namespace == "" || name == "" {
		return nil, "", errors.NewBadRequestError("namespace and name are required", nil)
	}
	if sessionID == "" {
		return nil, "", errors.NewBadRequestError("session id is required", nil)
	}

	var ah v1alpha2.AgentHarness
	if err := h.KubeClient.Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: name}, &ah); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, "", errors.NewNotFoundError("AgentHarness not found", err)
		}
		return nil, "", errors.NewInternalServerError("Failed to load AgentHarness", err)
	}

	return &ah, sessionID, nil
}

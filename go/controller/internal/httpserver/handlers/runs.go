package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// RunsHandler handles run-related requests
type RunsHandler struct {
	*Base
}

// NewRunsHandler creates a new RunsHandler
func NewRunsHandler(base *Base) *RunsHandler {
	return &RunsHandler{Base: base}
}

// HandleCreateRun handles POST /api/runs requests
func (h *RunsHandler) HandleCreateRun(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("runs-handler").WithValues("operation", "create")

	request := &autogen_client.CreateRunRequest{}
	if err := DecodeJSONBody(r, request); err != nil {
		w.RespondWithError(errors.NewBadRequestError("Invalid request body", err))
		return
	}

	log = log.WithValues(
		"userID", request.UserID,
		"sessionID", request.SessionID)

	log.V(1).Info("Creating run in Autogen")
	run, err := h.AutogenClient.CreateRun(request)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to create run", err))
		return
	}

	log.Info("Successfully created run", "runID", run.ID)
	RespondWithJSON(w, http.StatusCreated, run)
}

// HandleListSessionRuns handles GET /api/sessions/{sessionID}/runs requests
func (h *RunsHandler) HandleListSessionRuns(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("runs-handler").WithValues("operation", "list-session-runs")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get session ID from path", err))
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing runs for session from Autogen")
	runs, err := h.AutogenClient.ListSessionRuns(sessionID, userID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("Failed to list session runs", err))
		return
	}

	log.Info("Successfully listed session runs", "count", len(runs))
	RespondWithJSON(w, http.StatusOK, runs)
}

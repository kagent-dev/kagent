package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
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
func (h *RunsHandler) HandleCreateRun(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("runs-handler").WithValues("operation", "create")
	log.Info("Handling create run request")

	request := &autogen_client.CreateRunRequest{}
	if err := DecodeJSONBody(r, request); err != nil {
		log.Error(err, "Invalid request body")
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	log = log.WithValues(
		"userID", request.UserID,
		"sessionID", request.SessionID)

	log.V(1).Info("Creating run in Autogen")
	run, err := h.AutogenClient.CreateRun(request)
	if err != nil {
		log.Error(err, "Failed to create run")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully created run", "runID", run.ID)
	RespondWithJSON(w, http.StatusCreated, run)
}

// HandleListSessionRuns handles GET /api/sessions/{sessionID}/runs requests
func (h *RunsHandler) HandleListSessionRuns(w http.ResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("runs-handler").WithValues("operation", "list")
	log.Info("Handling list session runs request")

	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		log.Error(err, "Failed to get session ID from path")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("sessionID", sessionID)

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	log = log.WithValues("userID", userID)

	log.V(1).Info("Listing runs for session from Autogen")
	runs, err := h.AutogenClient.ListSessionRuns(sessionID, userID)
	if err != nil {
		log.Error(err, "Failed to list session runs")
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info("Successfully listed session runs", "count", len(runs))
	RespondWithJSON(w, http.StatusOK, runs)
}

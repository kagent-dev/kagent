package handlers

import (
	"net/http"

	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
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
	request := &autogen_client.CreateRunRequest{}
	if err := DecodeJSONBody(r, request); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	run, err := h.AutogenClient.CreateRun(request)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusCreated, run)
}

// HandleListSessionRuns handles GET /api/sessions/{sessionID}/runs requests
func (h *RunsHandler) HandleListSessionRuns(w http.ResponseWriter, r *http.Request) {
	sessionID, err := GetIntPathParam(r, "sessionID")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	runs, err := h.AutogenClient.ListSessionRuns(sessionID, userID)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	RespondWithJSON(w, http.StatusOK, runs)
}

package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/kagent-dev/kagent/go/internal/database"
	"github.com/kagent-dev/kagent/go/internal/httpserver/errors"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// FeedbackHandler handles user feedback submissions
type FeedbackHandler struct {
	*Base
}

// NewFeedbackHandler creates a new feedback handler
func NewFeedbackHandler(base *Base) *FeedbackHandler {
	return &FeedbackHandler{Base: base}
}

// HandleCreateFeedback handles the submission of user feedback and forwards it to the Python backend
func (h *FeedbackHandler) HandleCreateFeedback(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("feedback-handler").WithValues("operation", "create-feedback")

	log.Info("Received feedback submission")

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error(err, "Failed to read request body")
		w.RespondWithError(errors.NewBadRequestError("Failed to read request body", err))
		return
	}

	// Parse the feedback submission request
	var feedbackReq database.Feedback
	if err := json.Unmarshal(body, &feedbackReq); err != nil {
		log.Error(err, "Failed to parse feedback data")
		w.RespondWithError(errors.NewBadRequestError("Invalid feedback data format", err))
		return
	}

	// Validate the request
	if feedbackReq.FeedbackText == "" {
		log.Error(nil, "Missing required field: feedbackText")
		w.RespondWithError(errors.NewBadRequestError("Missing required field: feedbackText", nil))
		return
	}

	err = h.DatabaseService.CreateFeedback(&feedbackReq)
	if err != nil {
		log.Error(err, "Failed to create feedback")
		w.RespondWithError(errors.NewInternalServerError("Failed to create feedback", err))
		return
	}

	log.Info("Feedback successfully submitted")
	data := api.NewResponse(struct{}{}, "Feedback submitted successfully", false)
	RespondWithJSON(w, http.StatusOK, data)
}

func (h *FeedbackHandler) HandleListFeedback(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("feedback-handler").WithValues("operation", "list-feedback")

	log.Info("Listing feedback")

	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}

	feedback, err := h.DatabaseService.ListFeedback(userID)
	if err != nil {
		log.Error(err, "Failed to list feedback")
		w.RespondWithError(errors.NewInternalServerError("Failed to list feedback", err))
		return
	}

	log.Info("Feedback listed successfully")
	data := api.NewResponse(feedback, "Successfully listed feedback", false)
	RespondWithJSON(w, http.StatusOK, data)
}

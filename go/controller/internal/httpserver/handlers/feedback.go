package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/errors"
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
	var feedbackReq client.FeedbackSubmission
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

	err = h.AutogenClient.CreateFeedback(&feedbackReq)
	if err != nil {
		log.Error(err, "Failed to create feedback")
		w.RespondWithError(errors.NewInternalServerError("Failed to create feedback", err))
		return
	}

	log.Info("Feedback successfully submitted")
	RespondWithJSON(w, http.StatusOK, "Feedback submitted successfully")
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

	feedback, err := h.AutogenClient.ListFeedback(userID)
	if err != nil {
		log.Error(err, "Failed to list feedback")
		w.RespondWithError(errors.NewInternalServerError("Failed to list feedback", err))
		return
	}

	log.Info("Feedback listed successfully")
	RespondWithJSON(w, http.StatusOK, feedback)
}

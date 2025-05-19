package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/kagent-dev/kagent/go/autogen/api"
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
	var feedbackReq api.FeedbackSubmissionRequest
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

	// Get user ID
	userID, err := GetUserID(r)
	if err != nil {
		log.Error(err, "Failed to get user ID")
		w.RespondWithError(errors.NewBadRequestError("Failed to get user ID", err))
		return
	}
	log = log.WithValues("userID", userID)

	// Forward to backend
	path := "/feedback"
	url := h.AutogenClient.BaseURL + path

	// Create a new request with the original body (must use the body we've already read)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Error(err, "Failed to create feedback request")
		w.RespondWithError(errors.NewInternalServerError("Failed to process feedback", err))
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := h.AutogenClient.HTTPClient.Do(httpReq)
	if err != nil {
		log.Error(err, "Failed to send feedback request")
		w.RespondWithError(errors.NewInternalServerError("Failed to process feedback", err))
		return
	}
	defer resp.Body.Close()

	// Handle error responses
	if resp.StatusCode >= 400 {
		log.Error(nil, "Backend returned error response", "status", resp.StatusCode)
		w.RespondWithError(errors.NewInternalServerError("Failed to process feedback on backend", nil))
		return
	}

	// Parse response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error(err, "Failed to read response body")
		w.RespondWithError(errors.NewInternalServerError("Failed to read feedback response", err))
		return
	}

	// Parse into the standard response structure
	var result api.FeedbackSubmissionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Error(err, "Failed to parse response")
		w.RespondWithError(errors.NewInternalServerError("Failed to parse feedback response", err))
		return
	}

	log.Info("Feedback successfully submitted")
	RespondWithJSON(w, http.StatusOK, result)
}

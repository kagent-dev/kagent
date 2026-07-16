package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// SessionSharesHandler handles session share CRUD operations.
type SessionSharesHandler struct {
	*Base
}

func NewSessionSharesHandler(base *Base) *SessionSharesHandler {
	return &SessionSharesHandler{Base: base}
}

func generateShareToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// createSessionShareRequest is the optional POST body for creating a share.
// ReadOnly defaults to true when omitted.
type createSessionShareRequest struct {
	ReadOnly *bool `json:"read_only"`
}

// HandleCreateSessionShare handles POST /api/sessions/{session_id}/shares.
// Only the session owner may create share links.
func (h *SessionSharesHandler) HandleCreateSessionShare(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("session-shares").WithValues("op", "create")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("missing session_id", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("failed to get user ID", err))
		return
	}

	// Default read_only to true; explicit false opt-in to read-write.
	readOnly := true
	if r.Body != nil && r.ContentLength != 0 {
		var body createSessionShareRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.RespondWithError(errors.NewBadRequestError("invalid request body", err))
			return
		}
		if body.ReadOnly != nil {
			readOnly = *body.ReadOnly
		}
	}

	// Verify the session belongs to the caller.
	if _, err := h.DatabaseService.GetSession(r.Context(), sessionID, userID); err != nil {
		RespondNotFoundOrError(w, "session not found", err)
		return
	}

	token, err := generateShareToken()
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("failed to generate token", err))
		return
	}

	share := &dbpkg.SessionShare{
		Token:     token,
		SessionID: sessionID,
		UserID:    userID,
		ReadOnly:  readOnly,
	}
	created, err := h.DatabaseService.CreateSessionShare(r.Context(), share)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("failed to create share", err))
		return
	}

	log.Info("created session share", "sessionID", sessionID)
	RespondWithJSON(w, http.StatusCreated, api.NewResponse(created, "share created", false))
}

// HandleListSessionShares handles GET /api/sessions/{session_id}/shares.
// Only the session owner may list share links.
func (h *SessionSharesHandler) HandleListSessionShares(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("session-shares").WithValues("op", "list")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("missing session_id", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("failed to get user ID", err))
		return
	}

	// Verify the session belongs to the caller.
	if _, err := h.DatabaseService.GetSession(r.Context(), sessionID, userID); err != nil {
		RespondNotFoundOrError(w, "session not found", err)
		return
	}

	shares, err := h.DatabaseService.ListSessionSharesBySession(r.Context(), sessionID)
	if err != nil {
		w.RespondWithError(errors.NewInternalServerError("failed to list shares", err))
		return
	}

	log.V(1).Info("listed session shares", "sessionID", sessionID, "count", len(shares))
	RespondWithJSON(w, http.StatusOK, api.NewResponse(shares, "shares listed", false))
}

// HandleDeleteSessionShare handles DELETE /api/sessions/{session_id}/shares/{token}.
// Only the session owner may delete share links.
func (h *SessionSharesHandler) HandleDeleteSessionShare(w ErrorResponseWriter, r *http.Request) {
	log := ctrllog.FromContext(r.Context()).WithName("session-shares").WithValues("op", "delete")

	sessionID, err := GetPathParam(r, "session_id")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("missing session_id", err))
		return
	}

	token, err := GetPathParam(r, "token")
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("missing token", err))
		return
	}

	userID, err := GetUserID(r)
	if err != nil {
		w.RespondWithError(errors.NewBadRequestError("failed to get user ID", err))
		return
	}

	// Verify the session belongs to the caller before attempting deletion.
	if _, err := h.DatabaseService.GetSession(r.Context(), sessionID, userID); err != nil {
		RespondNotFoundOrError(w, "session not found", err)
		return
	}

	if err := h.DatabaseService.DeleteSessionShare(r.Context(), token, sessionID, userID); err != nil {
		w.RespondWithError(errors.NewInternalServerError("failed to delete share", err))
		return
	}

	log.Info("deleted session share", "sessionID", sessionID)
	RespondWithJSON(w, http.StatusOK, api.NewResponse(struct{}{}, "share deleted", false))
}

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
)

func TestSessionSharesHandler(t *testing.T) {
	setupHandler := func(t *testing.T) (*handlers.SessionSharesHandler, dbpkg.Client, *mockErrorResponseWriter) {
		t.Helper()
		dbClient := setupTestDBClient(t)
		base := &handlers.Base{
			DatabaseService: dbClient,
		}
		handler := handlers.NewSessionSharesHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, dbClient, responseRecorder
	}

	createTestSession := func(t *testing.T, dbClient dbpkg.Client, sessionID, userID string) {
		t.Helper()
		agentID := "agent-1"
		session := &dbpkg.Session{
			ID:      sessionID,
			Name:    new(sessionID),
			UserID:  userID,
			AgentID: &agentID,
		}
		require.NoError(t, dbClient.StoreSession(context.Background(), session))
	}

	createTestShare := func(t *testing.T, dbClient dbpkg.Client, token, sessionID, userID string, readOnly bool) {
		t.Helper()
		share := &dbpkg.SessionShare{
			Token:     token,
			SessionID: sessionID,
			UserID:    userID,
			ReadOnly:  readOnly,
		}
		_, err := dbClient.CreateSessionShare(context.Background(), share)
		require.NoError(t, err)
	}

	t.Run("HandleCreateSessionShare", func(t *testing.T) {
		t.Run("DefaultsToReadOnly", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"

			createTestSession(t, dbClient, sessionID, userID)

			req := httptest.NewRequest("POST", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleCreateSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var response api.StandardResponse[*dbpkg.SessionShare]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "share created", response.Message)
			assert.True(t, response.Data.ReadOnly)
			assert.Equal(t, sessionID, response.Data.SessionID)
			assert.NotEmpty(t, response.Data.Token)
		})

		t.Run("ExplicitReadWrite", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"

			createTestSession(t, dbClient, sessionID, userID)

			readOnly := false
			body, _ := json.Marshal(map[string]bool{"read_only": readOnly})
			req := httptest.NewRequest("POST", "/api/sessions/"+sessionID+"/shares", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleCreateSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var response api.StandardResponse[*dbpkg.SessionShare]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.False(t, response.Data.ReadOnly)
		})

		t.Run("ExplicitReadOnly", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"

			createTestSession(t, dbClient, sessionID, userID)

			body, _ := json.Marshal(map[string]bool{"read_only": true})
			req := httptest.NewRequest("POST", "/api/sessions/"+sessionID+"/shares", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleCreateSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var response api.StandardResponse[*dbpkg.SessionShare]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.True(t, response.Data.ReadOnly)
		})

		t.Run("SessionNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "non-existent-session"

			req := httptest.NewRequest("POST", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleCreateSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			sessionID := "test-session-1"

			req := httptest.NewRequest("POST", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleCreateSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("WrongOwner", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			ownerID := "other-user"
			callerID := "user-a"
			sessionID := "test-session-1"

			createTestSession(t, dbClient, sessionID, ownerID)

			req := httptest.NewRequest("POST", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, callerID)

			handler.HandleCreateSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleListSessionShares", func(t *testing.T) {
		t.Run("EmptyList", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"

			createTestSession(t, dbClient, sessionID, userID)

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleListSessionShares(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[[]dbpkg.SessionShare]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Empty(t, response.Data)
		})

		t.Run("WithShares", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"

			createTestSession(t, dbClient, sessionID, userID)
			createTestShare(t, dbClient, "token-ro", sessionID, userID, true)
			createTestShare(t, dbClient, "token-rw", sessionID, userID, false)

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleListSessionShares(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[[]dbpkg.SessionShare]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Len(t, response.Data, 2)

			byToken := make(map[string]dbpkg.SessionShare, len(response.Data))
			for _, s := range response.Data {
				byToken[s.Token] = s
			}
			roShare, ok := byToken["token-ro"]
			require.True(t, ok)
			assert.True(t, roShare.ReadOnly)

			rwShare, ok := byToken["token-rw"]
			require.True(t, ok)
			assert.False(t, rwShare.ReadOnly)
		})

		t.Run("SessionNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "non-existent-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleListSessionShares(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			sessionID := "test-session-1"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/shares", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleListSessionShares(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteSessionShare", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"
			token := "test-token-123"

			createTestSession(t, dbClient, sessionID, userID)
			createTestShare(t, dbClient, token, sessionID, userID, true)

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID+"/shares/"+token, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID, "token": token})
			req = setUser(req, userID)

			handler.HandleDeleteSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[struct{}]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "share deleted", response.Message)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			sessionID := "test-session-1"
			token := "test-token-123"

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID+"/shares/"+token, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID, "token": token})

			handler.HandleDeleteSessionShare(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("WrongOwner", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			ownerID := "owner-user"
			callerID := "attacker-user"
			sessionID := "test-session-1"
			token := "test-token-123"

			createTestSession(t, dbClient, sessionID, ownerID)
			createTestShare(t, dbClient, token, sessionID, ownerID, true)

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID+"/shares/"+token, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID, "token": token})
			req = setUser(req, callerID)

			handler.HandleDeleteSessionShare(responseRecorder, req)

			shares, err := dbClient.ListSessionSharesBySession(context.Background(), sessionID)
			require.NoError(t, err)
			assert.Len(t, shares, 1, "share must not be deleted by a non-owner")
		})

		t.Run("RevokedTokenIsRejected", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "user-a"
			sessionID := "test-session-1"
			token := "test-token-revoke"

			createTestSession(t, dbClient, sessionID, userID)
			createTestShare(t, dbClient, token, sessionID, userID, true)

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID+"/shares/"+token, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID, "token": token})
			req = setUser(req, userID)

			handler.HandleDeleteSessionShare(responseRecorder, req)
			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			// The middleware rejects requests by calling GetSessionShareByToken. Verify the
			// revoked token no longer resolves — any subsequent request carrying it gets 403.
			_, err := dbClient.GetSessionShareByToken(context.Background(), token)
			assert.Error(t, err, "revoked token must not be found; middleware would return 403")
		})
	})
}

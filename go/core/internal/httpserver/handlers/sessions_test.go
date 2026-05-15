package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/database"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kmcp/api/v1alpha1"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func setUser(req *http.Request, userID string) *http.Request {
	ctx := auth.AuthSessionTo(req.Context(), &authimpl.SimpleSession{
		P: auth.Principal{
			User: auth.User{
				ID: userID,
			},
		},
	})
	return req.WithContext(ctx)
}

func TestSessionsHandler(t *testing.T) {
	scheme := runtime.NewScheme()
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func(t *testing.T) (*handlers.SessionsHandler, database.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		dbClient := setupTestDBClient(t)

		base := &handlers.Base{
			KubeClient:         kubeClient,
			DatabaseService:    dbClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		}
		handler := handlers.NewSessionsHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, dbClient, responseRecorder
	}

	createTestAgent := func(t *testing.T, dbClient database.Client, agentRef string) *database.Agent {
		t.Helper()
		agent := &database.Agent{
			ID:           agentRef,
			WorkloadType: v1alpha2.WorkloadModeDeployment,
		}
		require.NoError(t, dbClient.StoreAgent(context.Background(), agent))
		return agent
	}

	createTestSession := func(t *testing.T, dbClient database.Client, sessionID, userID string, agentID string) *database.Session {
		t.Helper()
		session := &database.Session{
			ID:      sessionID,
			Name:    new(sessionID),
			UserID:  userID,
			AgentID: &agentID,
		}
		require.NoError(t, dbClient.StoreSession(context.Background(), session))
		return session
	}

	setSessionActivity := func(t *testing.T, sessionID, userID string, createdAt, updatedAt time.Time) {
		t.Helper()
		_, err := sharedDB.Exec(context.Background(), `
			UPDATE session
			SET created_at = $1, updated_at = $2
			WHERE id = $3 AND user_id = $4
		`, createdAt, updatedAt, sessionID, userID)
		require.NoError(t, err)
	}

	t.Run("HandleListSessions", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

			// Create test sessions
			agentID := "1"
			session1 := createTestSession(t, dbClient, "session-1", userID, agentID)
			session2 := createTestSession(t, dbClient, "session-2", userID, agentID)
			setSessionActivity(t, session1.ID, userID, base, base.Add(2*time.Hour))
			setSessionActivity(t, session2.ID, userID, base.Add(time.Hour), base.Add(time.Hour))

			req := httptest.NewRequest("GET", "/api/sessions?user_id="+userID, nil)
			req = setUser(req, userID)
			handler.HandleListSessions(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[[]*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Len(t, response.Data, 2)
			assert.Equal(t, session1.ID, response.Data[0].ID)
			assert.Equal(t, session2.ID, response.Data[1].ID)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)

			req := httptest.NewRequest("GET", "/api/sessions", nil)
			handler.HandleListSessions(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleCreateSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			agentRef := utils.ConvertToPythonIdentifier("default/test-agent")

			// Create test agent
			createTestAgent(t, dbClient, agentRef)

			sessionReq := api.SessionRequest{
				AgentRef: &agentRef,
				Name:     new("test-session"),
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, userID)

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var response api.StandardResponse[*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "test-session", *response.Data.Name)
			assert.Equal(t, userID, response.Data.UserID)
			assert.NotEmpty(t, response.Data.ID)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			agentRef := utils.ConvertToPythonIdentifier("default/test-agent")

			sessionReq := api.SessionRequest{
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingAgentRef", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			userID := "test-user"

			sessionReq := api.SessionRequest{}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			req = setUser(req, userID)

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("AgentNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			agentRef := utils.ConvertToPythonIdentifier("default/non-existent-agent")

			sessionReq := api.SessionRequest{
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, "test-user")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)

			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("SandboxAgentAllowsOnlyOneSessionGlobally", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			agentRef := utils.ConvertToPythonIdentifier("default/test-sandbox-agent")

			require.NoError(t, dbClient.StoreAgent(context.Background(), &database.Agent{
				ID:           agentRef,
				WorkloadType: v1alpha2.WorkloadModeSandbox,
			}))

			existingAgentID := agentRef
			createTestSession(t, dbClient, "existing-session", "other-user", existingAgentID)

			sessionReq := api.SessionRequest{
				AgentRef: &agentRef,
				Name:     new("second-session"),
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, userID)

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusConflict, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleGetSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionID := "test-session"

			// Create test session
			agentID := "1"
			session := createTestSession(t, dbClient, sessionID, userID, agentID)

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[handlers.SessionResponse]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, session.ID, response.Data.Session.ID)
			assert.Equal(t, session.UserID, response.Data.Session.UserID)
		})

		t.Run("SessionNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionID := "non-existent-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			sessionID := "test-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("OrderAsc", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionID := "test-session"

			// Create test session
			agentID := "1"
			createTestSession(t, dbClient, sessionID, userID, agentID)

			// Create events with different timestamps
			event1 := &database.Event{
				ID:        "event-1",
				SessionID: sessionID,
				UserID:    userID,
				CreatedAt: time.Now().Add(-2 * time.Hour),
				Data:      "{}",
			}
			event2 := &database.Event{
				ID:        "event-2",
				SessionID: sessionID,
				UserID:    userID,
				CreatedAt: time.Now().Add(-1 * time.Hour),
				Data:      "{}",
			}
			dbClient.StoreEvents(context.Background(), event1, event2)

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"?order=asc", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[handlers.SessionResponse]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			require.Len(t, response.Data.Events, 2)
			assert.Equal(t, event1.ID, response.Data.Events[0].ID)
			assert.Equal(t, event2.ID, response.Data.Events[1].ID)
		})
	})

	t.Run("HandleUpdateSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionName := "test-session"

			// Create test agent and session
			agentRef := utils.ConvertToPythonIdentifier("default/test-agent")
			agent := createTestAgent(t, dbClient, agentRef)
			session := createTestSession(t, dbClient, sessionName, userID, agent.ID)

			newAgentRef := utils.ConvertToPythonIdentifier("default/new-agent")
			newAgent := createTestAgent(t, dbClient, newAgentRef)

			sessionReq := api.SessionRequest{
				Name:     &sessionName,
				AgentRef: &newAgentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("PUT", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, userID)

			handler.HandleUpdateSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, session.ID, response.Data.ID)
			assert.Equal(t, newAgent.ID, *response.Data.AgentID)
		})

		t.Run("MissingSessionName", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			userID := "test-user"
			agentRef := "default/test-agent"

			sessionReq := api.SessionRequest{
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("PUT", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, userID)

			handler.HandleUpdateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("SessionNotFound", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionName := "non-existent-session"
			agentRef := "default/test-agent"

			createTestAgent(t, dbClient, agentRef)

			sessionReq := api.SessionRequest{
				Name:     &sessionName,
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("PUT", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			req = setUser(req, userID)

			handler.HandleUpdateSession(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionID := "test-session"

			// Session.AgentID must resolve via GetAgent (non-Sandbox: delete allowed).
			require.NoError(t, dbClient.StoreAgent(context.Background(), &database.Agent{
				ID:   "1",
				Type: "Declarative",
			}))
			agentID := "1"
			createTestSession(t, dbClient, sessionID, userID, agentID)

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleDeleteSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[struct{}]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "Session deleted successfully", response.Message)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			sessionID := "test-session"

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleDeleteSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleGetSessionsForAgent", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			namespace := "default"
			agentName := "test-agent"
			agentRef := utils.ConvertToPythonIdentifier(namespace + "/" + agentName)

			// Create test agent and sessions
			agent := createTestAgent(t, dbClient, agentRef)
			session1 := createTestSession(t, dbClient, "session-1", userID, agent.ID)
			session2 := createTestSession(t, dbClient, "session-2", userID, agent.ID)
			base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			setSessionActivity(t, session1.ID, userID, base, base.Add(2*time.Hour))
			setSessionActivity(t, session2.ID, userID, base.Add(time.Hour), base.Add(time.Hour))

			req := httptest.NewRequest("GET", "/api/agents/"+namespace+"/"+agentName+"/sessions", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": namespace, "name": agentName})
			req = setUser(req, userID)

			handler.HandleGetSessionsForAgent(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[[]*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Len(t, response.Data, 2)
			assert.Equal(t, session1.ID, response.Data[0].ID)
			assert.Equal(t, session2.ID, response.Data[1].ID)
		})

		t.Run("AgentNotFound", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			userID := "test-user"
			namespace := "default"
			agentName := "non-existent-agent"

			req := httptest.NewRequest("GET", "/api/agents/"+namespace+"/"+agentName+"/sessions", nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": namespace, "name": agentName})
			req = setUser(req, userID)

			handler.HandleGetSessionsForAgent(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleListTasksForSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, responseRecorder := setupHandler(t)
			userID := "test-user"
			sessionID := "test-session"

			// Create test session and tasks
			agentID := "1"
			createTestSession(t, dbClient, sessionID, userID, agentID)

			require.NoError(t, dbClient.StoreTask(context.Background(), &protocol.Task{
				ID:        "task-1",
				ContextID: sessionID,
			}))
			require.NoError(t, dbClient.StoreTask(context.Background(), &protocol.Task{
				ID:        "task-2",
				ContextID: sessionID,
			}))

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/tasks", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})
			req = setUser(req, userID)

			handler.HandleListTasksForSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[[]*protocol.Task]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Len(t, response.Data, 2)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, responseRecorder := setupHandler(t)
			sessionID := "test-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/tasks", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleListTasksForSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})
}

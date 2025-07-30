package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	autogen_fake "github.com/kagent-dev/kagent/go/internal/autogen/client/fake"
	"github.com/kagent-dev/kagent/go/internal/database"
	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
	"github.com/kagent-dev/kagent/go/pkg/client/api"
)

func TestSessionsHandler(t *testing.T) {
	scheme := runtime.NewScheme()
	err := v1alpha1.AddToScheme(scheme)
	require.NoError(t, err)

	setupHandler := func() (*handlers.SessionsHandler, *database_fake.InMemmoryFakeClient, autogen_client.Client, *mockErrorResponseWriter) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		dbClient := database_fake.NewClient()
		autogenClient := autogen_fake.NewInMemoryAutogenClient()

		base := &handlers.Base{
			KubeClient:         kubeClient,
			DatabaseService:    dbClient,
			AutogenClient:      autogenClient,
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
		}
		handler := handlers.NewSessionsHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, dbClient.(*database_fake.InMemmoryFakeClient), autogenClient, responseRecorder
	}

	createTestAgent := func(dbClient database.Client, agentRef string) *database.Agent {
		agent := &database.Agent{
			Name: agentRef,
		}
		dbClient.CreateAgent(agent)
		// The fake client should assign an ID, but we'll use a default for testing
		agent.ID = 1 // Simulate the ID that would be assigned by GORM
		return agent
	}

	createTestSession := func(dbClient database.Client, sessionID, userID string, agentID *uint) *database.Session {
		session := &database.Session{
			ID:      sessionID,
			Name:    sessionID,
			UserID:  userID,
			AgentID: agentID,
		}
		dbClient.CreateSession(session)
		return session
	}

	t.Run("HandleListSessions", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"

			// Create test sessions
			agentID := uint(1)
			session1 := createTestSession(dbClient, "session-1", userID, &agentID)
			session2 := createTestSession(dbClient, "session-2", userID, &agentID)

			req := httptest.NewRequest("GET", "/api/sessions?user_id="+userID, nil)
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
			handler, _, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("GET", "/api/sessions", nil)
			handler.HandleListSessions(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleCreateSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			agentRef := "default/test-agent"

			// Create test agent
			createTestAgent(dbClient, agentRef)

			sessionReq := api.SessionRequest{
				UserID:   userID,
				AgentRef: &agentRef,
				Name:     ptr.To("test-session"),
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var response api.StandardResponse[*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "test-session", response.Data.Name)
			assert.Equal(t, userID, response.Data.UserID)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			agentRef := "default/test-agent"

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
			handler, _, _, responseRecorder := setupHandler()
			userID := "test-user"

			sessionReq := api.SessionRequest{
				UserID: userID,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("AgentNotFound", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			userID := "test-user"
			agentRef := "default/non-existent-agent"

			sessionReq := api.SessionRequest{
				UserID:   userID,
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString("invalid json"))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleCreateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleGetSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionID := "test-session"

			// Create test session
			agentID := uint(1)
			session := createTestSession(dbClient, sessionID, userID, &agentID)

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, session.ID, response.Data.ID)
			assert.Equal(t, session.UserID, response.Data.UserID)
		})

		t.Run("SessionNotFound", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionID := "non-existent-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			sessionID := "test-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleGetSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleUpdateSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionName := "test-session"

			// Create test agent and session
			agentRef := "default/test-agent"
			agent := createTestAgent(dbClient, agentRef)
			session := createTestSession(dbClient, sessionName, userID, &agent.ID)

			newAgentRef := "default/new-agent"
			newAgent := createTestAgent(dbClient, newAgentRef)

			sessionReq := api.SessionRequest{
				UserID:   userID,
				Name:     &sessionName,
				AgentRef: &newAgentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("PUT", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleUpdateSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[*database.Session]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, session.ID, response.Data.ID)
			assert.Equal(t, newAgent.ID, *response.Data.AgentID)
		})

		t.Run("MissingSessionName", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			userID := "test-user"
			agentRef := "default/test-agent"

			sessionReq := api.SessionRequest{
				UserID:   userID,
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("PUT", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleUpdateSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})

		t.Run("SessionNotFound", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionName := "non-existent-session"
			agentRef := "default/test-agent"

			createTestAgent(dbClient, agentRef)

			sessionReq := api.SessionRequest{
				UserID:   userID,
				Name:     &sessionName,
				AgentRef: &agentRef,
			}

			jsonBody, _ := json.Marshal(sessionReq)
			req := httptest.NewRequest("PUT", "/api/sessions", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			handler.HandleUpdateSession(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleDeleteSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionID := "test-session"

			// Create test session
			agentID := uint(1)
			createTestSession(dbClient, sessionID, userID, &agentID)

			req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID+"?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleDeleteSession(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[struct{}]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, "Session deleted successfully", response.Message)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
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
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			namespace := "default"
			agentName := "test-agent"
			agentRef := namespace + "/" + agentName

			// Create test agent and sessions
			agent := createTestAgent(dbClient, agentRef)
			session1 := createTestSession(dbClient, "session-1", userID, &agent.ID)
			session2 := createTestSession(dbClient, "session-2", userID, &agent.ID)

			req := httptest.NewRequest("GET", "/api/agents/"+namespace+"/"+agentName+"/sessions?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": namespace, "name": agentName})

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
			handler, _, _, responseRecorder := setupHandler()
			userID := "test-user"
			namespace := "default"
			agentName := "non-existent-agent"

			req := httptest.NewRequest("GET", "/api/agents/"+namespace+"/"+agentName+"/sessions?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"namespace": namespace, "name": agentName})

			handler.HandleGetSessionsForAgent(responseRecorder, req)

			assert.Equal(t, http.StatusNotFound, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleListSessionTasks", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionID := "test-session"

			// Create test session and tasks
			agentID := uint(1)
			createTestSession(dbClient, sessionID, userID, &agentID)

			task1 := &database.Task{
				ID:        "task-1",
				SessionID: sessionID,
				UserID:    userID,
				Data:      "{}",
			}
			task2 := &database.Task{
				ID:        "task-2",
				SessionID: sessionID,
				UserID:    userID,
				Data:      "{}",
			}
			// Use the fake client's AddTask method for testing
			dbClient.AddTask(task1)
			dbClient.AddTask(task2)

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/tasks?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleListSessionTasks(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response api.StandardResponse[[]*database.Task]
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Len(t, response.Data, 2)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			sessionID := "test-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/tasks", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleListSessionTasks(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})

	t.Run("HandleListSessionMessages", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, dbClient, _, responseRecorder := setupHandler()
			userID := "test-user"
			sessionID := "test-session"

			// Create test session and messages
			agentID := uint(1)
			createTestSession(dbClient, sessionID, userID, &agentID)

			// For messages, we'll just test with empty list since the parsing is complex
			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/messages?user_id="+userID, nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			message := protocol.NewMessageWithContext(protocol.MessageRoleUser, []protocol.Part{
				protocol.NewTextPart("test-message"),
			}, nil, ptr.To(sessionID))
			err := dbClient.CreateMessages(&message)
			require.NoError(t, err)

			handler.HandleListSessionMessages(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			// The response should be autogen events, not raw messages
			var response api.StandardResponse[interface{}]
			err = json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.NotNil(t, response.Data)
		})

		t.Run("MissingUserID", func(t *testing.T) {
			handler, _, _, responseRecorder := setupHandler()
			sessionID := "test-session"

			req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/messages", nil)
			req = mux.SetURLVars(req, map[string]string{"session_id": sessionID})

			handler.HandleListSessionMessages(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.NotNil(t, responseRecorder.errorReceived)
		})
	})
}

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

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/autogen/client/fake"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
)

func TestInvokeHandler(t *testing.T) {
	setupHandler := func() (*handlers.InvokeHandler, *fake.InMemoryAutogenClient, *mockErrorResponseWriter) {
		mockClient := fake.NewMockAutogenClient()
		base := &handlers.Base{}
		handler := handlers.NewInvokeHandler(base)
		handler.WithClient(mockClient)
		responseRecorder := newMockErrorResponseWriter()
		return handler, mockClient, responseRecorder
	}

	t.Run("StandardInvoke", func(t *testing.T) {
		handler, mockClient, responseRecorder := setupHandler()

		// Create a team in the in-memory client instead of setting a function
		team := &autogen_client.Team{
			BaseObject: autogen_client.BaseObject{
				Id: 1,
			},
			Component: &api.Component{
				Label:    "test-team",
				Provider: "test-provider",
				Config: map[string]interface{}{
					"test-key": "test-value",
				},
			},
		}
		err := mockClient.CreateTeam(team)
		require.NoError(t, err)

		// Note: InvokeTask will use the default in-memory implementation behavior

		agentID := "1"
		reqBody := handlers.InvokeRequest{
			Message: "Test message",
			UserID:  "test-user",
		}
		jsonBody, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/agents/"+agentID+"/invoke", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		router := mux.NewRouter()
		router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
			handler.HandleInvokeAgent(responseRecorder, r)
		}).Methods("POST")

		router.ServeHTTP(responseRecorder, req)

		assert.Equal(t, http.StatusOK, responseRecorder.Code)

		var response autogen_client.InvokeTaskResult
		err = json.Unmarshal(responseRecorder.Body.Bytes(), &response)
		require.NoError(t, err)

		// Note: The in-memory implementation returns default values
		assert.NotEmpty(t, response.TaskResult.Messages)
	})

	t.Run("HandlerError", func(t *testing.T) {
		handler, _, responseRecorder := setupHandler()

		// Don't create any team - this will cause GetTeamByID to return an error

		agentID := "1"
		reqBody := handlers.InvokeRequest{
			Message: "Test message",
			UserID:  "test-user",
		}
		jsonBody, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/agents/"+agentID+"/invoke", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		router := mux.NewRouter()
		router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
			handler.HandleInvokeAgent(responseRecorder, r)
		}).Methods("POST")

		router.ServeHTTP(responseRecorder, req)

		assert.Equal(t, http.StatusInternalServerError, responseRecorder.Code)
		assert.NotNil(t, responseRecorder.errorReceived)
	})

	t.Run("InvalidAgentIdParameter", func(t *testing.T) {
		handler, _, responseRecorder := setupHandler()

		reqBody := handlers.InvokeRequest{
			Message: "Test message",
			UserID:  "test-user",
		}
		jsonBody, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/agents/invalid/invoke", bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		router := mux.NewRouter()
		router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
			handler.HandleInvokeAgent(responseRecorder, r)
		}).Methods("POST")

		router.ServeHTTP(responseRecorder, req)

		assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		assert.NotNil(t, responseRecorder.errorReceived)
	})
}

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
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
)

func TestInvokeHandler(t *testing.T) {
	setupHandler := func() (*handlers.InvokeHandler, *mockAutogenClient, *mockErrorResponseWriter) {
		mockClient := &mockAutogenClient{}
		base := &handlers.Base{}
		handler := handlers.NewInvokeHandler(base)
		handler.WithClient(mockClient)
		responseRecorder := newMockErrorResponseWriter()
		return handler, mockClient, responseRecorder
	}

	t.Run("StandardInvoke", func(t *testing.T) {
		handler, mockClient, responseRecorder := setupHandler()

		mockClient.getTeamByIDFunc = func(teamID int, userID string) (*autogen_client.Team, error) {
			return &autogen_client.Team{
				Component: &api.Component{
					Label:    "test-team",
					Provider: "test-provider",
					Config: map[string]interface{}{
						"test-key": "test-value",
					},
				},
			}, nil
		}

		mockClient.invokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			return &autogen_client.InvokeTaskResult{
				Duration:   100,
				TaskResult: autogen_client.TaskResult{},
				Usage:      "Test usage",
			}, nil
		}

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
		err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(100), response.Duration)
		assert.Equal(t, autogen_client.TaskResult{}, response.TaskResult)
		assert.Equal(t, "Test usage", response.Usage)
	})

	t.Run("HandlerError", func(t *testing.T) {
		handler, mockClient, responseRecorder := setupHandler()

		mockClient.getTeamByIDFunc = func(teamID int, userID string) (*autogen_client.Team, error) {
			return nil, autogen_client.NotFoundError
		}

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

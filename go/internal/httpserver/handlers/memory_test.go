package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/internal/httpserver/handlers"
)

func TestMemoryHandler(t *testing.T) {
	setupHandler := func() (*handlers.MemoryHandler, *mockErrorResponseWriter) {
		base := &handlers.Base{
			DefaultModelConfig: types.NamespacedName{Namespace: "default", Name: "default"},
			DatabaseService:    database_fake.NewClient(),
			Authorizer:         &auth.NoopAuthorizer{},
		}
		handler := handlers.NewMemoryHandler(base)
		responseRecorder := newMockErrorResponseWriter()
		return handler, responseRecorder
	}

	t.Run("AddSession", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Content:   "This is a test conversation",
				Vector:    []float32{0.1, 0.2, 0.3, 0.4, 0.5},
				Metadata:  json.RawMessage(`{"session_id": "session-abc"}`),
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusCreated, responseRecorder.Code)

			var response map[string]string
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Contains(t, response, "id")
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBufferString("invalid json"))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("MissingRequiredFields_AgentName", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{
				AgentName: "", // Missing
				UserID:    "user123",
				Content:   "This is a test",
				Vector:    []float32{0.1, 0.2, 0.3},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("MissingRequiredFields_UserID", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "", // Missing
				Content:   "This is a test",
				Vector:    []float32{0.1, 0.2, 0.3},
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("MissingRequiredFields_Vector", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.AddSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Content:   "This is a test",
				Vector:    []float32{}, // Empty
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/sessions", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.AddSession(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})
	})

	t.Run("Search", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Vector:    []float32{0.1, 0.2, 0.3, 0.4, 0.5},
				Limit:     5,
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)

			var response []handlers.SearchSessionMemoryResponse
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			require.NoError(t, err)
			// Fake client returns empty results, which is fine for this test
			assert.NotNil(t, response)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBufferString("invalid json"))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("MissingRequiredFields_AgentName", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "", // Missing
				UserID:    "user123",
				Vector:    []float32{0.1, 0.2, 0.3},
				Limit:     5,
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("MissingRequiredFields_UserID", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "", // Missing
				Vector:    []float32{0.1, 0.2, 0.3},
				Limit:     5,
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("MissingRequiredFields_Vector", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Vector:    []float32{}, // Empty
				Limit:     5,
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
		})

		t.Run("DefaultLimit", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Vector:    []float32{0.1, 0.2, 0.3},
				Limit:     0, // Should default to 5
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
		})

		t.Run("WithMinScore", func(t *testing.T) {
			handler, responseRecorder := setupHandler()

			reqBody := handlers.SearchSessionMemoryRequest{
				AgentName: "test-agent",
				UserID:    "user123",
				Vector:    []float32{0.1, 0.2, 0.3},
				Limit:     5,
				MinScore:  0.8, // Filter results below 0.8
			}

			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/memories/search", bytes.NewBuffer(jsonBody))
			req = setUser(req, "test-user")
			req.Header.Set("Content-Type", "application/json")

			handler.Search(responseRecorder, req)

			assert.Equal(t, http.StatusOK, responseRecorder.Code)
		})
	})
}

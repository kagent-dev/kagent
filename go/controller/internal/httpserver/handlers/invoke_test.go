package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Mock ErrorResponseWriter implementation
type mockErrorResponseWriter struct {
	*httptest.ResponseRecorder
	errorReceived error
}

func newMockErrorResponseWriter() *mockErrorResponseWriter {
	return &mockErrorResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

func (m *mockErrorResponseWriter) RespondWithError(err error) {
	m.errorReceived = err
	handlers.RespondWithError(m, http.StatusInternalServerError, err.Error())
}

// We only need to mock the methods we use, not the entire client
type mockAutogenClient struct {
	createSessionFunc func(*autogen_client.CreateSession) (*autogen_client.Session, error)
	createRunFunc     func(*autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error)
}

func (m *mockAutogenClient) CreateSession(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
	return m.createSessionFunc(req)
}

func (m *mockAutogenClient) CreateRun(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
	return m.createRunFunc(req)
}

// Add stubs for other methods to satisfy the interface
func (m *mockAutogenClient) ListSessions(userID string) ([]*autogen_client.Session, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetSession(sessionID int, userID string) (*autogen_client.Session, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListSessionRuns(sessionID int, userID string) ([]*autogen_client.Run, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListRuns(userID string) ([]*autogen_client.Run, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetRun(runID string) (*autogen_client.Run, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetVersion() (string, error) {
	return "", nil
}

func (m *mockAutogenClient) Validate(req *autogen_client.ValidationRequest) (*autogen_client.ValidationResponse, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListTeams(userID string) ([]*autogen_client.Team, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetTeam(teamID int, userID string) (*autogen_client.Team, error) {
	return nil, nil
}

func (m *mockAutogenClient) CreateTeam(team *autogen_client.Team) error {
	return nil
}

func (m *mockAutogenClient) UpdateTeam(team *autogen_client.Team) error {
	return nil
}

func (m *mockAutogenClient) DeleteTeam(teamID int, userID string) error {
	return nil
}

func (m *mockAutogenClient) ListTools() ([]*autogen_client.Tool, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListToolServers() ([]*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *mockAutogenClient) CreateToolServer(server *autogen_client.ToolServer) error {
	return nil
}

func (m *mockAutogenClient) DeleteToolServer(name string) error {
	return nil
}

// TestInvokeHandler tests the InvokeHandler functions
func TestInvokeHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Invoke Handler Suite")
}

// helper function to create a Base handler with a client interface
func createBaseWithClient(client *mockAutogenClient) *handlers.Base {
	// create a handler with a nil client (we'll use the mock directly)
	base := &handlers.Base{
		AutogenClient: nil,
	}
	
	return base
}

var _ = Describe("InvokeHandler", func() {
	var (
		handler          *handlers.InvokeHandler
		mockClient       *mockAutogenClient
		responseRecorder *mockErrorResponseWriter
	)

	BeforeEach(func() {
		mockClient = &mockAutogenClient{}
		
		// create Base handler manually using the helper function
		base := createBaseWithClient(mockClient)
		handler = handlers.NewInvokeHandler(base)
		
		// set the mock client for testing
		handler.SetTestClient(mockClient)
		
		responseRecorder = newMockErrorResponseWriter()
	})

	Context("HandleInvokeAgent", func() {
		It("should handle synchronous invocation successfully", func() {
			// mock session creation
			mockClient.createSessionFunc = func(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
				return &autogen_client.Session{
					ID:      42,
					UserID:  req.UserID,
					Version: "1.0",
					TeamID:  req.TeamID,
					Name:    req.Name,
				}, nil
			}

			// mock run creation
			mockClient.createRunFunc = func(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
				return &autogen_client.CreateRunResult{
					ID: 123,
				}, nil
			}

			// create test request
			agentID := "1"
			reqBody := handlers.InvokeRequest{
				Message: "Test message",
				Sync:    true,
				UserID:  "test-user",
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/agents/"+agentID+"/invoke", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// add agentId to request context via gorilla/mux
			router := mux.NewRouter()
			router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleInvokeAgent(responseRecorder, r)
			}).Methods("POST")

			// execute request
			router.ServeHTTP(responseRecorder, req)

			// verify response
			Expect(responseRecorder.Code).To(Equal(http.StatusOK))

			var response handlers.InvokeResponse
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())

			Expect(response.SessionID).To(Equal("42"))
			Expect(response.Status).To(Equal("completed"))
			Expect(response.Response).NotTo(BeEmpty())
		})

		It("should handle asynchronous invocation successfully", func() {
			// mock session creation
			mockClient.createSessionFunc = func(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
				return &autogen_client.Session{
					ID:      42,
					UserID:  req.UserID,
					Version: "1.0",
					TeamID:  req.TeamID,
					Name:    req.Name,
				}, nil
			}

			// mock run creation
			mockClient.createRunFunc = func(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
				return &autogen_client.CreateRunResult{
					ID: 123,
				}, nil
			}

			// create test request
			agentID := "1"
			reqBody := handlers.InvokeRequest{
				Message: "Test message",
				Sync:    false,
				UserID:  "test-user",
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/agents/"+agentID+"/invoke", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// add agentId to request context via gorilla/mux
			router := mux.NewRouter()
			router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleInvokeAgent(responseRecorder, r)
			}).Methods("POST")

			// execute request
			router.ServeHTTP(responseRecorder, req)

			// verify response
			Expect(responseRecorder.Code).To(Equal(http.StatusOK))

			var response handlers.InvokeResponse
			err := json.Unmarshal(responseRecorder.Body.Bytes(), &response)
			Expect(err).NotTo(HaveOccurred())

			Expect(response.SessionID).To(Equal("42"))
			Expect(response.Status).To(Equal("processing"))
			Expect(response.StatusURL).To(Equal("/api/sessions/42"))
		})

		It("should handle errors in session creation", func() {
			// mock session creation failure
			mockClient.createSessionFunc = func(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
				return nil, fmt.Errorf("session creation failed")
			}

			// create test request
			agentID := "1"
			reqBody := handlers.InvokeRequest{
				Message: "Test message",
				Sync:    true,
				UserID:  "test-user",
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/agents/"+agentID+"/invoke", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// add agentId to request context via gorilla/mux
			router := mux.NewRouter()
			router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleInvokeAgent(responseRecorder, r)
			}).Methods("POST")

			// execute request
			router.ServeHTTP(responseRecorder, req)

			// verify response
			Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
			Expect(responseRecorder.errorReceived).ToNot(BeNil())
		})

		It("should handle errors in run creation", func() {
			// mock session creation
			mockClient.createSessionFunc = func(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
				return &autogen_client.Session{
					ID:      42,
					UserID:  req.UserID,
					Version: "1.0",
					TeamID:  req.TeamID,
					Name:    req.Name,
				}, nil
			}

			// mock run creation failure
			mockClient.createRunFunc = func(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
				return nil, fmt.Errorf("run creation failed")
			}

			// create test request
			agentID := "1"
			reqBody := handlers.InvokeRequest{
				Message: "Test message",
				Sync:    true,
				UserID:  "test-user",
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/agents/"+agentID+"/invoke", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// add agentId to request context via gorilla/mux
			router := mux.NewRouter()
			router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleInvokeAgent(responseRecorder, r)
			}).Methods("POST")

			// execute request
			router.ServeHTTP(responseRecorder, req)

			// verify response
			Expect(responseRecorder.Code).To(Equal(http.StatusInternalServerError))
			Expect(responseRecorder.errorReceived).ToNot(BeNil())
		})

		It("should handle invalid agentId parameter", func() {
			// create test request with invalid agentId
			reqBody := handlers.InvokeRequest{
				Message: "Test message",
				Sync:    true,
				UserID:  "test-user",
			}
			jsonBody, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/api/agents/invalid/invoke", bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")

			// add agentId to request context via gorilla/mux
			router := mux.NewRouter()
			router.HandleFunc("/api/agents/{agentId}/invoke", func(w http.ResponseWriter, r *http.Request) {
				handler.HandleInvokeAgent(responseRecorder, r)
			}).Methods("POST")

			// execute request
			router.ServeHTTP(responseRecorder, req)

			// verify response
			Expect(responseRecorder.Code).To(Equal(http.StatusBadRequest))
			Expect(responseRecorder.errorReceived).ToNot(BeNil())
		})
	})
}) 

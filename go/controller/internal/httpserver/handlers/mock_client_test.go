package handlers_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/controller/internal/httpserver/handlers"
)

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

	if errWithStatus, ok := err.(interface{ StatusCode() int }); ok {
		handlers.RespondWithError(m, errWithStatus.StatusCode(), err.Error())
	} else {
		handlers.RespondWithError(m, http.StatusInternalServerError, err.Error())
	}
}

type mockAutogenClient struct {
	createSessionFunc func(*autogen_client.CreateSession) (*autogen_client.Session, error)
	createRunFunc     func(*autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error)
	getTeamByIDFunc   func(teamID int, userID string) (*autogen_client.Team, error)
	invokeTaskFunc    func(*autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error)
}

func (m *mockAutogenClient) CreateSession(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
	return m.createSessionFunc(req)
}

func (m *mockAutogenClient) CreateRun(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
	return m.createRunFunc(req)
}

func (m *mockAutogenClient) GetTeamByID(teamID int, userID string) (*autogen_client.Team, error) {
	return m.getTeamByIDFunc(teamID, userID)
}

func (m *mockAutogenClient) CreateFeedback(feedback *autogen_client.FeedbackSubmission) error {
	return nil
}

func (m *mockAutogenClient) CreateTeam(team *autogen_client.Team) error {
	return nil
}

func (m *mockAutogenClient) CreateToolServer(toolServer *autogen_client.ToolServer, userID string) (*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *mockAutogenClient) DeleteRun(runID uuid.UUID) error {
	return nil
}

func (m *mockAutogenClient) DeleteSession(sessionID int, userID string) error {
	return nil
}

func (m *mockAutogenClient) DeleteTeam(teamID int, userID string) error {
	return nil
}

func (m *mockAutogenClient) DeleteToolServer(serverID *int, userID string) error {
	return nil
}

func (m *mockAutogenClient) GetRun(runID int) (*autogen_client.Run, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetRunMessages(runID uuid.UUID) ([]*autogen_client.RunMessage, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetSession(sessionLabel string, userID string) (*autogen_client.Session, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetSessionById(sessionID int, userID string) (*autogen_client.Session, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetTeam(teamLabel string, userID string) (*autogen_client.Team, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetTool(provider string, userID string) (*autogen_client.Tool, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetToolServer(serverID int, userID string) (*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetToolServerByLabel(toolServerLabel string, userID string) (*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *mockAutogenClient) GetVersion() (string, error) {
	return "", nil
}

func (m *mockAutogenClient) InvokeSession(sessionID int, userID string, task string) (*autogen_client.TeamResult, error) {
	return nil, nil
}

func (m *mockAutogenClient) InvokeSessionStream(sessionID int, userID string, task string) (<-chan *autogen_client.SseEvent, error) {
	return nil, nil
}

func (m *mockAutogenClient) InvokeTask(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
	return m.invokeTaskFunc(req)
}

func (m *mockAutogenClient) InvokeTaskStream(req *autogen_client.InvokeTaskRequest) (<-chan *autogen_client.SseEvent, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListFeedback(userID string) ([]*autogen_client.FeedbackSubmission, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListRuns(userID string) ([]*autogen_client.Run, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListSessionRuns(sessionID int, userID string) ([]*autogen_client.Run, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListSessions(userID string) ([]*autogen_client.Session, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListSupportedModels() (*autogen_client.ProviderModels, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListTeams(userID string) ([]*autogen_client.Team, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListToolServers(userID string) ([]*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListTools(userID string) ([]*autogen_client.Tool, error) {
	return nil, nil
}

func (m *mockAutogenClient) ListToolsForServer(serverID *int, userID string) ([]*autogen_client.Tool, error) {
	return nil, nil
}

func (m *mockAutogenClient) RefreshToolServer(serverID int, userID string) error {
	return nil
}

func (m *mockAutogenClient) RefreshTools(serverID *int, userID string) error {
	return nil
}

func (m *mockAutogenClient) UpdateSession(sessionID int, userID string, session *autogen_client.Session) (*autogen_client.Session, error) {
	return nil, nil
}

func (m *mockAutogenClient) UpdateToolServer(server *autogen_client.ToolServer, userID string) error {
	return nil
}

func (m *mockAutogenClient) Validate(req *autogen_client.ValidationRequest) (*autogen_client.ValidationResponse, error) {
	return nil, nil
}

package fake

import (
	"github.com/google/uuid"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

type MockAutogenClient struct {
	CreateSessionFunc func(*autogen_client.CreateSession) (*autogen_client.Session, error)
	CreateRunFunc     func(*autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error)
	GetTeamByIDFunc   func(teamID int, userID string) (*autogen_client.Team, error)
	InvokeTaskFunc    func(*autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error)
	GetSessionFunc    func(sessionLabel string, userID string) (*autogen_client.Session, error)
	InvokeSessionFunc func(sessionID int, userID string, task string) (*autogen_client.TeamResult, error)
}

func NewMockAutogenClient() *MockAutogenClient {
	return &MockAutogenClient{}
}

func (m *MockAutogenClient) CreateSession(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(req)
	}
	return nil, nil
}

func (m *MockAutogenClient) CreateRun(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
	if m.CreateRunFunc != nil {
		return m.CreateRunFunc(req)
	}
	return nil, nil
}

func (m *MockAutogenClient) GetTeamByID(teamID int, userID string) (*autogen_client.Team, error) {
	if m.GetTeamByIDFunc != nil {
		return m.GetTeamByIDFunc(teamID, userID)
	}
	return nil, nil
}

func (m *MockAutogenClient) InvokeTask(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
	if m.InvokeTaskFunc != nil {
		return m.InvokeTaskFunc(req)
	}
	return nil, nil
}

func (m *MockAutogenClient) GetSession(sessionLabel string, userID string) (*autogen_client.Session, error) {
	if m.GetSessionFunc != nil {
		return m.GetSessionFunc(sessionLabel, userID)
	}
	return nil, nil
}

func (m *MockAutogenClient) InvokeSession(sessionID int, userID string, task string) (*autogen_client.TeamResult, error) {
	if m.InvokeSessionFunc != nil {
		return m.InvokeSessionFunc(sessionID, userID, task)
	}
	return nil, nil
}

func (m *MockAutogenClient) CreateFeedback(feedback *autogen_client.FeedbackSubmission) error {
	return nil
}

func (m *MockAutogenClient) CreateTeam(team *autogen_client.Team) error {
	return nil
}

func (m *MockAutogenClient) CreateToolServer(toolServer *autogen_client.ToolServer, userID string) (*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *MockAutogenClient) DeleteRun(runID uuid.UUID) error {
	return nil
}

func (m *MockAutogenClient) DeleteSession(sessionID int, userID string) error {
	return nil
}

func (m *MockAutogenClient) DeleteTeam(teamID int, userID string) error {
	return nil
}

func (m *MockAutogenClient) DeleteToolServer(serverID *int, userID string) error {
	return nil
}

func (m *MockAutogenClient) GetRun(runID int) (*autogen_client.Run, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetRunMessages(runID uuid.UUID) ([]*autogen_client.RunMessage, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetSessionById(sessionID int, userID string) (*autogen_client.Session, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetTeam(teamLabel string, userID string) (*autogen_client.Team, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetTool(provider string, userID string) (*autogen_client.Tool, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetToolServer(serverID int, userID string) (*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetToolServerByLabel(toolServerLabel string, userID string) (*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *MockAutogenClient) GetVersion() (string, error) {
	return "", nil
}

func (m *MockAutogenClient) InvokeSessionStream(sessionID int, userID string, task string) (<-chan *autogen_client.SseEvent, error) {
	return nil, nil
}

func (m *MockAutogenClient) InvokeTaskStream(req *autogen_client.InvokeTaskRequest) (<-chan *autogen_client.SseEvent, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListFeedback(userID string) ([]*autogen_client.FeedbackSubmission, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListRuns(userID string) ([]*autogen_client.Run, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListSessionRuns(sessionID int, userID string) ([]*autogen_client.Run, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListSessions(userID string) ([]*autogen_client.Session, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListSupportedModels() (*autogen_client.ProviderModels, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListTeams(userID string) ([]*autogen_client.Team, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListToolServers(userID string) ([]*autogen_client.ToolServer, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListTools(userID string) ([]*autogen_client.Tool, error) {
	return nil, nil
}

func (m *MockAutogenClient) ListToolsForServer(serverID *int, userID string) ([]*autogen_client.Tool, error) {
	return nil, nil
}

func (m *MockAutogenClient) RefreshToolServer(serverID int, userID string) error {
	return nil
}

func (m *MockAutogenClient) RefreshTools(serverID *int, userID string) error {
	return nil
}

func (m *MockAutogenClient) UpdateSession(sessionID int, userID string, session *autogen_client.Session) (*autogen_client.Session, error) {
	return nil, nil
}

func (m *MockAutogenClient) UpdateToolServer(server *autogen_client.ToolServer, userID string) error {
	return nil
}

func (m *MockAutogenClient) Validate(req *autogen_client.ValidationRequest) (*autogen_client.ValidationResponse, error) {
	return nil, nil
}

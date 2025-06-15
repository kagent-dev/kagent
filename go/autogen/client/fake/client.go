package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
)

type InMemoryAutogenClient struct {
	mu sync.RWMutex

	// Storage maps
	sessions           map[int]*autogen_client.Session
	sessionsByLabel    map[string]*autogen_client.Session
	teams              map[int]*autogen_client.Team
	teamsByLabel       map[string]*autogen_client.Team
	runs               map[int]*autogen_client.Run
	runsByUUID         map[uuid.UUID]*autogen_client.Run
	toolServers        map[int]*autogen_client.ToolServer
	toolServersByLabel map[string]*autogen_client.ToolServer
	tools              map[string]*autogen_client.Tool
	toolsByServer      map[int][]*autogen_client.Tool
	feedback           []*autogen_client.FeedbackSubmission
	runMessages        map[uuid.UUID][]*autogen_client.RunMessage

	// ID counters
	nextSessionID    int
	nextTeamID       int
	nextRunID        int
	nextToolServerID int
}

func NewInMemoryAutogenClient() *InMemoryAutogenClient {
	return &InMemoryAutogenClient{
		sessions:           make(map[int]*autogen_client.Session),
		sessionsByLabel:    make(map[string]*autogen_client.Session),
		teams:              make(map[int]*autogen_client.Team),
		teamsByLabel:       make(map[string]*autogen_client.Team),
		runs:               make(map[int]*autogen_client.Run),
		runsByUUID:         make(map[uuid.UUID]*autogen_client.Run),
		toolServers:        make(map[int]*autogen_client.ToolServer),
		toolServersByLabel: make(map[string]*autogen_client.ToolServer),
		tools:              make(map[string]*autogen_client.Tool),
		toolsByServer:      make(map[int][]*autogen_client.Tool),
		feedback:           make([]*autogen_client.FeedbackSubmission, 0),
		runMessages:        make(map[uuid.UUID][]*autogen_client.RunMessage),
		nextSessionID:      1,
		nextTeamID:         1,
		nextRunID:          1,
		nextToolServerID:   1,
	}
}

// NewMockAutogenClient creates a new in-memory autogen client for backward compatibility
func NewMockAutogenClient() *InMemoryAutogenClient {
	return NewInMemoryAutogenClient()
}

func (m *InMemoryAutogenClient) CreateSession(req *autogen_client.CreateSession) (*autogen_client.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &autogen_client.Session{
		ID:     m.nextSessionID,
		Name:   req.Name,
		UserID: req.UserID,
	}

	m.sessions[session.ID] = session
	if session.Name != "" {
		m.sessionsByLabel[session.Name] = session
	}
	m.nextSessionID++

	return session, nil
}

func (m *InMemoryAutogenClient) CreateRun(req *autogen_client.CreateRunRequest) (*autogen_client.CreateRunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	runUUID := uuid.New()
	run := &autogen_client.Run{
		ID:        m.nextRunID,
		SessionID: req.SessionID,
	}

	m.runs[run.ID] = run
	m.runsByUUID[runUUID] = run
	m.nextRunID++

	return &autogen_client.CreateRunResult{
		ID: run.ID,
	}, nil
}

func (m *InMemoryAutogenClient) GetTeamByID(teamID int, userID string) (*autogen_client.Team, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	team, exists := m.teams[teamID]
	if !exists {
		return nil, fmt.Errorf("team with ID %d not found", teamID)
	}
	return team, nil
}

func (m *InMemoryAutogenClient) InvokeTask(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
	// For in-memory implementation, return a basic result
	return &autogen_client.InvokeTaskResult{
		TaskResult: autogen_client.TaskResult{
			Messages: []autogen_client.TaskMessageMap{
				{
					"role":    "assistant",
					"content": fmt.Sprintf("Task completed: %s", req.Task),
				},
			},
		},
	}, nil
}

func (m *InMemoryAutogenClient) GetSession(sessionLabel string, userID string) (*autogen_client.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessionsByLabel[sessionLabel]
	if !exists {
		return nil, autogen_client.NotFoundError
	}
	return session, nil
}

func (m *InMemoryAutogenClient) InvokeSession(sessionID int, userID string, request *autogen_client.InvokeRequest) (*autogen_client.TeamResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session with ID %d not found", sessionID)
	}

	return &autogen_client.TeamResult{
		TaskResult: autogen_client.TaskResult{
			Messages: []autogen_client.TaskMessageMap{
				{
					"role":    "assistant",
					"content": fmt.Sprintf("Session task completed: %s", request.Task),
				},
			},
		},
	}, nil
}

func (m *InMemoryAutogenClient) CreateFeedback(feedback *autogen_client.FeedbackSubmission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.feedback = append(m.feedback, feedback)
	return nil
}

func (m *InMemoryAutogenClient) CreateTeam(team *autogen_client.Team) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if team.Id == 0 {
		team.Id = m.nextTeamID
		m.nextTeamID++
	}

	m.teams[team.Id] = team
	if team.Component != nil && team.Component.Label != "" {
		m.teamsByLabel[team.Component.Label] = team
	}

	return nil
}

func (m *InMemoryAutogenClient) CreateToolServer(toolServer *autogen_client.ToolServer, userID string) (*autogen_client.ToolServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if toolServer.Id == 0 {
		toolServer.Id = m.nextToolServerID
		m.nextToolServerID++
	}

	m.toolServers[toolServer.Id] = toolServer
	if toolServer.Component.Label != "" {
		m.toolServersByLabel[toolServer.Component.Label] = toolServer
	}

	return toolServer, nil
}

func (m *InMemoryAutogenClient) DeleteRun(runID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	run, exists := m.runsByUUID[runID]
	if !exists {
		return fmt.Errorf("run with UUID %s not found", runID)
	}

	delete(m.runs, run.ID)
	delete(m.runsByUUID, runID)
	delete(m.runMessages, runID)

	return nil
}

func (m *InMemoryAutogenClient) DeleteSession(sessionID int, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session with ID %d not found", sessionID)
	}

	delete(m.sessions, sessionID)
	if session.Name != "" {
		delete(m.sessionsByLabel, session.Name)
	}

	return nil
}

func (m *InMemoryAutogenClient) DeleteTeam(teamID int, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	team, exists := m.teams[teamID]
	if !exists {
		return fmt.Errorf("team with ID %d not found", teamID)
	}

	delete(m.teams, teamID)
	if team.Component != nil && team.Component.Label != "" {
		delete(m.teamsByLabel, team.Component.Label)
	}

	return nil
}

func (m *InMemoryAutogenClient) DeleteToolServer(serverID *int, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if serverID == nil {
		return fmt.Errorf("server ID cannot be nil")
	}

	toolServer, exists := m.toolServers[*serverID]
	if !exists {
		return fmt.Errorf("tool server with ID %d not found", *serverID)
	}

	delete(m.toolServers, *serverID)
	if toolServer.Component.Label != "" {
		delete(m.toolServersByLabel, toolServer.Component.Label)
	}
	delete(m.toolsByServer, *serverID)

	return nil
}

func (m *InMemoryAutogenClient) GetRun(runID int) (*autogen_client.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	run, exists := m.runs[runID]
	if !exists {
		return nil, fmt.Errorf("run with ID %d not found", runID)
	}

	return run, nil
}

func (m *InMemoryAutogenClient) GetRunMessages(runID uuid.UUID) ([]*autogen_client.RunMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messages, exists := m.runMessages[runID]
	if !exists {
		return []*autogen_client.RunMessage{}, nil
	}

	return messages, nil
}

func (m *InMemoryAutogenClient) GetSessionById(sessionID int, userID string) (*autogen_client.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session with ID %d not found", sessionID)
	}

	return session, nil
}

func (m *InMemoryAutogenClient) GetTeam(teamLabel string, userID string) (*autogen_client.Team, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	team, exists := m.teamsByLabel[teamLabel]
	if !exists {
		return nil, fmt.Errorf("team with label %s not found", teamLabel)
	}

	return team, nil
}

func (m *InMemoryAutogenClient) GetTool(provider string, userID string) (*autogen_client.Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tool, exists := m.tools[provider]
	if !exists {
		return nil, fmt.Errorf("tool with provider %s not found", provider)
	}

	return tool, nil
}

func (m *InMemoryAutogenClient) GetToolServer(serverID int, userID string) (*autogen_client.ToolServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	toolServer, exists := m.toolServers[serverID]
	if !exists {
		return nil, fmt.Errorf("tool server with ID %d not found", serverID)
	}

	return toolServer, nil
}

func (m *InMemoryAutogenClient) GetToolServerByLabel(toolServerLabel string, userID string) (*autogen_client.ToolServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	toolServer, exists := m.toolServersByLabel[toolServerLabel]
	if !exists {
		return nil, fmt.Errorf("tool server with label %s not found", toolServerLabel)
	}

	return toolServer, nil
}

func (m *InMemoryAutogenClient) GetVersion(_ context.Context) (string, error) {
	return "1.0.0-inmemory", nil
}

func (m *InMemoryAutogenClient) InvokeSessionStream(sessionID int, userID string, request *autogen_client.InvokeRequest) (<-chan *autogen_client.SseEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session with ID %d not found", sessionID)
	}

	ch := make(chan *autogen_client.SseEvent, 1)
	go func() {
		defer close(ch)
		ch <- &autogen_client.SseEvent{
			Event: "message",
			Data:  []byte(fmt.Sprintf("Session stream task completed: %s", request.Task)),
		}
	}()

	return ch, nil
}

func (m *InMemoryAutogenClient) InvokeTaskStream(req *autogen_client.InvokeTaskRequest) (<-chan *autogen_client.SseEvent, error) {
	ch := make(chan *autogen_client.SseEvent, 1)
	go func() {
		defer close(ch)
		ch <- &autogen_client.SseEvent{
			Event: "message",
			Data:  []byte(fmt.Sprintf("Task stream completed: %s", req.Task)),
		}
	}()

	return ch, nil
}

func (m *InMemoryAutogenClient) ListFeedback(userID string) ([]*autogen_client.FeedbackSubmission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.feedback, nil
}

func (m *InMemoryAutogenClient) ListRuns(userID string) ([]*autogen_client.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runs := make([]*autogen_client.Run, 0, len(m.runs))
	for _, run := range m.runs {
		runs = append(runs, run)
	}

	return runs, nil
}

func (m *InMemoryAutogenClient) ListSessionRuns(sessionID int, userID string) ([]*autogen_client.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runs := make([]*autogen_client.Run, 0)
	for _, run := range m.runs {
		if run.SessionID == sessionID {
			runs = append(runs, run)
		}
	}

	return runs, nil
}

func (m *InMemoryAutogenClient) ListSessions(userID string) ([]*autogen_client.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*autogen_client.Session, 0)
	for _, session := range m.sessions {
		if session.UserID == userID {
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

func (m *InMemoryAutogenClient) ListSupportedModels() (*autogen_client.ProviderModels, error) {
	providerModels := autogen_client.ProviderModels{
		"openai": []autogen_client.ModelInfo{
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-3.5-turbo", FunctionCalling: true},
		},
		"azure": []autogen_client.ModelInfo{
			{Name: "gpt-4", FunctionCalling: true},
			{Name: "gpt-35-turbo", FunctionCalling: true},
		},
	}
	return &providerModels, nil
}

func (m *InMemoryAutogenClient) ListTeams(userID string) ([]*autogen_client.Team, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	teams := make([]*autogen_client.Team, 0)
	for _, team := range m.teams {
		if team.UserID == userID {
			teams = append(teams, team)
		}
	}

	return teams, nil
}

func (m *InMemoryAutogenClient) ListToolServers(userID string) ([]*autogen_client.ToolServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	toolServers := make([]*autogen_client.ToolServer, 0)
	for _, toolServer := range m.toolServers {
		toolServers = append(toolServers, toolServer)
	}

	return toolServers, nil
}

func (m *InMemoryAutogenClient) ListTools(userID string) ([]*autogen_client.Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := make([]*autogen_client.Tool, 0)
	for _, tool := range m.tools {
		tools = append(tools, tool)
	}

	return tools, nil
}

func (m *InMemoryAutogenClient) ListToolsForServer(serverID *int, userID string) ([]*autogen_client.Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if serverID == nil {
		return []*autogen_client.Tool{}, nil
	}

	tools, exists := m.toolsByServer[*serverID]
	if !exists {
		return []*autogen_client.Tool{}, nil
	}

	return tools, nil
}

func (m *InMemoryAutogenClient) RefreshToolServer(serverID int, userID string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.toolServers[serverID]
	if !exists {
		return fmt.Errorf("tool server with ID %d not found", serverID)
	}

	// In-memory implementation: refresh is a no-op
	return nil
}

func (m *InMemoryAutogenClient) RefreshTools(serverID *int, userID string) error {
	// In-memory implementation: refresh is a no-op
	return nil
}

func (m *InMemoryAutogenClient) UpdateSession(sessionID int, userID string, session *autogen_client.Session) (*autogen_client.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existingSession, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session with ID %d not found", sessionID)
	}

	// Remove old label mapping if it exists
	if existingSession.Name != "" {
		delete(m.sessionsByLabel, existingSession.Name)
	}

	// Update the session
	session.ID = sessionID
	m.sessions[sessionID] = session

	// Add new label mapping if it exists
	if session.Name != "" {
		m.sessionsByLabel[session.Name] = session
	}

	return session, nil
}

func (m *InMemoryAutogenClient) UpdateToolServer(server *autogen_client.ToolServer, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if server.Id == 0 {
		return fmt.Errorf("tool server ID cannot be zero")
	}

	existingServer, exists := m.toolServers[server.Id]
	if !exists {
		return fmt.Errorf("tool server with ID %d not found", server.Id)
	}

	// Remove old label mapping if it exists
	if existingServer.Component.Label != "" {
		delete(m.toolServersByLabel, existingServer.Component.Label)
	}

	// Update the tool server
	m.toolServers[server.Id] = server

	// Add new label mapping if it exists
	if server.Component.Label != "" {
		m.toolServersByLabel[server.Component.Label] = server
	}

	return nil
}

func (m *InMemoryAutogenClient) Validate(req *autogen_client.ValidationRequest) (*autogen_client.ValidationResponse, error) {
	return &autogen_client.ValidationResponse{
		IsValid:  true,
		Errors:   []*autogen_client.ValidationError{},
		Warnings: []*autogen_client.ValidationError{},
	}, nil
}

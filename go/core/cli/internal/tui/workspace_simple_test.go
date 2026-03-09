package tui

import (
	"net/http"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/core/cli/internal/config"
	"github.com/kagent-dev/kagent/go/core/cli/test/testutil"
)

func TestWorkspaceModel_Initialization(t *testing.T) {
	// Create mock HTTP server
	mockServer := testutil.NewMockHTTPServer(t,
		testutil.MockAgentResponse([]api.AgentResponse{}),
	)

	cfg := &config.Config{
		KAgentURL: mockServer.URL,
		Namespace: "kagent",
	}
	clientSet := cfg.Client()

	m := newWorkspaceModel(cfg, clientSet, false)

	// Verify initial state
	assert.NotNil(t, m)
	assert.Equal(t, focusChat, m.focus, "Initial focus should be chat")
	assert.False(t, m.naming, "Should not be naming initially")
	assert.False(t, m.choosingAgent, "Should not be choosing agent initially")
	assert.False(t, m.showDetails, "Details should be hidden initially")
	assert.Nil(t, m.agent, "No agent selected initially")
	assert.Nil(t, m.current, "No session selected initially")
}

func TestWorkspaceModel_FocusAreas(t *testing.T) {
	tests := []struct {
		name          string
		initialFocus  focusArea
		expectedValue int
	}{
		{
			name:          "focus sessions",
			initialFocus:  focusSessions,
			expectedValue: 0,
		},
		{
			name:          "focus chat",
			initialFocus:  focusChat,
			expectedValue: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedValue, int(tt.initialFocus))
		})
	}
}

func TestWorkspaceModel_LoadAgentsCommand(t *testing.T) {
	mockServer := testutil.NewMockHTTPServer(t,
		testutil.MockAgentResponse([]api.AgentResponse{
			{ID: "agent-1"},
			{ID: "agent-2"},
		}),
	)

	cfg := &config.Config{KAgentURL: mockServer.URL}
	clientSet := cfg.Client()
	m := newWorkspaceModel(cfg, clientSet, false)

	// Test the loadAgents command returns a valid command
	cmd := m.loadAgents()
	require.NotNil(t, cmd, "loadAgents should return a command")
}

func TestSessionListItem_Interface(t *testing.T) {
	sessionName := "test-session"
	sessionID := "sess-123"

	tests := []struct {
		name       string
		item       sessionListItem
		wantTitle  string
		wantDesc   string
		wantFilter string
	}{
		{
			name: "session with name",
			item: sessionListItem{
				s: &api.Session{
					ID:   sessionID,
					Name: &sessionName,
				},
			},
			wantTitle:  sessionName,
			wantDesc:   sessionID,
			wantFilter: sessionName,
		},
		{
			name: "session without name",
			item: sessionListItem{
				s: &api.Session{
					ID:   sessionID,
					Name: nil,
				},
			},
			wantTitle:  sessionID,
			wantDesc:   sessionID,
			wantFilter: sessionID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantTitle, tt.item.Title())
			assert.Equal(t, tt.wantDesc, tt.item.Description())
			assert.Equal(t, tt.wantFilter, tt.item.FilterValue())
		})
	}
}

func TestWorkspaceModel_WindowSizeMessage(t *testing.T) {
	mockServer := testutil.NewMockHTTPServer(t,
		testutil.MockAgentResponse([]api.AgentResponse{}),
	)

	cfg := &config.Config{KAgentURL: mockServer.URL}
	clientSet := cfg.Client()
	m := newWorkspaceModel(cfg, clientSet, false)

	// Send window size message
	updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	finalModel := updatedModel.(*workspaceModel)

	// Verify dimensions were updated
	assert.Equal(t, 120, finalModel.width)
	assert.Equal(t, 40, finalModel.height)
}

func TestWorkspaceModel_CreateSessionCommand(t *testing.T) {
	mockServer := testutil.NewMockHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})

	cfg := &config.Config{KAgentURL: mockServer.URL}
	clientSet := cfg.Client()
	m := newWorkspaceModel(cfg, clientSet, false)
	m.agentRef = "test-agent"

	// Test createSession command returns a valid command
	cmd := m.createSession("My Session")
	require.NotNil(t, cmd, "createSession should return a command")
}

func TestWorkspaceModel_LoadSessionsCommand(t *testing.T) {
	mockServer := testutil.NewMockHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": []}`))
	})

	cfg := &config.Config{KAgentURL: mockServer.URL}
	clientSet := cfg.Client()
	m := newWorkspaceModel(cfg, clientSet, false)
	m.agent = &api.AgentResponse{ID: "agent-1"}

	// Test loadSessions command returns a valid command
	cmd := m.loadSessions()
	require.NotNil(t, cmd, "loadSessions should return a command")
}

func TestWorkspaceModel_AgentsLoadedUpdate(t *testing.T) {
	mockServer := testutil.NewMockHTTPServer(t,
		testutil.MockAgentResponse([]api.AgentResponse{}),
	)

	cfg := &config.Config{KAgentURL: mockServer.URL}
	clientSet := cfg.Client()
	m := newWorkspaceModel(cfg, clientSet, false)

	// Simulate agents loaded message with valid agent data
	agents := []api.AgentResponse{
		{
			ID:    "agent-1",
			Agent: testutil.CreateTestAgent("default", "agent-1"),
		},
		{
			ID:    "agent-2",
			Agent: testutil.CreateTestAgent("default", "agent-2"),
		},
	}

	updatedModel, _ := m.Update(wsAgentsLoadedMsg{agents: agents, err: nil})
	finalModel := updatedModel.(*workspaceModel)

	// Verify agents were loaded
	assert.Len(t, finalModel.agents, 2)
}

func TestWorkspaceModel_InitCommand(t *testing.T) {
	mockServer := testutil.NewMockHTTPServer(t,
		testutil.MockAgentResponse([]api.AgentResponse{}),
	)

	cfg := &config.Config{KAgentURL: mockServer.URL}
	clientSet := cfg.Client()
	m := newWorkspaceModel(cfg, clientSet, false)

	// Test Init returns a command
	cmd := m.Init()
	assert.NotNil(t, cmd, "Init should return a command")
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database_fake "github.com/kagent-dev/kagent/go/internal/database/fake"
	"github.com/kagent-dev/kagent/go/internal/utils"
	dbpkg "github.com/kagent-dev/kagent/go/pkg/database"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func newTestHandler() (*MCPHandler, *database_fake.InMemoryFakeClient) {
	dbClient := database_fake.NewClient()
	fakeClient := dbClient.(*database_fake.InMemoryFakeClient)
	handler := &MCPHandler{
		dbClient:  dbClient,
		uiBaseURL: "http://test-ui.example.com",
		sendA2AMessageFunc: func(ctx context.Context, agentRef string, contextID *string, text string) error {
			return nil
		},
	}
	return handler, fakeClient
}

func storeTestAgent(t *testing.T, dbClient dbpkg.Client, agentRef string) *dbpkg.Agent {
	t.Helper()
	agent := &dbpkg.Agent{ID: utils.ConvertToPythonIdentifier(agentRef)}
	require.NoError(t, dbClient.StoreAgent(agent))
	return agent
}

func storeTestSession(t *testing.T, dbClient dbpkg.Client, sessionID, userID, agentID string) *dbpkg.Session {
	t.Helper()
	session := &dbpkg.Session{
		ID:      sessionID,
		UserID:  userID,
		AgentID: &agentID,
	}
	require.NoError(t, dbClient.StoreSession(session))
	return session
}

func makeEventData(t *testing.T, role protocol.MessageRole, text string) string {
	t.Helper()
	msg := protocol.Message{
		Kind:  protocol.KindMessage,
		Role:  role,
		Parts: []protocol.Part{protocol.NewTextPart(text)},
	}
	data, err := json.Marshal(msg)
	require.NoError(t, err)
	return string(data)
}

func TestHandleCreateSession(t *testing.T) {
	tests := []struct {
		name               string
		input              CreateSessionInput
		setupAgent         bool
		a2aError           error
		wantError          bool
		wantSessionID      bool
		wantURL            bool
		errorContains      string
		wantRetryHint      bool
	}{
		{
			name: "success with custom name",
			input: CreateSessionInput{
				Agent:  "test-ns/test-agent",
				Task:   "Do something",
				UserID: "user-1",
				Name:   "My Session",
			},
			setupAgent:    true,
			wantSessionID: true,
			wantURL:       true,
		},
		{
			name: "success with default name",
			input: CreateSessionInput{
				Agent:  "test-ns/test-agent",
				Task:   "Do something",
				UserID: "user-1",
			},
			setupAgent:    true,
			wantSessionID: true,
			wantURL:       true,
		},
		{
			name: "error invalid agent format",
			input: CreateSessionInput{
				Agent:  "no-slash",
				Task:   "Do something",
				UserID: "user-1",
			},
			wantError:     true,
			errorContains: "namespace/name",
		},
		{
			name: "defaults user_id when omitted",
			input: CreateSessionInput{
				Agent: "test-ns/test-agent",
				Task:  "Do something",
			},
			setupAgent:    true,
			wantSessionID: true,
			wantURL:       true,
		},
		{
			name: "error agent not found",
			input: CreateSessionInput{
				Agent:  "test-ns/missing-agent",
				Task:   "Do something",
				UserID: "user-1",
			},
			wantError:     true,
			errorContains: "Failed to find agent",
		},
		{
			name: "a2a dispatch failure returns error with retry hint",
			input: CreateSessionInput{
				Agent:  "test-ns/test-agent",
				Task:   "Do something",
				UserID: "user-1",
			},
			setupAgent:    true,
			a2aError:      fmt.Errorf("connection refused"),
			wantError:     true,
			wantSessionID: true,
			wantURL:       true,
			errorContains: "failed to dispatch",
			wantRetryHint: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newTestHandler()

			if tt.a2aError != nil {
				handler.sendA2AMessageFunc = func(ctx context.Context, agentRef string, contextID *string, text string) error {
					return tt.a2aError
				}
			}

			if tt.setupAgent {
				storeTestAgent(t, handler.dbClient, tt.input.Agent)
			}

			result, output, err := handler.handleCreateSession(context.Background(), &mcpsdk.CallToolRequest{}, tt.input)
			require.NoError(t, err, "handler should not return Go error")
			require.NotNil(t, result)

			if tt.wantError {
				assert.True(t, result.IsError)
				require.NotEmpty(t, result.Content)
				text := result.Content[0].(*mcpsdk.TextContent).Text
				assert.Contains(t, text, tt.errorContains)
				if tt.wantRetryHint {
					assert.Contains(t, text, "send_agent_session_message")
				}
			} else {
				assert.False(t, result.IsError)
				assert.Contains(t, output.Message, "Session created successfully")
			}

			if tt.wantSessionID {
				assert.NotEmpty(t, output.SessionID)
			}
			if tt.wantURL {
				assert.Contains(t, output.URL, "http://test-ui.example.com/agents/"+tt.input.Agent+"/chat/")
			}

			if !tt.wantSessionID {
				return
			}

			expectedUserID := tt.input.UserID
			if expectedUserID == "" {
				expectedUserID = defaultUserID
			}

			session, err := handler.dbClient.GetSession(output.SessionID, expectedUserID)
			require.NoError(t, err)
			assert.Equal(t, expectedUserID, session.UserID)
			assert.NotNil(t, session.AgentID)

			if tt.input.Name != "" {
				assert.Equal(t, tt.input.Name, *session.Name)
			} else {
				assert.Contains(t, *session.Name, "Session with")
			}
		})
	}
}

func TestHandleGetSessionEvents(t *testing.T) {
	tests := []struct {
		name       string
		input      GetSessionEventsInput
		setupFunc  func(t *testing.T, dbClient dbpkg.Client)
		wantEvents int
		wantText   string
	}{
		{
			name: "success with events",
			input: GetSessionEventsInput{
				SessionID: "session-1",
				UserID:    "user-1",
				Limit:     50,
			},
			setupFunc: func(t *testing.T, dbClient dbpkg.Client) {
				require.NoError(t, dbClient.StoreEvents(
					&dbpkg.Event{ID: "evt-1", SessionID: "session-1", UserID: "user-1", Data: makeEventData(t, protocol.MessageRoleUser, "hello")},
					&dbpkg.Event{ID: "evt-2", SessionID: "session-1", UserID: "user-1", Data: makeEventData(t, protocol.MessageRoleAgent, "hi there")},
				))
			},
			wantEvents: 2,
			wantText:   "[USER]",
		},
		{
			name: "success with no events",
			input: GetSessionEventsInput{
				SessionID: "empty-session",
				UserID:    "user-1",
			},
			wantEvents: 0,
			wantText:   "No events found",
		},
		{
			name: "default limit applied",
			input: GetSessionEventsInput{
				SessionID: "session-1",
				UserID:    "user-1",

			},
			setupFunc: func(t *testing.T, dbClient dbpkg.Client) {
				require.NoError(t, dbClient.StoreEvents(
					&dbpkg.Event{ID: "evt-1", SessionID: "session-1", UserID: "user-1", Data: makeEventData(t, protocol.MessageRoleUser, "msg")},
				))
			},
			wantEvents: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newTestHandler()

			if tt.setupFunc != nil {
				tt.setupFunc(t, handler.dbClient)
			}

			result, output, err := handler.handleGetSessionEvents(context.Background(), &mcpsdk.CallToolRequest{}, tt.input)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.IsError)

			assert.Len(t, output.Events, tt.wantEvents)

			require.NotEmpty(t, result.Content)
			text := result.Content[0].(*mcpsdk.TextContent).Text
			if tt.wantText != "" {
				assert.Contains(t, text, tt.wantText)
			}

			if tt.wantEvents >= 2 {
				assert.Equal(t, "user", output.Events[0].Role)
				assert.Equal(t, "agent", output.Events[1].Role)
			}
		})
	}
}

func TestHandleSendSessionMessage(t *testing.T) {
	tests := []struct {
		name          string
		input         SendSessionMessageInput
		setupFunc     func(t *testing.T, dbClient dbpkg.Client)
		a2aError      error
		wantError     bool
		errorContains string
	}{
		{
			name: "success",
			input: SendSessionMessageInput{
				SessionID: "session-1",
				UserID:    "user-1",
				Content:   "hello agent",
			},
			setupFunc: func(t *testing.T, dbClient dbpkg.Client) {
				storeTestSession(t, dbClient, "session-1", "user-1", "agent-1")
			},
		},
		{
			name: "defaults user_id when omitted",
			input: SendSessionMessageInput{
				SessionID: "session-default",
				Content:   "hello with default user",
			},
			setupFunc: func(t *testing.T, dbClient dbpkg.Client) {
				storeTestSession(t, dbClient, "session-default", defaultUserID, "agent-1")
			},
		},
		{
			name: "error session not found",
			input: SendSessionMessageInput{
				SessionID: "nonexistent",
				UserID:    "user-1",
				Content:   "hello",
			},
			wantError:     true,
			errorContains: "Failed to find session",
		},
		{
			name: "a2a dispatch failure returns error with no-resend warning",
			input: SendSessionMessageInput{
				SessionID: "session-1",
				UserID:    "user-1",
				Content:   "hello agent",
			},
			setupFunc: func(t *testing.T, dbClient dbpkg.Client) {
				storeTestSession(t, dbClient, "session-1", "user-1", "agent-1")
			},
			a2aError:      fmt.Errorf("connection refused"),
			wantError:     true,
			errorContains: "do NOT resend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newTestHandler()

			if tt.a2aError != nil {
				handler.sendA2AMessageFunc = func(ctx context.Context, agentRef string, contextID *string, text string) error {
					return tt.a2aError
				}
			}

			if tt.setupFunc != nil {
				tt.setupFunc(t, handler.dbClient)
			}

			result, output, err := handler.handleSendSessionMessage(context.Background(), &mcpsdk.CallToolRequest{}, tt.input)
			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.wantError {
				assert.True(t, result.IsError)
				require.NotEmpty(t, result.Content)
				text := result.Content[0].(*mcpsdk.TextContent).Text
				assert.Contains(t, text, tt.errorContains)
				return
			}

			assert.False(t, result.IsError)
			assert.Contains(t, output.Message, "Message sent successfully")
		})
	}
}

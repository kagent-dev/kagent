package a2a_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/a2a"
	"github.com/kagent-dev/kagent/go/internal/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/autogen/client/fake"
	"github.com/kagent-dev/kagent/go/internal/database"
	fake_db "github.com/kagent-dev/kagent/go/internal/database/fake"
	common "github.com/kagent-dev/kagent/go/internal/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// Helper function to create a mock autogen team with proper Component
func createMockAutogenTeam(id int, label string) *database.Agent {
	return &database.Agent{
		Component: api.Component{
			Provider:      "test.provider",
			ComponentType: "team",
			Version:       1,
			Description:   "Test team component",
			Label:         label,
			Config:        map[string]interface{}{},
		},
	}
}

func TestNewAutogenA2ATranslator(t *testing.T) {
	mockClient := fake.NewMockAutogenClient()
	baseURL := "http://localhost:8083"
	dbService := fake_db.NewClient()

	translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

	assert.NotNil(t, translator)
	assert.Implements(t, (*a2a.AutogenA2ATranslator)(nil), translator)
}

func TestTranslateHandlerForAgent(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should return handler params for valid agent with A2A config", func(t *testing.T) {
		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{
							ID:          "skill1",
							Name:        "Test Skill",
							Description: ptr.To("A test skill"),
						},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-namespace/test-agent", result.AgentCard.Name)
		assert.Equal(t, "Test agent", result.AgentCard.Description)
		assert.Equal(t, "http://localhost:8083/test-namespace/test-agent", result.AgentCard.URL)
		assert.Equal(t, "1", result.AgentCard.Version)
		assert.Equal(t, []string{"text"}, result.AgentCard.DefaultInputModes)
		assert.Equal(t, []string{"text"}, result.AgentCard.DefaultOutputModes)
		assert.Len(t, result.AgentCard.Skills, 1)
		assert.Equal(t, "skill1", result.AgentCard.Skills[0].ID)
		assert.NotNil(t, result.TaskHandler)
	})

	t.Run("should return nil for agent without A2A config", func(t *testing.T) {
		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-agent",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig:   nil,
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)

		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("should return error for agent with A2A config but no skills", func(t *testing.T) {
		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-agent",
				Namespace: "test-namespace",
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no skills found for agent test-namespace/test-agent")
		assert.Nil(t, result)
	})
}

func TestTaskHandlerWithSession(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should use existing session when session ID provided", func(t *testing.T) {
		sessionID := "test-session"
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()

		// Create a session in the in-memory client
		session := &database.Session{
			ID:      sessionID,
			UserID:  "admin@kagent.dev",
			AgentID: ptr.To(uint(1)),
		}
		err := dbService.CreateSession(session)
		require.NoError(t, err)
		assert.Equal(t, sessionID, session.ID)
		assert.Equal(t, "test-session", session.ID)

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		events, err := result.TaskHandler.HandleMessage(ctx, task, ptr.To(sessionID))
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Check that we got a TextMessage with the expected content
		textMsg, ok := events[0].(*autogen_client.TextMessage)
		require.True(t, ok, "Expected TextMessage event")
		assert.Equal(t, "Session task completed: test task", textMsg.Content)
		assert.Equal(t, "assistant", textMsg.Source)
	})

	t.Run("should create new session when session not found", func(t *testing.T) {
		sessionID := "new-session"
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()
		// Don't create any session - this will trigger the NotFound behavior

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler - this should create a new session and then invoke it
		events, err := result.TaskHandler.HandleMessage(ctx, task, ptr.To(sessionID))
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Check that we got a TextMessage with the expected content
		textMsg, ok := events[0].(*autogen_client.TextMessage)
		require.True(t, ok, "Expected TextMessage event")
		assert.Equal(t, "Session task completed: test task", textMsg.Content)

		// Verify the session was created
		createdSession, err := dbService.GetSession(sessionID, "admin@kagent.dev")
		require.NoError(t, err)
		assert.Equal(t, sessionID, createdSession.ID)
	})
}

func TestTaskHandlerWithoutSession(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should invoke task directly when no session ID provided", func(t *testing.T) {
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler without session ID
		events, err := result.TaskHandler.HandleMessage(ctx, task, nil)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Check that we got a TextMessage with the expected content
		textMsg, ok := events[0].(*autogen_client.TextMessage)
		require.True(t, ok, "Expected TextMessage event")
		assert.Equal(t, "Session task completed: test task", textMsg.Content)
		assert.Equal(t, "assistant", textMsg.Source)
	})

	t.Run("should invoke task directly when empty session ID provided", func(t *testing.T) {
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler with empty session ID
		events, err := result.TaskHandler.HandleMessage(ctx, task, nil)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Check that we got a TextMessage with the expected content
		textMsg, ok := events[0].(*autogen_client.TextMessage)
		require.True(t, ok, "Expected TextMessage event")
		assert.Equal(t, "Session task completed: test task", textMsg.Content)
	})
}

func TestTaskHandlerMessageContentExtraction(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should extract string content from TextMessage events", func(t *testing.T) {
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		events, err := result.TaskHandler.HandleMessage(ctx, task, nil)
		require.NoError(t, err)
		require.Len(t, events, 1)

		// Test that GetLastStringMessage works correctly
		lastString := autogen_client.GetLastStringMessage(events)
		assert.Equal(t, "Session task completed: test task", lastString)
	})

	t.Run("should handle empty event list", func(t *testing.T) {
		// Test that GetLastStringMessage handles empty list gracefully
		lastString := autogen_client.GetLastStringMessage([]autogen_client.Event{})
		assert.Equal(t, "", lastString)
	})
}

func TestTaskHandlerStreamingSupport(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should support streaming without session", func(t *testing.T) {
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test streaming
		eventChan, err := result.TaskHandler.HandleMessageStream(ctx, task, nil)
		require.NoError(t, err)
		require.NotNil(t, eventChan)

		// Collect events from channel
		var events []autogen_client.Event
		for event := range eventChan {
			events = append(events, event)
		}

		require.Len(t, events, 1)
		textMsg, ok := events[0].(*autogen_client.TextMessage)
		require.True(t, ok, "Expected TextMessage event")
		assert.Equal(t, "Session task completed: test task", textMsg.Content)
	})

	t.Run("should support streaming with session", func(t *testing.T) {
		sessionID := "test-session"
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		dbService := fake_db.NewClient()

		// Create a session
		err := dbService.CreateSession(&database.Session{
			ID:     sessionID,
			UserID: "admin@kagent.dev",
		})
		require.NoError(t, err)

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient, dbService)

		agent := &v1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-agent",
				Namespace:  "test-namespace",
				Generation: 1,
			},
			Spec: v1alpha1.AgentSpec{
				Description: "Test agent",
				A2AConfig: &v1alpha1.A2AConfig{
					Skills: []v1alpha1.AgentSkill{
						{ID: "skill1", Name: "Test Skill"},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, common.GetObjectRef(agent))

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test streaming with session
		eventChan, err := result.TaskHandler.HandleMessageStream(ctx, task, ptr.To(sessionID))
		require.NoError(t, err)
		require.NotNil(t, eventChan)

		// Collect events from channel
		var events []autogen_client.Event
		for event := range eventChan {
			events = append(events, event)
		}

		require.Len(t, events, 1)
		textMsg, ok := events[0].(*autogen_client.TextMessage)
		require.True(t, ok, "Expected TextMessage event")
		assert.Equal(t, "Session task completed: test task", textMsg.Content)
	})
}

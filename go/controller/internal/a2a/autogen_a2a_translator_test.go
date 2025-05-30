package a2a_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/autogen/api"
	autogen_client "github.com/kagent-dev/kagent/go/autogen/client"
	"github.com/kagent-dev/kagent/go/autogen/client/fake"
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/controller/internal/a2a"
	common "github.com/kagent-dev/kagent/go/controller/internal/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Helper function to create a mock autogen team with proper Component
func createMockAutogenTeam(id int, label string) *autogen_client.Team {
	return &autogen_client.Team{
		BaseObject: autogen_client.BaseObject{
			Id: id,
		},
		Component: &api.Component{
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

	translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

	assert.NotNil(t, translator)
	assert.Implements(t, (*a2a.AutogenA2ATranslator)(nil), translator)
}

func TestTranslateHandlerForAgent(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should return handler params for valid agent with A2A config", func(t *testing.T) {
		mockClient := fake.NewMockAutogenClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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
							Description: common.MakePtr("A test skill"),
						},
					},
				},
			},
		}

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-agent", result.AgentCard.Name)
		assert.Equal(t, "Test agent", *result.AgentCard.Description)
		assert.Equal(t, "http://localhost:8083/test-namespace/test-agent", result.AgentCard.URL)
		assert.Equal(t, "1", result.AgentCard.Version)
		assert.Equal(t, []string{"text"}, result.AgentCard.DefaultInputModes)
		assert.Equal(t, []string{"text"}, result.AgentCard.DefaultOutputModes)
		assert.Len(t, result.AgentCard.Skills, 1)
		assert.Equal(t, "skill1", result.AgentCard.Skills[0].ID)
		assert.NotNil(t, result.HandleTask)
	})

	t.Run("should return nil for agent without A2A config", func(t *testing.T) {
		mockClient := fake.NewMockAutogenClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)

		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("should return error for agent with A2A config but no skills", func(t *testing.T) {
		mockClient := fake.NewMockAutogenClient()
		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no skills found for agent test-agent")
		assert.Nil(t, result)
	})
}

func TestTaskHandlerWithSession(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should use existing session when session ID provided", func(t *testing.T) {
		sessionID := "test-session"
		task := "test task"
		expectedResult := "test result"

		mockClient := fake.NewMockAutogenClient()
		mockClient.GetSessionFunc = func(sessionLabel string, userID string) (*autogen_client.Session, error) {
			assert.Equal(t, sessionID, sessionLabel)
			return &autogen_client.Session{
				ID: 456,
			}, nil
		}
		mockClient.InvokeSessionFunc = func(sessID int, userID string, taskText string) (*autogen_client.TeamResult, error) {
			assert.Equal(t, 456, sessID)
			assert.Equal(t, task, taskText)
			return &autogen_client.TeamResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{
						{"content": expectedResult},
					},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		handlerResult, err := result.HandleTask(ctx, task, &sessionID)
		require.NoError(t, err)
		assert.Equal(t, expectedResult, handlerResult)
	})

	t.Run("should create new session when session not found", func(t *testing.T) {
		sessionID := "new-session"
		task := "test task"
		expectedResult := "test result"

		mockClient := fake.NewMockAutogenClient()
		mockClient.GetSessionFunc = func(sessionLabel string, userID string) (*autogen_client.Session, error) {
			return nil, autogen_client.NotFoundError
		}
		mockClient.CreateSessionFunc = func(session *autogen_client.CreateSession) (*autogen_client.Session, error) {
			assert.Equal(t, sessionID, session.Name)
			assert.Equal(t, 123, session.TeamID)
			return &autogen_client.Session{
				ID: 789,
			}, nil
		}
		mockClient.InvokeSessionFunc = func(sessID int, userID string, taskText string) (*autogen_client.TeamResult, error) {
			assert.Equal(t, 789, sessID)
			return &autogen_client.TeamResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{
						{"content": expectedResult},
					},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		handlerResult, err := result.HandleTask(ctx, task, &sessionID)
		require.NoError(t, err)
		assert.Equal(t, expectedResult, handlerResult)
	})

	t.Run("should handle error when creating new session fails", func(t *testing.T) {
		sessionID := "new-session"
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		mockClient.GetSessionFunc = func(sessionLabel string, userID string) (*autogen_client.Session, error) {
			return nil, autogen_client.NotFoundError
		}
		mockClient.CreateSessionFunc = func(session *autogen_client.CreateSession) (*autogen_client.Session, error) {
			return nil, errors.New("failed to create session")
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		_, err = result.HandleTask(ctx, task, &sessionID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create session")
	})
}

func TestTaskHandlerWithoutSession(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should invoke task directly when no session ID provided", func(t *testing.T) {
		task := "test task"
		expectedResult := "test result"

		mockClient := fake.NewMockAutogenClient()
		mockClient.InvokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			assert.Equal(t, task, req.Task)
			assert.NotNil(t, req.TeamConfig)
			return &autogen_client.InvokeTaskResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{
						{"content": expectedResult},
					},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler without session ID
		handlerResult, err := result.HandleTask(ctx, task, nil)
		require.NoError(t, err)
		assert.Equal(t, expectedResult, handlerResult)
	})

	t.Run("should invoke task directly when empty session ID provided", func(t *testing.T) {
		task := "test task"
		expectedResult := "test result"
		emptySessionID := ""

		mockClient := fake.NewMockAutogenClient()
		mockClient.InvokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			assert.Equal(t, task, req.Task)
			return &autogen_client.InvokeTaskResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{
						{"content": expectedResult},
					},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler with empty session ID
		handlerResult, err := result.HandleTask(ctx, task, &emptySessionID)
		require.NoError(t, err)
		assert.Equal(t, expectedResult, handlerResult)
	})
}

func TestTaskHandlerMessageContentExtraction(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should extract string content from messages", func(t *testing.T) {
		task := "test task"
		expectedResult := "final result"

		mockClient := fake.NewMockAutogenClient()
		mockClient.InvokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			return &autogen_client.InvokeTaskResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{
						{"content": "first message"},
						{"content": "second message"},
						{"content": expectedResult},
					},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		handlerResult, err := result.HandleTask(ctx, task, nil)
		require.NoError(t, err)
		assert.Equal(t, expectedResult, handlerResult)
	})

	t.Run("should marshal non-string content from messages", func(t *testing.T) {
		task := "test task"
		complexContent := map[string]interface{}{
			"type": "complex",
			"data": []string{"item1", "item2"},
		}

		mockClient := fake.NewMockAutogenClient()
		mockClient.InvokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			return &autogen_client.InvokeTaskResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{
						{"content": complexContent},
					},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		handlerResult, err := result.HandleTask(ctx, task, nil)
		require.NoError(t, err)

		// Verify the result is valid JSON
		var parsedResult map[string]interface{}
		err = json.Unmarshal([]byte(handlerResult), &parsedResult)
		require.NoError(t, err)
		assert.Equal(t, "complex", parsedResult["type"])
		assert.Equal(t, []interface{}{"item1", "item2"}, parsedResult["data"])
	})

	t.Run("should handle empty messages", func(t *testing.T) {
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		mockClient.InvokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			return &autogen_client.InvokeTaskResult{
				TaskResult: autogen_client.TaskResult{
					Messages: []autogen_client.TaskMessageMap{},
				},
			}, nil
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		handlerResult, err := result.HandleTask(ctx, task, nil)
		require.NoError(t, err)
		assert.Equal(t, "", handlerResult)
	})
}

func TestTaskHandlerErrorHandling(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:8083"

	t.Run("should handle invoke task error", func(t *testing.T) {
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		mockClient.InvokeTaskFunc = func(req *autogen_client.InvokeTaskRequest) (*autogen_client.InvokeTaskResult, error) {
			return nil, errors.New("invoke task failed")
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		_, err = result.HandleTask(ctx, task, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to invoke task")
	})

	t.Run("should handle invoke session error", func(t *testing.T) {
		sessionID := "test-session"
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		mockClient.GetSessionFunc = func(sessionLabel string, userID string) (*autogen_client.Session, error) {
			return &autogen_client.Session{ID: 456}, nil
		}
		mockClient.InvokeSessionFunc = func(sessID int, userID string, taskText string) (*autogen_client.TeamResult, error) {
			return nil, errors.New("invoke session failed")
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		_, err = result.HandleTask(ctx, task, &sessionID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to invoke task")
	})

	t.Run("should handle get session error (not NotFoundError)", func(t *testing.T) {
		sessionID := "test-session"
		task := "test task"

		mockClient := fake.NewMockAutogenClient()
		mockClient.GetSessionFunc = func(sessionLabel string, userID string) (*autogen_client.Session, error) {
			return nil, errors.New("unexpected session error")
		}

		translator := a2a.NewAutogenA2ATranslator(baseURL, mockClient)

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

		autogenTeam := createMockAutogenTeam(123, "test-team")

		result, err := translator.TranslateHandlerForAgent(ctx, agent, autogenTeam)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Test the handler
		_, err = result.HandleTask(ctx, task, &sessionID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get session")
	})
}

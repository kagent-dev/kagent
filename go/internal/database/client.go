package database

import (
	"encoding/json"
	"fmt"
	"slices"

	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/utils"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type Client interface {
	CreateFeedback(feedback *Feedback) error
	CreateSession(session *Session) error
	CreateAgent(agent *Agent) error
	CreateToolServer(toolServer *ToolServer) (*ToolServer, error)
	CreateMessages(messages ...*protocol.Message) error
	CreateTask(task *protocol.Task) error
	CreatePushNotification(taskID string, config *protocol.TaskPushNotificationConfig) error

	UpsertAgent(agent *Agent) error

	RefreshToolsForServer(serverName string, tools []*autogen_client.NamedTool) error

	DeleteSession(sessionName string, userID string) error
	DeleteAgent(agentName string) error
	DeleteToolServer(serverName string) error
	DeleteTask(taskID string) error

	UpdateSession(session *Session) error
	UpdateToolServer(server *ToolServer) error
	UpdateAgent(agent *Agent) error
	UpdateTask(task *Task) error

	GetSession(name string, userID string) (*Session, error)
	GetAgent(name string) (*Agent, error)
	GetTool(name string) (*Tool, error)
	GetToolServer(name string) (*ToolServer, error)
	GetTask(taskID string) (*Task, error)
	GetMessage(messageID string) (*Message, error)
	GetPushNotification(taskID string) (*protocol.TaskPushNotificationConfig, error)

	ListTools() ([]Tool, error)
	ListFeedback(userID string) ([]Feedback, error)
	ListSessionTasks(sessionID string, userID string) ([]Task, error)
	ListSessions(userID string) ([]Session, error)
	ListSessionsForAgent(agentID uint, userID string) ([]Session, error)
	ListAgents() ([]Agent, error)
	ListToolServers() ([]ToolServer, error)
	ListToolsForServer(serverName string) ([]Tool, error)
	ListMessagesForTask(taskID, userID string) ([]Message, error)
	ListMessagesForSession(sessionID, userID string) ([]Message, error)
}

type clientImpl struct {
	db *gorm.DB
}

func NewClient(dbManager *Manager) Client {
	return &clientImpl{
		db: dbManager.db,
	}
}

// CreateFeedback creates a new feedback record
func (c *clientImpl) CreateFeedback(feedback *Feedback) error {
	return create(c.db, feedback)
}

// CreateSession creates a new session record
func (c *clientImpl) CreateSession(session *Session) error {
	return create(c.db, session)
}

// CreateAgent creates a new agent record
func (c *clientImpl) CreateAgent(agent *Agent) error {
	return create(c.db, agent)
}

// CreateTask creates a new task record
func (c *clientImpl) CreateTask(task *protocol.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to serialize task: %w", err)
	}

	return create(c.db, &Task{
		ID:        task.ID,
		SessionID: task.ContextID,
		UserID:    utils.GetGlobalUserID(),
		Data:      string(data),
	})
}

func (c *clientImpl) CreatePushNotification(taskID string, config *protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification config: %w", err)
	}

	return create(c.db, &PushNotification{
		TaskID: taskID,
		Data:   string(data),
	})
}

// UpsertAgent upserts an agent record
func (c *clientImpl) UpsertAgent(agent *Agent) error {
	return upsert(c.db, agent)
}

// CreateToolServer creates a new tool server record
func (c *clientImpl) CreateToolServer(toolServer *ToolServer) (*ToolServer, error) {
	err := create(c.db, toolServer)
	if err != nil {
		return nil, err
	}
	return toolServer, nil
}

// CreateTool creates a new tool record
func (c *clientImpl) CreateTool(tool *Tool) error {
	return create(c.db, tool)
}

// DeleteTask deletes a task by ID
func (c *clientImpl) DeleteTask(taskID string) error {
	return delete[Task](c.db, Clause{Key: "id", Value: taskID})
}

// DeleteSession deletes a session by name and user ID
func (c *clientImpl) DeleteSession(sessionName string, userID string) error {
	return delete[Session](c.db,
		Clause{Key: "name", Value: sessionName},
		Clause{Key: "user_id", Value: userID})
}

// DeleteAgent deletes an agent by name and user ID
func (c *clientImpl) DeleteAgent(agentName string) error {
	return delete[Agent](c.db, Clause{Key: "name", Value: agentName})
}

// DeleteToolServer deletes a tool server by name and user ID
func (c *clientImpl) DeleteToolServer(serverName string) error {
	return delete[ToolServer](c.db, Clause{Key: "name", Value: serverName})
}

func (c *clientImpl) GetTask(taskID string) (*Task, error) {
	return get[Task](c.db, Clause{Key: "id", Value: taskID})
}

// GetTaskMessages retrieves messages for a specific task
func (c *clientImpl) GetTaskMessages(taskID int) ([]Message, error) {
	messages, err := list[Message](c.db, Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// GetSession retrieves a session by name and user ID
func (c *clientImpl) GetSession(sessionName string, userID string) (*Session, error) {
	return get[Session](c.db,
		Clause{Key: "id", Value: sessionName},
		Clause{Key: "user_id", Value: userID})
}

// GetAgent retrieves an agent by name and user ID
func (c *clientImpl) GetAgent(agentName string) (*Agent, error) {
	return get[Agent](c.db, Clause{Key: "name", Value: agentName})
}

// GetTool retrieves a tool by provider (name) and user ID
func (c *clientImpl) GetTool(provider string) (*Tool, error) {
	return get[Tool](c.db, Clause{Key: "name", Value: provider})
}

// GetToolServer retrieves a tool server by name and user ID
func (c *clientImpl) GetToolServer(serverName string) (*ToolServer, error) {
	return get[ToolServer](c.db, Clause{Key: "name", Value: serverName})
}

// ListFeedback lists all feedback for a user
func (c *clientImpl) ListFeedback(userID string) ([]Feedback, error) {
	feedback, err := list[Feedback](c.db, Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return feedback, nil
}

func (c *clientImpl) CreateMessages(messages ...*protocol.Message) error {
	for _, message := range messages {
		if message == nil {
			continue
		}

		data, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("failed to serialize message: %w", err)
		}

		dbMessage := Message{
			ID:        message.MessageID,
			Data:      string(data),
			SessionID: message.ContextID,
			TaskID:    message.TaskID,
			UserID:    utils.GetGlobalUserID(),
		}

		err = create(c.db, &dbMessage)
		if err != nil {
			return fmt.Errorf("failed to create message: %w", err)
		}
	}
	return nil
}

// ListRuns lists all runs for a user
func (c *clientImpl) ListTasks(userID string) ([]Task, error) {
	tasks, err := list[Task](c.db, Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

// ListSessionRuns lists all runs for a specific session
func (c *clientImpl) ListSessionTasks(sessionID string, userID string) ([]Task, error) {
	return list[Task](c.db,
		Clause{Key: "session_id", Value: sessionID},
		Clause{Key: "user_id", Value: userID})
}

func (c *clientImpl) ListSessionsForAgent(agentID uint, userID string) ([]Session, error) {
	return list[Session](c.db,
		Clause{Key: "agent_id", Value: agentID},
		Clause{Key: "user_id", Value: userID})
}

// ListSessions lists all sessions for a user
func (c *clientImpl) ListSessions(userID string) ([]Session, error) {
	return list[Session](c.db, Clause{Key: "user_id", Value: userID})
}

// ListAgents lists all agents
func (c *clientImpl) ListAgents() ([]Agent, error) {
	return list[Agent](c.db)
}

// ListToolServers lists all tool servers for a user
func (c *clientImpl) ListToolServers() ([]ToolServer, error) {
	return list[ToolServer](c.db)
}

// ListTools lists all tools for a user
func (c *clientImpl) ListTools() ([]Tool, error) {
	return list[Tool](c.db)
}

// ListToolsForServer lists all tools for a specific server
func (c *clientImpl) ListToolsForServer(serverName string) ([]Tool, error) {
	return list[Tool](c.db, Clause{Key: "server_name", Value: serverName})
}

// RefreshToolsForServer refreshes a tool server
func (c *clientImpl) RefreshToolsForServer(serverName string, tools []*autogen_client.NamedTool) error {
	existingTools, err := c.ListToolsForServer(serverName)
	if err != nil {
		return err
	}

	// Check if the tool exists in the existing tools
	// If it does, update it
	// If it doesn't, create it
	// If it's in the existing tools but not in the new tools, delete it
	for _, tool := range tools {
		existingToolIndex := slices.IndexFunc(existingTools, func(t Tool) bool {
			return t.Name == tool.Name
		})
		if existingToolIndex != -1 {
			existingTool := existingTools[existingToolIndex]
			existingTool.Component = *tool.Component
			existingTool.ServerName = serverName
			err = upsert(c.db, &existingTool)
			if err != nil {
				return err
			}
		} else {
			err = create(c.db, &Tool{
				Name:       tool.Name,
				Component:  *tool.Component,
				ServerName: serverName,
			})
			if err != nil {
				return fmt.Errorf("failed to create tool %s: %v", tool.Name, err)
			}
		}
	}

	// Delete any tools that are in the existing tools but not in the new tools
	for _, existingTool := range existingTools {
		if !slices.ContainsFunc(tools, func(t *autogen_client.NamedTool) bool {
			return t.Name == existingTool.Name
		}) {
			err = delete[Tool](c.db, Clause{Key: "name", Value: existingTool.Name})
			if err != nil {
				return fmt.Errorf("failed to delete tool %s: %v", existingTool.Name, err)
			}
		}
	}
	return nil
}

// UpdateSession updates a session
func (c *clientImpl) UpdateSession(session *Session) error {
	return upsert(c.db, session)
}

// UpdateToolServer updates a tool server
func (c *clientImpl) UpdateToolServer(server *ToolServer) error {
	return upsert(c.db, server)
}

// UpdateTask updates a task record
func (c *clientImpl) UpdateTask(task *Task) error {
	return upsert(c.db, task)
}

// UpdateAgent updates an agent record
func (c *clientImpl) UpdateAgent(agent *Agent) error {
	return upsert(c.db, agent)
}

// ListMessagesForRun retrieves messages for a specific run (helper method)
func (c *clientImpl) ListMessagesForTask(taskID, userID string) ([]Message, error) {
	return list[Message](c.db,
		Clause{Key: "task_id", Value: taskID},
		Clause{Key: "user_id", Value: userID})
}

func (c *clientImpl) ListMessagesForSession(sessionID, userID string) ([]Message, error) {
	return list[Message](c.db,
		Clause{Key: "session_id", Value: sessionID},
		Clause{Key: "user_id", Value: userID})
}

// GetMessage retrieves a protocol message from the database
func (c *clientImpl) GetMessage(messageID string) (*Message, error) {
	dbMessage, err := get[Message](c.db, Clause{Key: "id", Value: messageID})
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	return dbMessage, nil
}

func (c *clientImpl) GetPushNotification(taskID string) (*protocol.TaskPushNotificationConfig, error) {
	dbPushNotification, err := get[PushNotification](c.db, Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to get push notification config: %w", err)
	}
	var config protocol.TaskPushNotificationConfig
	if err := json.Unmarshal([]byte(dbPushNotification.Data), &config); err != nil {
		return nil, fmt.Errorf("failed to deserialize push notification config: %w", err)
	}
	return &config, nil
}

// DeleteMessage deletes a protocol message from the database
func (c *clientImpl) DeleteMessage(messageID string) error {
	return delete[Message](c.db, Clause{Key: "id", Value: messageID})
}

// ListMessagesByContextID retrieves messages by context ID with optional limit
func (c *clientImpl) ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error) {
	var dbMessages []Message
	query := c.db.Where("session_id = ?", contextID).Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&dbMessages).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	protocolMessages := make([]protocol.Message, 0, len(dbMessages))
	for _, dbMessage := range dbMessages {
		var protocolMessage protocol.Message
		if err := json.Unmarshal([]byte(dbMessage.Data), &protocolMessage); err != nil {
			return nil, fmt.Errorf("failed to deserialize message: %w", err)
		}
		protocolMessages = append(protocolMessages, protocolMessage)
	}

	return protocolMessages, nil
}

// StorePushNotification stores a push notification configuration in the database
func (c *clientImpl) StorePushNotification(taskID string, config protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification config: %w", err)
	}

	dbPushNotification := PushNotification{
		TaskID: taskID,
		Data:   string(data),
	}

	return upsert(c.db, &dbPushNotification)
}

// DeletePushNotification deletes a push notification configuration from the database
func (c *clientImpl) DeletePushNotification(taskID string) error {
	return delete[PushNotification](c.db, Clause{Key: "task_id", Value: taskID})
}

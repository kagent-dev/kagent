package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type Client interface {
	// Store methods
	StoreFeedback(feedback *Feedback) error
	StoreSession(session *Session) error
	StoreAgent(agent *Agent) error
	StoreTask(task *protocol.Task) error
	StorePushNotification(config *protocol.TaskPushNotificationConfig) error
	StoreToolServer(toolServer *ToolServer) (*ToolServer, error)
	StoreEvents(messages ...*Event) error

	// Delete methods
	DeleteSession(sessionName string, userID string) error
	DeleteAgent(agentID string) error
	DeleteToolServer(serverName string, groupKind string) error
	DeleteTask(taskID string) error
	DeletePushNotification(taskID string) error
	DeleteToolsForServer(serverName string, groupKind string) error

	// Get methods
	GetSession(name string, userID string) (*Session, error)
	GetAgent(name string) (*Agent, error)
	GetTask(id string) (*protocol.Task, error)
	GetTool(name string) (*Tool, error)
	GetToolServer(name string) (*ToolServer, error)
	GetPushNotification(taskID string, configID string) (*protocol.TaskPushNotificationConfig, error)

	// List methods
	ListTools() ([]Tool, error)
	ListFeedback(userID string) ([]Feedback, error)
	ListTasksForSession(sessionID string) ([]*protocol.Task, error)
	ListSessions(userID string) ([]Session, error)
	ListSessionsForAgent(agentID string, userID string) ([]Session, error)
	ListAgents() ([]Agent, error)
	ListToolServers() ([]ToolServer, error)
	ListToolsForServer(serverName string, groupKind string) ([]Tool, error)
	ListEventsForSession(sessionID, userID string, options QueryOptions) ([]*Event, error)
	ListPushNotifications(taskID string) ([]*protocol.TaskPushNotificationConfig, error)

	// Helper methods
	RefreshToolsForServer(serverName string, groupKind string, tools ...*v1alpha2.MCPTool) error

	// LangGraph Checkpoint methods
	StoreCheckpoint(checkpoint *LangGraphCheckpoint) error
	StoreCheckpointWrites(writes []*LangGraphCheckpointWrite) error
	ListCheckpoints(userID, threadID, checkpointNS string, checkpointID *string, limit int) ([]*LangGraphCheckpointTuple, error)
	DeleteCheckpoint(userID, threadID string) error

	// CrewAI methods
	StoreCrewAIMemory(memory *CrewAIAgentMemory) error
	SearchCrewAIMemoryByTask(userID, threadID, taskDescription string, limit int) ([]*CrewAIAgentMemory, error)
	ResetCrewAIMemory(userID, threadID string) error
	StoreCrewAIFlowState(state *CrewAIFlowState) error
	GetCrewAIFlowState(userID, threadID string) (*CrewAIFlowState, error)

	// SearchAgentMemory methods
	StoreAgentMemory(memory *Memory) error
	StoreAgentMemories(memories []*Memory) error
	SearchAgentMemory(agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error)
	DeleteAgentMemory(agentName, userID string) error
	PruneExpiredMemories() error
}

type AgentMemorySearchResult struct {
	Memory
	Score float64
}

type LangGraphCheckpointTuple struct {
	Checkpoint *LangGraphCheckpoint
	Writes     []*LangGraphCheckpointWrite
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
func (c *clientImpl) StoreFeedback(feedback *Feedback) error {
	return save(c.db, feedback)
}

// CreateSession creates a new session record
func (c *clientImpl) StoreSession(session *Session) error {
	return save(c.db, session)
}

// CreateAgent creates a new agent record
func (c *clientImpl) StoreAgent(agent *Agent) error {
	return save(c.db, agent)
}

func (c *clientImpl) CreatePushNotification(taskID string, config *protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification config: %w", err)
	}

	dbPushNotification := PushNotification{
		TaskID: taskID,
		Data:   string(data),
	}

	return save(c.db, &dbPushNotification)
}

// CreateToolServer creates a new tool server record
func (c *clientImpl) StoreToolServer(toolServer *ToolServer) (*ToolServer, error) {
	err := save(c.db, toolServer)
	if err != nil {
		return nil, err
	}
	return toolServer, nil
}

// CreateTool creates a new tool record
func (c *clientImpl) StoreTool(tool *Tool) error {
	return save(c.db, tool)
}

// DeleteTask deletes a task by ID
func (c *clientImpl) DeleteTask(taskID string) error {
	return delete[Task](c.db, Clause{Key: "id", Value: taskID})
}

// DeleteSession deletes a session by id and user ID
func (c *clientImpl) DeleteSession(sessionName string, userID string) error {
	return delete[Session](c.db,
		Clause{Key: "id", Value: sessionName},
		Clause{Key: "user_id", Value: userID})
}

// DeleteAgent deletes an agent by name and user ID
func (c *clientImpl) DeleteAgent(agentID string) error {
	return delete[Agent](c.db, Clause{Key: "id", Value: agentID})
}

// DeleteToolServer deletes a tool server by name and user ID
func (c *clientImpl) DeleteToolServer(serverName string, groupKind string) error {
	return delete[ToolServer](c.db,
		Clause{Key: "name", Value: serverName},
		Clause{Key: "group_kind", Value: groupKind})
}

func (c *clientImpl) DeleteToolsForServer(serverName string, groupKind string) error {
	return delete[Tool](c.db,
		Clause{Key: "server_name", Value: serverName},
		Clause{Key: "group_kind", Value: groupKind})
}

// GetTaskMessages retrieves messages for a specific task
func (c *clientImpl) GetTaskMessages(taskID int) ([]*protocol.Message, error) {
	messages, err := list[Event](c.db, Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, err
	}

	protocolMessages := make([]*protocol.Message, 0, len(messages))
	for _, message := range messages {
		var protocolMessage protocol.Message
		if err := json.Unmarshal([]byte(message.Data), &protocolMessage); err != nil {
			return nil, fmt.Errorf("failed to deserialize message: %w", err)
		}
		protocolMessages = append(protocolMessages, &protocolMessage)
	}

	return protocolMessages, nil
}

// GetSession retrieves a session by name and user ID
func (c *clientImpl) GetSession(sessionName string, userID string) (*Session, error) {
	return get[Session](c.db,
		Clause{Key: "id", Value: sessionName},
		Clause{Key: "user_id", Value: userID})
}

// GetAgent retrieves an agent by name and user ID
func (c *clientImpl) GetAgent(agentID string) (*Agent, error) {
	return get[Agent](c.db, Clause{Key: "id", Value: agentID})
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

func (c *clientImpl) StoreEvents(events ...*Event) error {
	for _, event := range events {
		err := save(c.db, event)
		if err != nil {
			return fmt.Errorf("failed to create event: %w", err)
		}
	}
	return nil
}

// ListSessionRuns lists all runs for a specific session
func (c *clientImpl) ListTasksForSession(sessionID string) ([]*protocol.Task, error) {
	tasks, err := list[Task](c.db,
		Clause{Key: "session_id", Value: sessionID},
	)
	if err != nil {
		return nil, err
	}

	return ParseTasks(tasks)
}

func (c *clientImpl) ListSessionsForAgent(agentID string, userID string) ([]Session, error) {
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

// ListToolsForServer lists all tools for a specific server and group kind
func (c *clientImpl) ListToolsForServer(serverName string, groupKind string) ([]Tool, error) {
	return list[Tool](c.db,
		Clause{Key: "server_name", Value: serverName},
		Clause{Key: "group_kind", Value: groupKind})
}

// RefreshToolsForServer atomically replaces all tools for a server.
// Uses a database transaction to ensure consistency under concurrent access.
//
// IMPORTANT: This function should only contain fast database operations.
// Network I/O (e.g., fetching tools from remote MCP servers) must happen
// BEFORE calling this function, not inside it. Holding a database transaction
// during slow operations can cause contention and degrade performance.
func (c *clientImpl) RefreshToolsForServer(serverName string, groupKind string, tools ...*v1alpha2.MCPTool) error {
	return c.db.Transaction(func(tx *gorm.DB) error {
		// Delete all existing tools for this server in the transaction
		if err := delete[Tool](tx,
			Clause{Key: "server_name", Value: serverName},
			Clause{Key: "group_kind", Value: groupKind}); err != nil {
			return fmt.Errorf("failed to delete existing tools: %w", err)
		}

		// Insert all new tools
		for _, tool := range tools {
			if err := save(tx, &Tool{
				ID:          tool.Name,
				ServerName:  serverName,
				GroupKind:   groupKind,
				Description: tool.Description,
			}); err != nil {
				return fmt.Errorf("failed to create tool %s: %w", tool.Name, err)
			}
		}

		return nil
	})
}

// ListMessagesForRun retrieves messages for a specific run (helper method)
func (c *clientImpl) ListMessagesForTask(taskID, userID string) ([]*protocol.Message, error) {
	messages, err := list[Event](c.db,
		Clause{Key: "task_id", Value: taskID},
		Clause{Key: "user_id", Value: userID})
	if err != nil {
		return nil, err
	}

	return ParseMessages(messages)
}

type QueryOptions struct {
	Limit int
	After time.Time
}

func (c *clientImpl) ListEventsForSession(sessionID, userID string, options QueryOptions) ([]*Event, error) {
	var events []Event
	query := c.db.
		Where("session_id = ?", sessionID).
		Where("user_id = ?", userID).
		Order("created_at DESC")

	if !options.After.IsZero() {
		query = query.Where("created_at > ?", options.After)
	}

	if options.Limit > 1 {
		query = query.Limit(options.Limit)
	}

	err := query.Find(&events).Error
	if err != nil {
		return nil, err
	}

	protocolEvents := make([]*Event, 0, len(events))
	for _, event := range events {
		protocolEvents = append(protocolEvents, &event)
	}

	return protocolEvents, nil
}

// GetMessage retrieves a protocol message from the database
func (c *clientImpl) GetMessage(messageID string) (*protocol.Message, error) {
	dbMessage, err := get[Event](c.db, Clause{Key: "id", Value: messageID})
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	var message protocol.Message
	if err := json.Unmarshal([]byte(dbMessage.Data), &message); err != nil {
		return nil, fmt.Errorf("failed to deserialize message: %w", err)
	}

	return &message, nil
}

// DeleteMessage deletes a protocol message from the database
func (c *clientImpl) DeleteMessage(messageID string) error {
	return delete[Event](c.db, Clause{Key: "id", Value: messageID})
}

// ListMessagesByContextID retrieves messages by context ID with optional limit
func (c *clientImpl) ListMessagesByContextID(contextID string, limit int) ([]protocol.Message, error) {
	var dbMessages []Event
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

// StoreTask stores a MemoryCancellableTask in the database
func (c *clientImpl) StoreTask(task *protocol.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to serialize task: %w", err)
	}

	dbTask := Task{
		ID:        task.ID,
		Data:      string(data),
		SessionID: task.ContextID,
	}

	return save(c.db, &dbTask)
}

// GetTask retrieves a MemoryCancellableTask from the database
func (c *clientImpl) GetTask(taskID string) (*protocol.Task, error) {
	dbTask, err := get[Task](c.db, Clause{Key: "id", Value: taskID})
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	var task protocol.Task
	if err := json.Unmarshal([]byte(dbTask.Data), &task); err != nil {
		return nil, fmt.Errorf("failed to deserialize task: %w", err)
	}

	return &task, nil
}

// TaskExists checks if a task exists in the database
func (c *clientImpl) TaskExists(taskID string) bool {
	var count int64
	c.db.Model(&Task{}).Where("id = ?", taskID).Count(&count)
	return count > 0
}

// StorePushNotification stores a push notification configuration in the database
func (c *clientImpl) StorePushNotification(config *protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification config: %w", err)
	}

	dbPushNotification := PushNotification{
		ID:     config.PushNotificationConfig.ID,
		TaskID: config.TaskID,
		Data:   string(data),
	}

	return save(c.db, &dbPushNotification)
}

// GetPushNotification retrieves a push notification configuration from the database
func (c *clientImpl) GetPushNotification(taskID string, configID string) (*protocol.TaskPushNotificationConfig, error) {
	dbPushNotification, err := get[PushNotification](c.db,
		Clause{Key: "task_id", Value: taskID},
		Clause{Key: "id", Value: configID})
	if err != nil {
		return nil, fmt.Errorf("failed to get push notification config: %w", err)
	}

	var config protocol.TaskPushNotificationConfig
	if err := json.Unmarshal([]byte(dbPushNotification.Data), &config); err != nil {
		return nil, fmt.Errorf("failed to deserialize push notification config: %w", err)
	}

	return &config, nil
}

func (c *clientImpl) ListPushNotifications(taskID string) ([]*protocol.TaskPushNotificationConfig, error) {
	pushNotifications, err := list[PushNotification](c.db, Clause{Key: "task_id", Value: taskID})
	if err != nil {
		return nil, err
	}

	protocolPushNotifications := make([]*protocol.TaskPushNotificationConfig, 0, len(pushNotifications))
	for _, pushNotification := range pushNotifications {
		var protocolPushNotification protocol.TaskPushNotificationConfig
		if err := json.Unmarshal([]byte(pushNotification.Data), &protocolPushNotification); err != nil {
			return nil, fmt.Errorf("failed to deserialize push notification config: %w", err)
		}
		protocolPushNotifications = append(protocolPushNotifications, &protocolPushNotification)
	}

	return protocolPushNotifications, nil
}

// DeletePushNotification deletes a push notification configuration from the database
func (c *clientImpl) DeletePushNotification(taskID string) error {
	return delete[PushNotification](c.db, Clause{Key: "task_id", Value: taskID})
}

// StoreCheckpoint stores a LangGraph checkpoint and its writes atomically
func (c *clientImpl) StoreCheckpoint(checkpoint *LangGraphCheckpoint) error {
	err := save(c.db, checkpoint)
	if err != nil {
		return fmt.Errorf("failed to store checkpoint: %w", err)
	}

	return nil
}

func (c *clientImpl) StoreCheckpointWrites(writes []*LangGraphCheckpointWrite) error {
	return c.db.Transaction(func(tx *gorm.DB) error {
		for _, write := range writes {
			if err := save(tx, write); err != nil {
				return fmt.Errorf("failed to store checkpoint write: %w", err)
			}
		}
		return nil
	})
}

// ListCheckpoints lists checkpoints for a thread, optionally filtered by beforeCheckpointID
func (c *clientImpl) ListCheckpoints(userID, threadID, checkpointNS string, checkpointID *string, limit int) ([]*LangGraphCheckpointTuple, error) {
	var checkpointTuples []*LangGraphCheckpointTuple
	if err := c.db.Transaction(func(tx *gorm.DB) error {
		query := c.db.Where(
			"user_id = ? AND thread_id = ? AND checkpoint_ns = ?",
			userID, threadID, checkpointNS,
		)

		if checkpointID != nil {
			query = query.Where("checkpoint_id = ?", *checkpointID)
		} else {
			query = query.Order("checkpoint_id DESC")
		}

		// Apply limit
		if limit > 0 {
			query = query.Limit(limit)
		}

		var checkpoints []LangGraphCheckpoint
		err := query.Find(&checkpoints).Error
		if err != nil {
			return fmt.Errorf("failed to list checkpoints: %w", err)
		}

		for _, checkpoint := range checkpoints {
			var writes []*LangGraphCheckpointWrite
			if err := tx.Where(
				"user_id = ? AND thread_id = ? AND checkpoint_ns = ? AND checkpoint_id = ?",
				userID, threadID, checkpointNS, checkpoint.CheckpointID,
			).Order("task_id, write_idx").Find(&writes).Error; err != nil {
				return fmt.Errorf("failed to get checkpoint writes: %w", err)
			}
			checkpointTuples = append(checkpointTuples, &LangGraphCheckpointTuple{
				Checkpoint: &checkpoint,
				Writes:     writes,
			})
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}
	return checkpointTuples, nil
}

// DeleteCheckpoint deletes a checkpoint and its writes atomically
func (c *clientImpl) DeleteCheckpoint(userID, threadID string) error {
	clauses := []Clause{
		{Key: "user_id", Value: userID},
		{Key: "thread_id", Value: threadID},
	}
	return c.db.Transaction(func(tx *gorm.DB) error {
		if err := delete[LangGraphCheckpoint](tx, clauses...); err != nil {
			return fmt.Errorf("failed to delete checkpoint: %w", err)
		}
		if err := delete[LangGraphCheckpointWrite](tx, clauses...); err != nil {
			return fmt.Errorf("failed to delete checkpoint writes: %w", err)
		}
		return nil
	})
}

// CrewAI methods

// StoreCrewAIMemory stores CrewAI agent memory
func (c *clientImpl) StoreCrewAIMemory(memory *CrewAIAgentMemory) error {
	err := save(c.db, memory)
	if err != nil {
		return fmt.Errorf("failed to store CrewAI agent memory: %w", err)
	}
	return nil
}

// SearchCrewAIMemoryByTask searches CrewAI agent memory by task description across all agents for a session
func (c *clientImpl) SearchCrewAIMemoryByTask(userID, threadID, taskDescription string, limit int) ([]*CrewAIAgentMemory, error) {
	var memories []*CrewAIAgentMemory

	// Search for task_description within the JSON memory_data field
	// Using JSON_EXTRACT or JSON_UNQUOTE for MySQL/PostgreSQL, or simple LIKE for SQLite
	// Sort by created_at DESC, then by score ASC (if score exists in JSON)
	query := c.db.Where(
		"user_id = ? AND thread_id = ? AND (memory_data LIKE ? OR JSON_EXTRACT(memory_data, '$.task_description') LIKE ?)",
		userID, threadID, "%"+taskDescription+"%", "%"+taskDescription+"%",
	).Order("created_at DESC, JSON_EXTRACT(memory_data, '$.score') ASC")

	// Apply limit
	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&memories).Error
	if err != nil {
		return nil, fmt.Errorf("failed to search CrewAI agent memory by task: %w", err)
	}

	return memories, nil
}

// ResetCrewAIMemory deletes all CrewAI agent memory for a session
func (c *clientImpl) ResetCrewAIMemory(userID, threadID string) error {
	result := c.db.Where(
		"user_id = ? AND thread_id = ?",
		userID, threadID,
	).Delete(&CrewAIAgentMemory{})

	if result.Error != nil {
		return fmt.Errorf("failed to reset CrewAI agent memory: %w", result.Error)
	}

	return nil
}

// StoreCrewAIFlowState stores CrewAI flow state
func (c *clientImpl) StoreCrewAIFlowState(state *CrewAIFlowState) error {
	err := save(c.db, state)
	if err != nil {
		return fmt.Errorf("failed to store CrewAI flow state: %w", err)
	}
	return nil
}

// GetCrewAIFlowState retrieves the most recent CrewAI flow state
func (c *clientImpl) GetCrewAIFlowState(userID, threadID string) (*CrewAIFlowState, error) {
	var state CrewAIFlowState

	// Get the most recent state by ordering by created_at DESC
	// Thread_id is equivalent to flow_uuid used by CrewAI because in each session there is only one flow
	err := c.db.Where(
		"user_id = ? AND thread_id = ?",
		userID, threadID,
	).Order("created_at DESC").First(&state).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Return nil for not found, as expected by the Python client
		}
		return nil, fmt.Errorf("failed to get CrewAI flow state: %w", err)
	}

	return &state, nil
}

// AgentMemory methods

func (c *clientImpl) StoreAgentMemory(memory *Memory) error {
	return save(c.db, memory)
}

func (c *clientImpl) StoreAgentMemories(memories []*Memory) error {
	return c.db.Transaction(func(tx *gorm.DB) error {
		for _, memory := range memories {
			if err := save(tx, memory); err != nil {
				return err
			}
		}
		return nil
	})
}

func (c *clientImpl) SearchAgentMemory(agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error) {
	var results []AgentMemorySearchResult

	if c.db.Name() == "sqlite" {
		// libSQL/Turso syntax: vector_distance_cos(embedding, vector32('JSON_ARRAY'))
		// We must use fmt.Sprintf to inline the JSON array because vector32() requires a string literal
		// and parameter binding with ? fails with "unexpected token" errors
		embeddingJSON, err := json.Marshal(embedding.Slice())
		if err != nil {
			return nil, fmt.Errorf("failed to serialize embedding: %w", err)
		}

		// Safe formatting because we control the JSON string generation from float slice
		query := fmt.Sprintf(`
			SELECT id, agent_name, user_id, content, metadata, created_at, expires_at, access_count,
			       1 - vector_distance_cos(embedding, vector32('%s')) as score
			FROM memory
			WHERE agent_name = ? AND user_id = ?
			ORDER BY vector_distance_cos(embedding, vector32('%s')) ASC
			LIMIT ?
		`, string(embeddingJSON), string(embeddingJSON))

		if err := c.db.Raw(query, agentName, userID, limit).Scan(&results).Error; err != nil {
			return nil, fmt.Errorf("failed to search agent memory (sqlite): %w", err)
		}
	} else {
		// Postgres pgvector syntax: uses <=> operator for cosine distance
		// pgvector.Vector implements sql.Scanner and driver.Valuer
		query := `
			SELECT *, 1 - (embedding <=> ?) as score
			FROM memory
			WHERE agent_name = ? AND user_id = ?
			ORDER BY embedding <=> ? ASC
			LIMIT ?
		`
		if err := c.db.Raw(query, embedding, agentName, userID, embedding, limit).Scan(&results).Error; err != nil {
			return nil, fmt.Errorf("failed to search agent memory (postgres): %w", err)
		}
	}

	// Increment access count for found memories asynchronously
	go func(memories []AgentMemorySearchResult) {
		ids := make([]string, len(memories))
		for i, m := range memories {
			ids[i] = m.ID
		}
		if len(ids) > 0 {
			if err := c.db.Model(&Memory{}).Where("id IN ?", ids).UpdateColumn("access_count", gorm.Expr("access_count + ?", 1)).Error; err != nil {
				// Just log error, don't fail the request
				fmt.Printf("failed to increment access count: %v\n", err)
			}
		}
	}(results)

	// Print results, the content and the associated score
	for _, result := range results {
		fmt.Printf("Memory: %v, Score: %v\n", result.Content, result.Score)
	}

	return results, nil
}

// PruneExpiredMemories deletes expired memories if they haven't been accessed enough,
// otherwise extends their TTL.
func (c *clientImpl) PruneExpiredMemories() error {
	return c.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		// 1. Extend TTL for popular memories (AccessCount >= 10)
		if err := tx.Model(&Memory{}).
			Where("expires_at < ? AND access_count >= ?", now, 10).
			Updates(map[string]any{
				"expires_at":   now.Add(15 * 24 * time.Hour),
				"access_count": 0, // Reset count to ensure it's still relevant next time
			}).Error; err != nil {
			return fmt.Errorf("failed to extend TTL for popular memories: %w", err)
		}

		// 2. Delete unpopular expired memories (AccessCount < 10)
		if err := tx.Where("expires_at < ? AND access_count < ?", now, 10).
			Delete(&Memory{}).Error; err != nil {
			return fmt.Errorf("failed to delete expired memories: %w", err)
		}

		return nil
	})
}

func (c *clientImpl) DeleteAgentMemory(agentName, userID string) error {
	normalizedName := strings.ReplaceAll(agentName, "-", "_")

	// Delete both original name and normalized name
	// Sometimes frontend has naming inconsistencies with backend
	err := delete[Memory](c.db,
		Clause{Key: "agent_name", Value: agentName},
		Clause{Key: "user_id", Value: userID})
	if err != nil {
		return err
	}

	if normalizedName != agentName {
		return delete[Memory](c.db,
			Clause{Key: "agent_name", Value: normalizedName},
			Clause{Key: "user_id", Value: userID})
	}

	return nil
}

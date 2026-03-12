package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// InMemoryFakeClient is a fake implementation of database.Client for testing
type InMemoryFakeClient struct {
	mu                sync.RWMutex
	feedback          map[string]*database.Feedback
	tasks             map[string]*database.Task    // changed from runs, key: taskID
	sessions          map[string]*database.Session // key: sessionID_userID
	agents            map[string]*database.Agent   // changed from teams
	toolServers       map[string]*database.ToolServer
	tools             map[string]*database.Tool
	eventsBySession   map[string][]*database.Event                    // key: sessionId
	events            map[string]*database.Event                      // key: eventID
	pushNotifications map[string]*protocol.TaskPushNotificationConfig // key: taskID
	checkpoints       map[string]*database.LangGraphCheckpoint        // key: user_id:thread_id:checkpoint_ns:checkpoint_id
	checkpointWrites  map[string][]*database.LangGraphCheckpointWrite // key: user_id:thread_id:checkpoint_ns:checkpoint_id
	crewaiMemory      map[string][]*database.CrewAIAgentMemory        // key: user_id:thread_id:agent_id
	crewaiFlowStates  map[string]*database.CrewAIFlowState            // key: user_id:thread_id
	memories          map[string]*database.Memory                     // key: user_id:thread_id:agent_id
	nextFeedbackID    int
}

// NewClient creates a new fake database client
func NewClient() database.Client {
	return &InMemoryFakeClient{
		feedback:          make(map[string]*database.Feedback),
		tasks:             make(map[string]*database.Task),
		sessions:          make(map[string]*database.Session),
		agents:            make(map[string]*database.Agent),
		toolServers:       make(map[string]*database.ToolServer),
		tools:             make(map[string]*database.Tool),
		eventsBySession:   make(map[string][]*database.Event),
		events:            make(map[string]*database.Event),
		pushNotifications: make(map[string]*protocol.TaskPushNotificationConfig),
		checkpoints:       make(map[string]*database.LangGraphCheckpoint),
		checkpointWrites:  make(map[string][]*database.LangGraphCheckpointWrite),
		crewaiMemory:      make(map[string][]*database.CrewAIAgentMemory),
		crewaiFlowStates:  make(map[string]*database.CrewAIFlowState),
		memories:          make(map[string]*database.Memory),
		nextFeedbackID:    1,
	}
}

func (c *InMemoryFakeClient) sessionKey(sessionID, userID string) string {
	return fmt.Sprintf("%s_%s", sessionID, userID)
}

func (c *InMemoryFakeClient) DeletePushNotification(_ context.Context, taskID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.pushNotifications, taskID)
	return nil
}

func (c *InMemoryFakeClient) GetPushNotification(_ context.Context, taskID string, configID string) (*protocol.TaskPushNotificationConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.pushNotifications[taskID], nil
}

func (c *InMemoryFakeClient) GetTask(_ context.Context, taskID string) (*protocol.Task, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	task, exists := c.tasks[taskID]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	parsedTask := &protocol.Task{}
	err := json.Unmarshal([]byte(task.Data), parsedTask)
	if err != nil {
		return nil, err
	}
	return parsedTask, nil
}

func (c *InMemoryFakeClient) DeleteTask(_ context.Context, taskID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.tasks, taskID)
	return nil
}

// StoreFeedback creates a new feedback record
func (c *InMemoryFakeClient) StoreFeedback(_ context.Context, feedback *database.Feedback) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Copy the feedback and assign an ID
	newFeedback := *feedback
	newFeedback.MessageID = uint(c.nextFeedbackID)
	c.nextFeedbackID++

	key := fmt.Sprintf("%d", newFeedback.MessageID)
	c.feedback[key] = &newFeedback
	return nil
}

// StoreEvents creates a new event record
func (c *InMemoryFakeClient) StoreEvents(_ context.Context, events ...*database.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, event := range events {
		c.events[event.ID] = event
		c.eventsBySession[event.SessionID] = append(c.eventsBySession[event.SessionID], event)
	}

	return nil
}

// StoreSession creates a new session record
func (c *InMemoryFakeClient) StoreSession(_ context.Context, session *database.Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.sessionKey(session.ID, session.UserID)
	c.sessions[key] = session
	return nil
}

// StoreAgent creates a new agent record
func (c *InMemoryFakeClient) StoreAgent(_ context.Context, agent *database.Agent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agents[agent.ID] = agent
	return nil
}

// StoreTask creates a new task record
func (c *InMemoryFakeClient) StoreTask(_ context.Context, task *protocol.Task) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	jsn, err := json.Marshal(task)
	if err != nil {
		return err
	}
	c.tasks[task.ID] = &database.Task{
		ID:   task.ID,
		Data: string(jsn),
	}
	return nil
}

// StorePushNotification creates a new push notification record
func (c *InMemoryFakeClient) StorePushNotification(_ context.Context, config *protocol.TaskPushNotificationConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pushNotifications[config.TaskID] = config
	return nil
}

// StoreToolServer creates a new tool server record
func (c *InMemoryFakeClient) StoreToolServer(_ context.Context, toolServer *database.ToolServer) (*database.ToolServer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.toolServers[toolServer.Name] = toolServer
	return toolServer, nil
}

// CreateTool creates a new tool record
func (c *InMemoryFakeClient) CreateTool(_ context.Context, tool *database.Tool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tools[tool.ID] = tool
	return nil
}

// DeleteSession deletes a session by ID and user ID
func (c *InMemoryFakeClient) DeleteSession(_ context.Context, sessionID string, userID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.sessionKey(sessionID, userID)
	delete(c.sessions, key)
	return nil
}

// DeleteAgent deletes an agent by name
func (c *InMemoryFakeClient) DeleteAgent(_ context.Context, agentName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, exists := c.agents[agentName]
	if !exists {
		return gorm.ErrRecordNotFound
	}

	delete(c.agents, agentName)

	return nil
}

// DeleteToolServer deletes a tool server by name
func (c *InMemoryFakeClient) DeleteToolServer(_ context.Context, serverName string, groupKind string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.toolServers, serverName)
	return nil
}

// DeleteToolsForServer deletes tools for a tool server by name
func (c *InMemoryFakeClient) DeleteToolsForServer(_ context.Context, serverName string, groupKind string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Delete all tools that belong to the specified server
	for toolID, tool := range c.tools {
		if tool.ServerName == serverName {
			delete(c.tools, toolID)
		}
	}
	return nil
}

// GetSession retrieves a session by ID and user ID
func (c *InMemoryFakeClient) GetSession(_ context.Context, sessionID string, userID string) (*database.Session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.sessionKey(sessionID, userID)
	session, exists := c.sessions[key]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return session, nil
}

// GetAgent retrieves an agent by name
func (c *InMemoryFakeClient) GetAgent(_ context.Context, agentName string) (*database.Agent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	agent, exists := c.agents[agentName]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return agent, nil
}

// GetTool retrieves a tool by name
func (c *InMemoryFakeClient) GetTool(_ context.Context, toolName string) (*database.Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tool, exists := c.tools[toolName]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return tool, nil
}

// GetToolServer retrieves a tool server by name
func (c *InMemoryFakeClient) GetToolServer(_ context.Context, serverName string) (*database.ToolServer, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	server, exists := c.toolServers[serverName]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return server, nil
}

// ListFeedback lists all feedback for a user
func (c *InMemoryFakeClient) ListFeedback(_ context.Context, userID string) ([]database.Feedback, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Feedback
	for _, feedback := range c.feedback {
		if feedback.UserID == userID {
			result = append(result, *feedback)
		}
	}
	return result, nil
}

func (c *InMemoryFakeClient) ListTasksForSession(_ context.Context, sessionID string) ([]*protocol.Task, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*protocol.Task
	for _, task := range c.tasks {
		if task.SessionID == sessionID {
			parsed, err := task.Parse()
			if err != nil {
				return nil, err
			}
			result = append(result, &parsed)
		}
	}
	return result, nil
}

// ListSessions lists all sessions for a user
func (c *InMemoryFakeClient) ListSessions(_ context.Context, userID string) ([]database.Session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Session
	for _, session := range c.sessions {
		if session.UserID == userID {
			result = append(result, *session)
		}
	}
	slices.SortStableFunc(result, func(i, j database.Session) int {
		return strings.Compare(i.ID, j.ID)
	})
	return result, nil
}

// ListSessionsForAgent lists all sessions for an agent
func (c *InMemoryFakeClient) ListSessionsForAgent(_ context.Context, agentID string, userID string) ([]database.Session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Session
	for _, session := range c.sessions {
		if session.AgentID != nil && *session.AgentID == agentID && session.UserID == userID {
			result = append(result, *session)
		}
	}
	slices.SortStableFunc(result, func(i, j database.Session) int {
		return strings.Compare(i.ID, j.ID)
	})
	return result, nil
}

// ListAgents lists all agents
func (c *InMemoryFakeClient) ListAgents(_ context.Context) ([]database.Agent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Agent
	for _, agent := range c.agents {
		result = append(result, *agent)
	}
	slices.SortStableFunc(result, func(i, j database.Agent) int {
		return strings.Compare(i.ID, j.ID)
	})
	return result, nil
}

// ListToolServers lists all tool servers
func (c *InMemoryFakeClient) ListToolServers(_ context.Context) ([]database.ToolServer, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.ToolServer
	for _, server := range c.toolServers {
		result = append(result, *server)
	}
	slices.SortStableFunc(result, func(i, j database.ToolServer) int {
		return strings.Compare(i.Name+i.GroupKind, j.Name+j.GroupKind)
	})
	return result, nil
}

// ListTools lists all tools for a user
func (c *InMemoryFakeClient) ListTools(_ context.Context) ([]database.Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Tool
	for _, tool := range c.tools {
		result = append(result, *tool)
	}
	slices.SortStableFunc(result, func(i, j database.Tool) int {
		return strings.Compare(i.ServerName+i.ID, j.ServerName+j.ID)
	})
	return result, nil
}

// ListToolsForServer lists all tools for a specific server and toolserver type
func (c *InMemoryFakeClient) ListToolsForServer(_ context.Context, serverName string, groupKind string) ([]database.Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Tool
	for _, tool := range c.tools {
		// Search for tool server by name
		toolServer, exists := c.toolServers[serverName]
		if !exists {
			continue
		}
		if tool.ServerName == toolServer.Name && tool.GroupKind == groupKind {
			result = append(result, *tool)
		}
	}

	slices.SortStableFunc(result, func(i, j database.Tool) int {
		return strings.Compare(i.ServerName+i.ID, j.ServerName+j.ID)
	})
	return result, nil
}

func (c *InMemoryFakeClient) ListPushNotifications(_ context.Context, taskID string) ([]*protocol.TaskPushNotificationConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*protocol.TaskPushNotificationConfig
	config, exists := c.pushNotifications[taskID]
	if exists {
		result = append(result, config)
	}
	return result, nil
}

// ListEventsForSession retrieves events for a specific session
func (c *InMemoryFakeClient) ListEventsForSession(_ context.Context, sessionID, userID string, options database.QueryOptions) ([]*database.Event, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	events, exists := c.eventsBySession[sessionID]
	if !exists {
		return nil, nil
	}

	// Make a copy to avoid mutating the stored slice
	result := make([]*database.Event, len(events))
	copy(result, events)

	if !options.OrderAsc {
		// Default is DESC (newest first), reverse the insertion-order slice
		for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
	}

	return result, nil
}

// RefreshToolsForServer refreshes a tool server
func (c *InMemoryFakeClient) RefreshToolsForServer(_ context.Context, serverName string, groupKind string, tools ...*v1alpha2.MCPTool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Simple implementation: remove all existing tools for this server+groupKind and add new ones
	for toolID, tool := range c.tools {
		if tool.ServerName == serverName && tool.GroupKind == groupKind {
			delete(c.tools, toolID)
		}
	}

	// Add new tools
	for _, tool := range tools {
		c.tools[tool.Name] = &database.Tool{
			ID:          tool.Name,
			ServerName:  serverName,
			GroupKind:   groupKind,
			Description: tool.Description,
		}
	}

	return nil
}

// UpdateSession updates a session
func (c *InMemoryFakeClient) UpdateSession(_ context.Context, session *database.Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.sessionKey(session.ID, session.UserID)
	c.sessions[key] = session
	return nil
}

// UpdateToolServer updates a tool server
func (c *InMemoryFakeClient) UpdateToolServer(_ context.Context, server *database.ToolServer) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.toolServers[server.Name] = server
	return nil
}

// UpdateAgent updates an agent record
func (c *InMemoryFakeClient) UpdateAgent(_ context.Context, agent *database.Agent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agents[agent.ID] = agent
	return nil
}

// UpdateTask updates a task record
func (c *InMemoryFakeClient) UpdateTask(_ context.Context, task *database.Task) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tasks[task.ID] = task
	return nil
}

// AddTool adds a tool for testing purposes
func (c *InMemoryFakeClient) AddTool(tool *database.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tools[tool.ID] = tool
}

// AddTask adds a task for testing purposes
func (c *InMemoryFakeClient) AddTask(task *database.Task) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tasks[task.ID] = task
}

// Clear clears all data for testing purposes
func (c *InMemoryFakeClient) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.feedback = make(map[string]*database.Feedback)
	c.tasks = make(map[string]*database.Task)
	c.sessions = make(map[string]*database.Session)
	c.agents = make(map[string]*database.Agent)
	c.toolServers = make(map[string]*database.ToolServer)
	c.tools = make(map[string]*database.Tool)
	c.eventsBySession = make(map[string][]*database.Event)
	c.events = make(map[string]*database.Event)
	c.pushNotifications = make(map[string]*protocol.TaskPushNotificationConfig)
	c.checkpoints = make(map[string]*database.LangGraphCheckpoint)
	c.checkpointWrites = make(map[string][]*database.LangGraphCheckpointWrite)
	c.memories = make(map[string]*database.Memory)
	c.nextFeedbackID = 1
}

// UpsertAgent upserts an agent record
func (c *InMemoryFakeClient) UpsertAgent(_ context.Context, agent *database.Agent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agents[agent.ID] = agent
	return nil
}

// checkpointKey creates a key for checkpoint storage
func (c *InMemoryFakeClient) checkpointKey(userID, threadID, checkpointNS, checkpointID string) string {
	return fmt.Sprintf("%s:%s:%s:%s", userID, threadID, checkpointNS, checkpointID)
}

// StoreCheckpoint stores a LangGraph checkpoint
func (c *InMemoryFakeClient) StoreCheckpoint(_ context.Context, checkpoint *database.LangGraphCheckpoint) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.checkpointKey(checkpoint.UserID, checkpoint.ThreadID, checkpoint.CheckpointNS, checkpoint.CheckpointID)

	// Check for idempotent retry
	if existing, exists := c.checkpoints[key]; exists {
		if existing.Metadata == checkpoint.Metadata && existing.Checkpoint == checkpoint.Checkpoint {
			return nil // Idempotent success
		}
		return fmt.Errorf("checkpoint already exists with different data")
	}

	// Store checkpoint
	c.checkpoints[key] = checkpoint

	return nil
}

// StoreCheckpointWrites stores checkpoint writes
func (c *InMemoryFakeClient) StoreCheckpointWrites(_ context.Context, writes []*database.LangGraphCheckpointWrite) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Group writes by checkpoint key
	writesByKey := make(map[string][]*database.LangGraphCheckpointWrite)
	for _, write := range writes {
		key := c.checkpointKey(write.UserID, write.ThreadID, write.CheckpointNS, write.CheckpointID)
		writesByKey[key] = append(writesByKey[key], write)
	}

	// Store writes for each checkpoint
	for key, keyWrites := range writesByKey {
		c.checkpointWrites[key] = append(c.checkpointWrites[key], keyWrites...)
	}

	return nil
}

// GetLatestCheckpoint retrieves the most recent checkpoint for a thread
func (c *InMemoryFakeClient) GetLatestCheckpoint(_ context.Context, userID, threadID, checkpointNS string) (*database.LangGraphCheckpoint, []*database.LangGraphCheckpointWrite, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var latest *database.LangGraphCheckpoint
	var latestKey string

	// Find the latest checkpoint by creation time
	for key, checkpoint := range c.checkpoints {
		if checkpoint.UserID == userID && checkpoint.ThreadID == threadID && checkpoint.CheckpointNS == checkpointNS {
			if latest == nil || checkpoint.CreatedAt.After(latest.CreatedAt) {
				latest = checkpoint
				latestKey = key
			}
		}
	}

	if latest == nil {
		return nil, nil, nil
	}

	// Get writes for this checkpoint
	writes := c.checkpointWrites[latestKey]

	return latest, writes, nil
}

// GetCheckpoint retrieves a specific checkpoint by ID
func (c *InMemoryFakeClient) GetCheckpoint(_ context.Context, userID, threadID, checkpointNS, checkpointID string) (*database.LangGraphCheckpoint, []*database.LangGraphCheckpointWrite, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.checkpointKey(userID, threadID, checkpointNS, checkpointID)
	checkpoint, exists := c.checkpoints[key]
	if !exists {
		return nil, nil, nil
	}

	// Get writes for this checkpoint
	writes := c.checkpointWrites[key]

	return checkpoint, writes, nil
}

// ListCheckpoints lists checkpoints for a thread, optionally filtered by checkpointID
func (c *InMemoryFakeClient) ListCheckpoints(_ context.Context, userID, threadID, checkpointNS string, checkpointID *string, limit int) ([]*database.LangGraphCheckpointTuple, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*database.LangGraphCheckpointTuple

	// Find matching checkpoints
	for key, checkpoint := range c.checkpoints {
		if checkpoint.UserID == userID && checkpoint.ThreadID == threadID && checkpoint.CheckpointNS == checkpointNS {
			// If a specific checkpoint ID is requested, only return that one
			if checkpointID != nil && checkpoint.CheckpointID != *checkpointID {
				continue
			}

			// Get writes for this checkpoint
			writes := c.checkpointWrites[key]
			if writes == nil {
				writes = []*database.LangGraphCheckpointWrite{}
			}

			result = append(result, &database.LangGraphCheckpointTuple{
				Checkpoint: checkpoint,
				Writes:     writes,
			})
		}
	}

	// Sort by creation time (newest first)
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Checkpoint.CreatedAt.Before(result[j].Checkpoint.CreatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// DeleteCheckpoint deletes a checkpoint and its writes atomically
func (c *InMemoryFakeClient) DeleteCheckpoint(_ context.Context, userID, threadID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find and delete all checkpoints for the thread
	keysToDelete := make([]string, 0)
	for key, checkpoint := range c.checkpoints {
		if checkpoint.UserID == userID && checkpoint.ThreadID == threadID {
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Delete checkpoints and their writes
	for _, key := range keysToDelete {
		delete(c.checkpoints, key)
		delete(c.checkpointWrites, key)
	}

	return nil
}

// ListWrites retrieves writes for a specific checkpoint
func (c *InMemoryFakeClient) ListWrites(_ context.Context, userID, threadID, checkpointNS, checkpointID string, offset, limit int) ([]*database.LangGraphCheckpointWrite, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := c.checkpointKey(userID, threadID, checkpointNS, checkpointID)
	writes := c.checkpointWrites[key]

	if writes == nil {
		return []*database.LangGraphCheckpointWrite{}, nil
	}

	// Apply pagination
	start := offset
	if start >= len(writes) {
		return []*database.LangGraphCheckpointWrite{}, nil
	}

	end := len(writes)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	return writes[start:end], nil
}

// CrewAI methods

// StoreCrewAIMemory stores CrewAI agent memory
func (c *InMemoryFakeClient) StoreCrewAIMemory(_ context.Context, memory *database.CrewAIAgentMemory) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.crewaiMemory == nil {
		c.crewaiMemory = make(map[string][]*database.CrewAIAgentMemory)
	}

	key := fmt.Sprintf("%s:%s", memory.UserID, memory.ThreadID)
	c.crewaiMemory[key] = append(c.crewaiMemory[key], memory)

	return nil
}

// SearchCrewAIMemoryByTask searches CrewAI agent memory by task description across all agents for a session
func (c *InMemoryFakeClient) SearchCrewAIMemoryByTask(_ context.Context, userID, threadID, taskDescription string, limit int) ([]*database.CrewAIAgentMemory, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.crewaiMemory == nil {
		return []*database.CrewAIAgentMemory{}, nil
	}

	var allMemories []*database.CrewAIAgentMemory

	// Search across all agents for this user/thread
	for key, memories := range c.crewaiMemory {
		// Key format is "user_id:thread_id"
		if strings.HasPrefix(key, userID+":"+threadID) {
			for _, memory := range memories {
				// Parse the JSON memory data and search for task_description
				var memoryData map[string]any
				if err := json.Unmarshal([]byte(memory.MemoryData), &memoryData); err == nil {
					if taskDesc, ok := memoryData["task_description"].(string); ok {
						if strings.Contains(strings.ToLower(taskDesc), strings.ToLower(taskDescription)) {
							allMemories = append(allMemories, memory)
						}
					}
				}
				// Fallback to simple string search if JSON parsing fails
				if len(allMemories) == 0 && strings.Contains(strings.ToLower(memory.MemoryData), strings.ToLower(taskDescription)) {
					allMemories = append(allMemories, memory)
				}
			}
		}
	}

	// Sort by created_at DESC, then by score ASC (if score exists in JSON)
	slices.SortStableFunc(allMemories, func(i, j *database.CrewAIAgentMemory) int {
		// First sort by created_at DESC (most recent first)
		if !i.CreatedAt.Equal(j.CreatedAt) {
			if i.CreatedAt.After(j.CreatedAt) {
				return -1
			} else {
				return 1
			}
		}

		// If created_at is equal, sort by score ASC
		var scoreI, scoreJ float64
		var memoryDataI, memoryDataJ map[string]any

		if err := json.Unmarshal([]byte(i.MemoryData), &memoryDataI); err == nil {
			if score, ok := memoryDataI["score"].(float64); ok {
				scoreI = score
			}
		}

		if err := json.Unmarshal([]byte(j.MemoryData), &memoryDataJ); err == nil {
			if score, ok := memoryDataJ["score"].(float64); ok {
				scoreJ = score
			}
		}

		if scoreI < scoreJ {
			return -1
		} else if scoreI > scoreJ {
			return 1
		} else {
			return 0
		}
	})

	// Apply limit
	if limit > 0 && len(allMemories) > limit {
		allMemories = allMemories[:limit]
	}

	return allMemories, nil
}

// ResetCrewAIMemory deletes all CrewAI agent memory for a session
func (c *InMemoryFakeClient) ResetCrewAIMemory(_ context.Context, userID, threadID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.crewaiMemory == nil {
		return nil
	}

	// Find and delete all memory entries for this user/thread combination
	keysToDelete := make([]string, 0)
	for key := range c.crewaiMemory {
		// Key format is "user_id:thread_id"
		if strings.HasPrefix(key, userID+":"+threadID) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	// Delete the entries
	for _, key := range keysToDelete {
		delete(c.crewaiMemory, key)
	}

	return nil
}

// StoreCrewAIFlowState stores CrewAI flow state
func (c *InMemoryFakeClient) StoreCrewAIFlowState(_ context.Context, state *database.CrewAIFlowState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.crewaiFlowStates == nil {
		c.crewaiFlowStates = make(map[string]*database.CrewAIFlowState)
	}

	key := fmt.Sprintf("%s:%s", state.UserID, state.ThreadID)
	c.crewaiFlowStates[key] = state

	return nil
}

// GetCrewAIFlowState retrieves CrewAI flow state
func (c *InMemoryFakeClient) GetCrewAIFlowState(_ context.Context, userID, threadID string) (*database.CrewAIFlowState, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.crewaiFlowStates == nil {
		return nil, nil
	}

	key := fmt.Sprintf("%s:%s", userID, threadID)
	state := c.crewaiFlowStates[key]

	return state, nil
}

// memoryKey creates a unique key for a memory record
func (c *InMemoryFakeClient) memoryKey(agentName, userID, id string) string {
	return fmt.Sprintf("%s:%s:%s", agentName, userID, id)
}

// StoreAgentMemory stores agent memory
func (c *InMemoryFakeClient) StoreAgentMemory(_ context.Context, memory *database.Memory) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if memory.ID == "" {
		memory.ID = fmt.Sprintf("%d", len(c.memories)+1)
	}
	key := c.memoryKey(memory.AgentName, memory.UserID, memory.ID)
	c.memories[key] = memory
	return nil
}

// StoreAgentMemories stores multiple agent memories
func (c *InMemoryFakeClient) StoreAgentMemories(_ context.Context, memories []*database.Memory) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, memory := range memories {
		if memory.ID == "" {
			memory.ID = fmt.Sprintf("%d", len(c.memories)+1)
		}
		key := c.memoryKey(memory.AgentName, memory.UserID, memory.ID)
		c.memories[key] = memory
	}
	return nil
}

// cosineSimilarity computes the cosine similarity between two float32 slices.
// Returns 0 if either vector has zero magnitude.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// SearchAgentMemory searches agent memory by vector similarity
func (c *InMemoryFakeClient) SearchAgentMemory(_ context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]database.AgentMemorySearchResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	queryVec := embedding.Slice()
	now := time.Now()

	var results []database.AgentMemorySearchResult
	for _, memory := range c.memories {
		if memory.AgentName != agentName || memory.UserID != userID {
			continue
		}
		// Skip expired memories
		if memory.ExpiresAt != nil && memory.ExpiresAt.Before(now) {
			continue
		}
		score := cosineSimilarity(queryVec, memory.Embedding.Slice())
		results = append(results, database.AgentMemorySearchResult{
			Memory: *memory,
			Score:  score,
		})
	}

	// Sort by score descending
	slices.SortStableFunc(results, func(i, j database.AgentMemorySearchResult) int {
		if i.Score > j.Score {
			return -1
		} else if i.Score < j.Score {
			return 1
		}
		return 0
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// ListAgentMemories lists agent memories ordered by access count descending
func (c *InMemoryFakeClient) ListAgentMemories(_ context.Context, agentName, userID string) ([]database.Memory, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Memory
	for _, memory := range c.memories {
		if memory.AgentName == agentName && memory.UserID == userID {
			result = append(result, *memory)
		}
	}

	// Sort by access_count DESC
	slices.SortStableFunc(result, func(i, j database.Memory) int {
		if i.AccessCount > j.AccessCount {
			return -1
		} else if i.AccessCount < j.AccessCount {
			return 1
		}
		return 0
	})

	return result, nil
}

// DeleteAgentMemory deletes all agent memory for a given agent and user
func (c *InMemoryFakeClient) DeleteAgentMemory(_ context.Context, agentName, userID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, memory := range c.memories {
		if memory.AgentName == agentName && memory.UserID == userID {
			delete(c.memories, key)
		}
	}
	return nil
}

// PruneExpiredMemories removes all memories whose ExpiresAt is in the past
func (c *InMemoryFakeClient) PruneExpiredMemories(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, memory := range c.memories {
		if memory.ExpiresAt != nil && memory.ExpiresAt.Before(now) {
			delete(c.memories, key)
		}
	}
	return nil
}

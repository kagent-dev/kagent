package fake

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	autogen_client "github.com/kagent-dev/kagent/go/internal/autogen/client"
	"github.com/kagent-dev/kagent/go/internal/database"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// InMemmoryFakeClient is a fake implementation of database.Client for testing
type InMemmoryFakeClient struct {
	mu                sync.RWMutex
	feedback          map[string]*database.Feedback
	tasks             map[string]*database.Task    // changed from runs, key: taskID
	sessions          map[string]*database.Session // key: sessionID_userID
	agents            map[string]*database.Agent   // changed from teams
	toolServers       map[string]*database.ToolServer
	tools             map[string]*database.Tool
	messagesBySession map[string][]*database.Message                  // key: sessionId
	messagesByTask    map[string][]*database.Message                  // key: taskID
	messages          map[string]*database.Message                    // key: messageID
	pushNotifications map[string]*protocol.TaskPushNotificationConfig // key: taskID
	nextFeedbackID    int
}

// NewClient creates a new fake database client
func NewClient() database.Client {
	return &InMemmoryFakeClient{
		feedback:          make(map[string]*database.Feedback),
		tasks:             make(map[string]*database.Task),
		sessions:          make(map[string]*database.Session),
		agents:            make(map[string]*database.Agent),
		toolServers:       make(map[string]*database.ToolServer),
		tools:             make(map[string]*database.Tool),
		messagesBySession: make(map[string][]*database.Message),
		messagesByTask:    make(map[string][]*database.Message),
		messages:          make(map[string]*database.Message),
		pushNotifications: make(map[string]*protocol.TaskPushNotificationConfig),
		nextFeedbackID:    1,
	}
}
func (c *InMemmoryFakeClient) messageKey(message *protocol.Message) string {
	taskId := "none"
	if message.TaskID != nil {
		taskId = *message.TaskID
	}
	contextId := "none"
	if message.ContextID != nil {
		contextId = *message.ContextID
	}
	return fmt.Sprintf("%s_%s", taskId, contextId)
}

func (c *InMemmoryFakeClient) sessionKey(sessionID, userID string) string {
	return fmt.Sprintf("%s_%s", sessionID, userID)
}

func (c *InMemmoryFakeClient) CreatePushNotification(taskID string, config *protocol.TaskPushNotificationConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pushNotifications[taskID] = config
	return nil
}

func (c *InMemmoryFakeClient) GetPushNotification(taskID string) (*protocol.TaskPushNotificationConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.pushNotifications[taskID], nil
}

func (c *InMemmoryFakeClient) CreateTask(task *protocol.Task) error {
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

func (c *InMemmoryFakeClient) GetMessage(messageID string) (*database.Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	message, exists := c.messages[messageID]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return message, nil
}

func (c *InMemmoryFakeClient) GetTask(taskID string) (*database.Task, error) {

	c.mu.RLock()
	defer c.mu.RUnlock()

	task, exists := c.tasks[taskID]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return task, nil
}

func (c *InMemmoryFakeClient) DeleteTask(taskID string) error {

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.tasks, taskID)
	return nil
}

// CreateFeedback creates a new feedback record
func (c *InMemmoryFakeClient) CreateFeedback(feedback *database.Feedback) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Copy the feedback and assign an ID
	newFeedback := *feedback
	newFeedback.ID = uint(c.nextFeedbackID)
	c.nextFeedbackID++

	key := fmt.Sprintf("%d", newFeedback.ID)
	c.feedback[key] = &newFeedback
	return nil
}

// CreateMessages creates a new message record

func (c *InMemmoryFakeClient) CreateMessages(messages ...*protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, message := range messages {
		jsn, err := json.Marshal(message)
		if err != nil {
			return err
		}
		marshaledMessage := &database.Message{
			ID:   message.MessageID,
			Data: string(jsn),
		}
		if message.TaskID != nil {
			c.messagesByTask[*message.TaskID] = append(c.messagesByTask[*message.TaskID], marshaledMessage)
		}
		if message.ContextID != nil {
			c.messagesBySession[*message.ContextID] = append(c.messagesBySession[*message.ContextID], marshaledMessage)
		}
	}

	return nil
}

// CreateSession creates a new session record
func (c *InMemmoryFakeClient) CreateSession(session *database.Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.sessionKey(session.ID, session.UserID)
	c.sessions[key] = session
	return nil
}

// CreateAgent creates a new agent record
func (c *InMemmoryFakeClient) CreateAgent(agent *database.Agent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agents[agent.Name] = agent
	return nil
}

// UpsertAgent upserts an agent record
func (c *InMemmoryFakeClient) UpsertAgent(agent *database.Agent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agents[agent.Name] = agent
	return nil
}

// CreateToolServer creates a new tool server record
func (c *InMemmoryFakeClient) CreateToolServer(toolServer *database.ToolServer) (*database.ToolServer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.toolServers[toolServer.Name] = toolServer
	return toolServer, nil
}

// CreateTool creates a new tool record
func (c *InMemmoryFakeClient) CreateTool(tool *database.Tool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tools[tool.Name] = tool
	return nil
}

// DeleteSession deletes a session by ID and user ID
func (c *InMemmoryFakeClient) DeleteSession(sessionID string, userID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.sessionKey(sessionID, userID)
	delete(c.sessions, key)
	return nil
}

// DeleteAgent deletes an agent by name
func (c *InMemmoryFakeClient) DeleteAgent(agentName string) error {
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
func (c *InMemmoryFakeClient) DeleteToolServer(serverName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.toolServers, serverName)
	return nil
}

// GetSession retrieves a session by ID and user ID
func (c *InMemmoryFakeClient) GetSession(sessionID string, userID string) (*database.Session, error) {
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
func (c *InMemmoryFakeClient) GetAgent(agentName string) (*database.Agent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	agent, exists := c.agents[agentName]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return agent, nil
}

// GetTool retrieves a tool by name
func (c *InMemmoryFakeClient) GetTool(toolName string) (*database.Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tool, exists := c.tools[toolName]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return tool, nil
}

// GetToolServer retrieves a tool server by name
func (c *InMemmoryFakeClient) GetToolServer(serverName string) (*database.ToolServer, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	server, exists := c.toolServers[serverName]
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}
	return server, nil
}

// ListFeedback lists all feedback for a user
func (c *InMemmoryFakeClient) ListFeedback(userID string) ([]database.Feedback, error) {
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

// ListSessionTasks lists all tasks for a specific session
func (c *InMemmoryFakeClient) ListSessionTasks(sessionID string, userID string) ([]database.Task, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Task
	for _, task := range c.tasks {
		if task.SessionID == sessionID && task.UserID == userID {
			result = append(result, *task)
		}
	}
	return result, nil
}

// ListSessions lists all sessions for a user
func (c *InMemmoryFakeClient) ListSessions(userID string) ([]database.Session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Session
	for _, session := range c.sessions {
		if session.UserID == userID {
			result = append(result, *session)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// ListSessionsForAgent lists all sessions for an agent
func (c *InMemmoryFakeClient) ListSessionsForAgent(agentID uint, userID string) ([]database.Session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Session
	for _, session := range c.sessions {
		if session.AgentID != nil && *session.AgentID == agentID && session.UserID == userID {
			result = append(result, *session)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// ListAgents lists all agents
func (c *InMemmoryFakeClient) ListAgents() ([]database.Agent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Agent
	for _, agent := range c.agents {
		result = append(result, *agent)
	}
	return result, nil
}

// ListToolServers lists all tool servers
func (c *InMemmoryFakeClient) ListToolServers() ([]database.ToolServer, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.ToolServer
	for _, server := range c.toolServers {
		result = append(result, *server)
	}
	return result, nil
}

// ListTools lists all tools for a user
func (c *InMemmoryFakeClient) ListTools() ([]database.Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Tool
	for _, tool := range c.tools {
		result = append(result, *tool)
	}
	return result, nil
}

// ListToolsForServer lists all tools for a specific server
func (c *InMemmoryFakeClient) ListToolsForServer(serverName string) ([]database.Tool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []database.Tool
	for _, tool := range c.tools {
		// Search for tool server by name
		toolServer, exists := c.toolServers[serverName]
		if !exists {
			continue
		}
		if tool.ServerName == toolServer.Name {
			result = append(result, *tool)
		}
	}
	return result, nil
}

// ListMessagesForTask retrieves messages for a specific task
func (c *InMemmoryFakeClient) ListMessagesForTask(taskID, userID string) ([]database.Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	messages, exists := c.messagesByTask[taskID]
	if !exists {
		return []database.Message{}, nil
	}

	// Convert []*Message to []Message
	result := make([]database.Message, len(messages))
	for i, msg := range messages {
		result[i] = *msg
	}
	return result, nil
}

// ListMessagesForSession retrieves messages for a specific session
func (c *InMemmoryFakeClient) ListMessagesForSession(sessionID, userID string) ([]database.Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	messages, exists := c.messagesBySession[sessionID]
	if !exists {
		return []database.Message{}, nil
	}

	result := make([]database.Message, len(messages))
	for i, msg := range messages {
		result[i] = *msg
	}
	return result, nil
}

// RefreshToolsForServer refreshes a tool server
func (c *InMemmoryFakeClient) RefreshToolsForServer(serverName string, tools []*autogen_client.NamedTool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// For now, just return nil - this would need a proper implementation
	// based on the actual requirements
	return nil
}

// UpdateSession updates a session
func (c *InMemmoryFakeClient) UpdateSession(session *database.Session) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.sessionKey(session.ID, session.UserID)
	c.sessions[key] = session
	return nil
}

// UpdateToolServer updates a tool server
func (c *InMemmoryFakeClient) UpdateToolServer(server *database.ToolServer) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.toolServers[server.Name] = server
	return nil
}

// UpdateAgent updates an agent record
func (c *InMemmoryFakeClient) UpdateAgent(agent *database.Agent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.agents[agent.Name] = agent
	return nil
}

// UpdateTask updates a task record
func (c *InMemmoryFakeClient) UpdateTask(task *database.Task) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tasks[task.ID] = task
	return nil
}

// AddTool adds a tool for testing purposes
func (c *InMemmoryFakeClient) AddTool(tool *database.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tools[tool.Name] = tool
}

// AddTask adds a task for testing purposes
func (c *InMemmoryFakeClient) AddTask(task *database.Task) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tasks[task.ID] = task
}

// Clear clears all data for testing purposes
func (c *InMemmoryFakeClient) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.feedback = make(map[string]*database.Feedback)
	c.tasks = make(map[string]*database.Task)
	c.sessions = make(map[string]*database.Session)
	c.agents = make(map[string]*database.Agent)
	c.toolServers = make(map[string]*database.ToolServer)
	c.tools = make(map[string]*database.Tool)
	c.messagesBySession = make(map[string][]*database.Message)
	c.messagesByTask = make(map[string][]*database.Message)
	c.nextFeedbackID = 1
}

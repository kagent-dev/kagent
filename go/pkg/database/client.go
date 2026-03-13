package database

import (
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type QueryOptions struct {
	Limit    int
	After    time.Time
	OrderAsc bool // When true, order results by created_at ASC (chronological). Default is DESC (newest first).
}
type LangGraphCheckpointTuple struct {
	Checkpoint *LangGraphCheckpoint
	Writes     []*LangGraphCheckpointWrite
}

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
}

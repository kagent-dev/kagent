package database

import (
	"context"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/pgvector/pgvector-go"
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
	StoreFeedback(ctx context.Context, feedback *Feedback) error
	StoreSession(ctx context.Context, session *Session) error
	StoreAgent(ctx context.Context, agent *Agent) error
	StoreTask(ctx context.Context, task *protocol.Task) error
	StorePushNotification(ctx context.Context, config *protocol.TaskPushNotificationConfig) error
	StoreToolServer(ctx context.Context, toolServer *ToolServer) (*ToolServer, error)
	StoreEvents(ctx context.Context, messages ...*Event) error

	// Delete methods
	DeleteSession(ctx context.Context, sessionID string, userID string) error
	DeleteAgent(ctx context.Context, agentID string) error
	DeleteToolServer(ctx context.Context, serverName string, groupKind string) error
	DeleteTask(ctx context.Context, taskID string) error
	DeletePushNotification(ctx context.Context, taskID string) error
	DeleteToolsForServer(ctx context.Context, serverName string, groupKind string) error

	// Get methods
	GetSession(ctx context.Context, sessionID string, userID string) (*Session, error)
	GetAgent(ctx context.Context, name string) (*Agent, error)
	GetTask(ctx context.Context, id string) (*protocol.Task, error)
	GetTool(ctx context.Context, name string) (*Tool, error)
	GetToolServer(ctx context.Context, name string) (*ToolServer, error)
	GetPushNotification(ctx context.Context, taskID string, configID string) (*protocol.TaskPushNotificationConfig, error)

	// List methods
	ListTools(ctx context.Context) ([]Tool, error)
	ListFeedback(ctx context.Context, userID string) ([]Feedback, error)
	ListTasksForSession(ctx context.Context, sessionID string) ([]*protocol.Task, error)
	ListSessions(ctx context.Context, userID string) ([]Session, error)
	ListSessionsForAgent(ctx context.Context, agentID string, userID string) ([]Session, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	ListToolServers(ctx context.Context) ([]ToolServer, error)
	ListToolsForServer(ctx context.Context, serverName string, groupKind string) ([]Tool, error)
	ListEventsForSession(ctx context.Context, sessionID, userID string, options QueryOptions) ([]*Event, error)
	ListPushNotifications(ctx context.Context, taskID string) ([]*protocol.TaskPushNotificationConfig, error)

	// Helper methods
	RefreshToolsForServer(ctx context.Context, serverName string, groupKind string, tools ...*v1alpha2.MCPTool) error

	// LangGraph Checkpoint methods
	StoreCheckpoint(ctx context.Context, checkpoint *LangGraphCheckpoint) error
	StoreCheckpointWrites(ctx context.Context, writes []*LangGraphCheckpointWrite) error
	ListCheckpoints(ctx context.Context, userID, threadID, checkpointNS string, checkpointID *string, limit int) ([]*LangGraphCheckpointTuple, error)
	DeleteCheckpoint(ctx context.Context, userID, threadID string) error

	// CrewAI methods
	StoreCrewAIMemory(ctx context.Context, memory *CrewAIAgentMemory) error
	SearchCrewAIMemoryByTask(ctx context.Context, userID, threadID, taskDescription string, limit int) ([]*CrewAIAgentMemory, error)
	ResetCrewAIMemory(ctx context.Context, userID, threadID string) error
	StoreCrewAIFlowState(ctx context.Context, state *CrewAIFlowState) error
	GetCrewAIFlowState(ctx context.Context, userID, threadID string) (*CrewAIFlowState, error)

	// Agent memory (vector search) methods
	StoreAgentMemory(ctx context.Context, memory *Memory) error
	StoreAgentMemories(ctx context.Context, memories []*Memory) error
	SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error)
	ListAgentMemories(ctx context.Context, agentName, userID string) ([]Memory, error)
	DeleteAgentMemory(ctx context.Context, agentName, userID string) error
	PruneExpiredMemories(ctx context.Context) error
}

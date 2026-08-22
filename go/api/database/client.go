package database

import (
	"context"
	"errors"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/pgvector/pgvector-go"
)

// ErrTaskOwnedByAnotherUser means a task with this id already belongs to a
// different user.
var ErrTaskOwnedByAnotherUser = errors.New("task id owned by another user")

// ErrSessionIDRetired means the session id belongs to a soft-deleted session.
// The id stays unusable for as long as the tombstone exists, and the tombstone
// and the events and tasks it owns are left as they are. Once retention
// hard-deletes the tombstone the id inserts normally again.
var ErrSessionIDRetired = errors.New("session id belongs to a deleted session")

var ErrIdempotencyConflict = errors.New("request id was already used with different parameters")

var ErrAgentInstanceConflict = errors.New("AgentInstance lifecycle operation conflicts with its current state")

var ErrAgentInstanceTaskConflict = errors.New("AgentInstance already has an active task")

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
	StoreTask(ctx context.Context, task *a2a.Task, userID string) error
	StorePushNotification(ctx context.Context, config *a2a.PushConfig) error
	StoreToolServer(ctx context.Context, toolServer *ToolServer) (*ToolServer, error)
	StoreEvents(ctx context.Context, messages ...*Event) error

	// Delete methods
	DeleteSession(ctx context.Context, sessionID string, userID string) error
	DeleteAgent(ctx context.Context, agentID string) error
	DeleteToolServer(ctx context.Context, serverName string, groupKind string) error
	DeleteTask(ctx context.Context, taskID string, userID string) error
	DeletePushNotification(ctx context.Context, taskID string) error
	DeleteToolsForServer(ctx context.Context, serverName string, groupKind string) error

	// Get methods

	GetSession(ctx context.Context, sessionID string, userID string) (*Session, error)
	GetAgent(ctx context.Context, name string) (*Agent, error)
	GetTask(ctx context.Context, id string, userID string) (*a2a.Task, error)
	GetTool(ctx context.Context, name string) (*Tool, error)
	GetToolServer(ctx context.Context, name string) (*ToolServer, error)
	GetPushNotification(ctx context.Context, taskID string, configID string) (*a2a.PushConfig, error)

	// List methods
	ListTools(ctx context.Context) ([]Tool, error)
	ListFeedback(ctx context.Context, userID string) ([]Feedback, error)
	ListTasksForSession(ctx context.Context, sessionID string, userID string) ([]*a2a.Task, error)
	ListSessions(ctx context.Context, userID string) ([]Session, error)
	ListSessionsForAgent(ctx context.Context, agentID string, userID string) ([]SessionWithShareToken, error)
	ListSessionsForAgentAllUsers(ctx context.Context, agentID string) ([]Session, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	ListToolServers(ctx context.Context) ([]ToolServer, error)
	ListToolsForServer(ctx context.Context, serverName string, groupKind string) ([]Tool, error)
	ListEventsForSession(ctx context.Context, sessionID, userID string, options QueryOptions) ([]*Event, error)
	ListPushNotifications(ctx context.Context, taskID string) ([]*a2a.PushConfig, error)

	// Helper methods
	RefreshToolsForServer(ctx context.Context, serverName string, groupKind string, tools ...*v1alpha3.MCPTool) error

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

	// Session share methods
	CreateSessionShare(ctx context.Context, share *SessionShare) (*SessionShare, error)
	GetSessionShareByToken(ctx context.Context, token string) (*SessionShare, error)
	ListSessionSharesBySession(ctx context.Context, sessionID string) ([]SessionShare, error)
	DeleteSessionShare(ctx context.Context, token, sessionID, userID string) error
	RecordShareAccess(ctx context.Context, userID string, shareID int64) error

	// Agent memory (vector search) methods
	StoreAgentMemory(ctx context.Context, memory *Memory) error
	StoreAgentMemories(ctx context.Context, memories []*Memory) error
	SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error)
	ListAgentMemories(ctx context.Context, agentName, userID string) ([]Memory, error)
	DeleteAgentMemory(ctx context.Context, agentName, userID string) error
	PruneExpiredMemories(ctx context.Context) error

	// PruneExpiredSessions hard-deletes idle sessions older than retentionDays
	// (sliding window on updated_at) and cascaded conversation state. No-op when
	// retentionDays <= 0. Returns the number of sessions deleted.
	PruneExpiredSessions(ctx context.Context, retentionDays int) (int64, error)

	// AgentTemplate runtime revision methods
	UpsertAgentTemplateHarnessPair(context.Context, AgentTemplateHarnessPair) error
	UpsertRuntimeRevision(context.Context, RuntimeRevision) error
	GetRuntimeRevision(context.Context, string) (*RuntimeRevision, error)
	MarkRuntimeRevisionSuccessful(context.Context, AgentTemplateHarnessPair) error
	RetireAgentTemplateHarnessPairs(context.Context, string, string) error
	RetireAgentTemplateHarnessPair(context.Context, string, string, string) error
	RetireOtherAgentTemplateHarnessPairs(context.Context, string, string, []string) error
	ListUnreferencedRuntimeRevisions(context.Context) ([]RuntimeRevision, error)
	DeleteUnreferencedRuntimeRevision(context.Context, string) error

	// AgentInstance lifecycle methods
	CreateAgentInstance(context.Context, *apiv1alpha1.AgentInstance, string) (*apiv1alpha1.AgentInstance, bool, error)
	GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error)
	ListAgentInstances(context.Context, string, string, bool, map[string]string, string, int) ([]*apiv1alpha1.AgentInstance, error)
	MarkAgentInstanceReady(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	TransitionAgentInstance(context.Context, *apiv1alpha1.AgentInstance, apiv1alpha1.AgentInstanceState, apiv1alpha1.AgentInstanceOperation) (*apiv1alpha1.AgentInstance, error)
	DeleteAgentInstance(context.Context, string) error
	CreateAgentInstanceShare(context.Context, AgentInstanceShare) (*AgentInstanceShare, error)
	ListAgentInstanceShares(context.Context, string, string, string, string, int) ([]AgentInstanceShare, error)
	DeleteAgentInstanceShare(context.Context, string, string, string) error
	CreateAgentInstanceTask(context.Context, string, []byte, *a2a.Task) (*a2a.Task, bool, error)
	StoreAgentInstanceTaskEvent(context.Context, string, *a2a.Task, a2a.Event) error
	GetAgentInstanceTask(context.Context, string, string) (*a2a.Task, error)
	ListAgentInstanceTasks(context.Context, string, string, a2a.TaskState, *time.Time, int) ([]*a2a.Task, int, error)
}

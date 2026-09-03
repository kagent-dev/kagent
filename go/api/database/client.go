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

var ErrIdempotencyConflict = errors.New("request id was already used with different parameters")

var ErrAgentInstanceConflict = errors.New("AgentInstance lifecycle operation conflicts with its current state")

var ErrAgentInstanceTaskConflict = errors.New("AgentInstance already has an active task")

var ErrAgentInstanceNotQuiescent = errors.New("AgentInstance has no quiescent turn boundary")

type LangGraphCheckpointTuple struct {
	Checkpoint *LangGraphCheckpoint
	Writes     []*LangGraphCheckpointWrite
}

type Client interface {
	// Store methods
	StoreFeedback(ctx context.Context, feedback *Feedback) error
	StoreAgent(ctx context.Context, agent *Agent) error
	StoreToolServer(ctx context.Context, toolServer *ToolServer) (*ToolServer, error)

	// Delete methods
	DeleteAgent(ctx context.Context, agentID string) error
	DeleteToolServer(ctx context.Context, serverName string, groupKind string) error
	DeleteToolsForServer(ctx context.Context, serverName string, groupKind string) error

	// Get methods

	GetAgent(ctx context.Context, name string) (*Agent, error)
	GetTool(ctx context.Context, name string) (*Tool, error)
	GetToolServer(ctx context.Context, name string) (*ToolServer, error)

	// List methods
	ListTools(ctx context.Context) ([]Tool, error)
	ListFeedback(ctx context.Context, userID string) ([]Feedback, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	ListToolServers(ctx context.Context) ([]ToolServer, error)
	ListToolsForServer(ctx context.Context, serverName string, groupKind string) ([]Tool, error)

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

	// Agent memory (vector search) methods
	StoreAgentMemory(ctx context.Context, memory *Memory) error
	StoreAgentMemories(ctx context.Context, memories []*Memory) error
	SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error)
	ListAgentMemories(ctx context.Context, agentName, userID string) ([]Memory, error)
	DeleteAgentMemory(ctx context.Context, agentName, userID string) error
	PruneExpiredMemories(ctx context.Context) error

	// AgentTemplate runtime revision methods
	UpsertAgentTemplateHarnessPair(context.Context, AgentTemplateHarnessPair) error
	UpsertRuntimeRevision(context.Context, RuntimeRevision) error
	GetRuntimeRevision(context.Context, string) (*RuntimeRevision, error)
	ListActorTemplateHarnesses(context.Context) ([]ActorTemplateHarness, error)
	MarkRuntimeRevisionSuccessful(context.Context, AgentTemplateHarnessPair) error
	RetireAgentTemplateHarnessPairs(context.Context, string, string) error
	RetireAgentTemplateHarnessPair(context.Context, string, string, string) error
	RetireOtherAgentTemplateHarnessPairs(context.Context, string, string, []string) error
	ListUnreferencedRuntimeRevisions(context.Context) ([]RuntimeRevision, error)
	DeleteUnreferencedRuntimeRevision(context.Context, string) error

	// AgentInstance lifecycle methods
	CreateAgentInstance(context.Context, *apiv1alpha1.AgentInstance, string) (*apiv1alpha1.AgentInstance, bool, error)
	ForkAgentInstance(context.Context, string, string, string, string, string) (*apiv1alpha1.AgentInstance, bool, error)
	GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error)
	ListAgentInstances(context.Context, AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error)
	// UpdateAgentInstanceName sets the instance's display name, scoped to its owner.
	// Takes namespace, id, owner and the new name.
	UpdateAgentInstanceName(context.Context, string, string, string, string) (*apiv1alpha1.AgentInstance, error)
	MarkAgentInstanceReady(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	TransitionAgentInstance(context.Context, *apiv1alpha1.AgentInstance, apiv1alpha1.AgentInstanceState, apiv1alpha1.AgentInstanceOperation) (*apiv1alpha1.AgentInstance, error)
	DeleteAgentInstance(context.Context, string) error
	CreateAgentInstanceShare(context.Context, AgentInstanceShare) (*AgentInstanceShare, error)
	ListAgentInstanceShares(context.Context, string, string, string, string, int) ([]AgentInstanceShare, error)
	// GetAgentInstanceShareByTokenHash resolves a share token to its share and the
	// owner of the instance it grants access to. Takes the digest, because only the
	// digest is stored.
	GetAgentInstanceShareByTokenHash(context.Context, []byte) (*AgentInstanceShare, error)
	DeleteAgentInstanceShare(context.Context, string, string, string) error
	// CreateAgentInstanceTask reserves the instance's single active-task slot.
	CreateAgentInstanceTask(context.Context, string, []byte, *a2a.Task) (*a2a.Task, bool, error)
	GetActiveAgentInstanceTask(context.Context, string) (*a2a.Task, error)
	// InterruptActiveAgentInstanceTask fails the expected task and records an
	// interruption. It returns false if that task is no longer active.
	InterruptActiveAgentInstanceTask(context.Context, string, string) (bool, error)
	StoreAgentInstanceTaskEvent(context.Context, string, *a2a.Task, a2a.Event, *AgentInstanceTaskSnapshot) error
	GetAgentInstanceTask(context.Context, string, string) (*a2a.Task, error)
	ListAgentInstanceTasks(context.Context, string, string, a2a.TaskState, *time.Time, int) ([]*a2a.Task, int, error)
	ReserveAgentInstanceCheckpoint(context.Context, AgentInstanceCheckpoint) (*AgentInstanceCheckpoint, error)
	FinalizeAgentInstanceCheckpoint(context.Context, string, string, string) (*AgentInstanceCheckpoint, error)
	GetAgentInstanceCheckpoint(context.Context, string, string, string) (*AgentInstanceCheckpoint, error)
	ListAgentInstanceCheckpoints(context.Context, string, string, string, string, int) ([]AgentInstanceCheckpoint, error)
	BeginDeleteAgentInstanceCheckpoint(context.Context, string, string, string) (*AgentInstanceCheckpoint, error)
	DeleteAgentInstanceCheckpoint(context.Context, string, string, string) error
}

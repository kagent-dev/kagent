package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
	"github.com/pgvector/pgvector-go"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

type postgresClient struct {
	q  *dbgen.Queries
	db *pgxpool.Pool
}

func NewClient(db *pgxpool.Pool) dbpkg.Client {
	return &postgresClient{
		q:  dbgen.New(db),
		db: db,
	}
}

func (c *postgresClient) withTx(ctx context.Context, fn func(*dbgen.Queries) error) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(c.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── Agents ────────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreAgent(ctx context.Context, agent *dbpkg.Agent) error {
	return c.q.UpsertAgent(ctx, dbgen.UpsertAgentParams{
		ID:     agent.ID,
		Type:   agent.Type,
		Config: agent.Config,
	})
}

func (c *postgresClient) GetAgent(ctx context.Context, id string) (*dbpkg.Agent, error) {
	row, err := c.q.GetAgent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %s: %w", id, err)
	}
	return toAgent(row), nil
}

func (c *postgresClient) ListAgents(ctx context.Context) ([]dbpkg.Agent, error) {
	rows, err := c.q.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}
	agents := make([]dbpkg.Agent, len(rows))
	for i, r := range rows {
		agents[i] = *toAgent(r)
	}
	return agents, nil
}

func (c *postgresClient) DeleteAgent(ctx context.Context, agentID string) error {
	return c.q.SoftDeleteAgent(ctx, agentID)
}

// ── Sessions ──────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreSession(ctx context.Context, session *dbpkg.Session) error {
	params := dbgen.UpsertSessionParams{
		ID:      session.ID,
		UserID:  session.UserID,
		Name:    session.Name,
		AgentID: session.AgentID,
	}
	if session.Source != nil {
		src := string(*session.Source)
		params.Source = &src
	}
	return c.q.UpsertSession(ctx, params)
}

func (c *postgresClient) GetSession(ctx context.Context, sessionID, userID string) (*dbpkg.Session, error) {
	row, err := c.q.GetSession(ctx, dbgen.GetSessionParams{ID: sessionID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get session %s: %w", sessionID, err)
	}
	return toSession(row), nil
}

func (c *postgresClient) ListSessions(ctx context.Context, userID string) ([]dbpkg.Session, error) {
	rows, err := c.q.ListSessions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	sessions := make([]dbpkg.Session, len(rows))
	for i, r := range rows {
		sessions[i] = *toSession(r)
	}
	return sessions, nil
}

func (c *postgresClient) ListSessionsForAgent(ctx context.Context, agentID, userID string) ([]dbpkg.Session, error) {
	rows, err := c.q.ListSessionsForAgent(ctx, dbgen.ListSessionsForAgentParams{
		AgentID: &agentID,
		UserID:  userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions for agent: %w", err)
	}
	sessions := make([]dbpkg.Session, len(rows))
	for i, r := range rows {
		sessions[i] = *toSession(r)
	}
	return sessions, nil
}

func (c *postgresClient) DeleteSession(ctx context.Context, sessionID, userID string) error {
	return c.q.SoftDeleteSession(ctx, dbgen.SoftDeleteSessionParams{ID: sessionID, UserID: userID})
}

// ── Events ────────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreEvents(ctx context.Context, events ...*dbpkg.Event) error {
	for _, e := range events {
		if err := c.q.InsertEvent(ctx, dbgen.InsertEventParams{
			ID:        e.ID,
			UserID:    e.UserID,
			SessionID: strPtrIfNotEmpty(e.SessionID),
			Data:      e.Data,
		}); err != nil {
			return fmt.Errorf("failed to store event %s: %w", e.ID, err)
		}
	}
	return nil
}

func (c *postgresClient) ListEventsForSession(ctx context.Context, sessionID, userID string, opts dbpkg.QueryOptions) ([]*dbpkg.Event, error) {
	var rows []dbgen.Event
	var err error
	sessionIDPtr := strPtrIfNotEmpty(sessionID)

	switch {
	case opts.OrderAsc && opts.Limit > 0:
		rows, err = c.q.ListEventsForSessionAscLimit(ctx, dbgen.ListEventsForSessionAscLimitParams{
			SessionID: sessionIDPtr, UserID: userID, Column3: opts.After, Limit: int32(opts.Limit),
		})
	case opts.OrderAsc:
		rows, err = c.q.ListEventsForSessionAsc(ctx, dbgen.ListEventsForSessionAscParams{
			SessionID: sessionIDPtr, UserID: userID, Column3: opts.After,
		})
	case opts.Limit > 0:
		rows, err = c.q.ListEventsForSessionDescLimit(ctx, dbgen.ListEventsForSessionDescLimitParams{
			SessionID: sessionIDPtr, UserID: userID, Column3: opts.After, Limit: int32(opts.Limit),
		})
	default:
		rows, err = c.q.ListEventsForSessionDesc(ctx, dbgen.ListEventsForSessionDescParams{
			SessionID: sessionIDPtr, UserID: userID, Column3: opts.After,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list events for session: %w", err)
	}

	events := make([]*dbpkg.Event, len(rows))
	for i, r := range rows {
		events[i] = toEvent(r)
	}
	return events, nil
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreTask(ctx context.Context, task *protocol.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to serialize task: %w", err)
	}
	return c.q.UpsertTask(ctx, dbgen.UpsertTaskParams{
		ID:        task.ID,
		Data:      string(data),
		SessionID: strPtrIfNotEmpty(task.ContextID),
	})
}

func (c *postgresClient) GetTask(ctx context.Context, taskID string) (*protocol.Task, error) {
	row, err := c.q.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", taskID, err)
	}
	var task protocol.Task
	if err := json.Unmarshal([]byte(row.Data), &task); err != nil {
		return nil, fmt.Errorf("failed to deserialize task: %w", err)
	}
	return &task, nil
}

func (c *postgresClient) ListTasksForSession(ctx context.Context, sessionID string) ([]*protocol.Task, error) {
	rows, err := c.q.ListTasksForSession(ctx, &sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks for session: %w", err)
	}
	tasks := make([]dbpkg.Task, len(rows))
	for i, r := range rows {
		tasks[i] = *toTask(r)
	}
	return dbpkg.ParseTasks(tasks)
}

func (c *postgresClient) DeleteTask(ctx context.Context, taskID string) error {
	return c.q.SoftDeleteTask(ctx, taskID)
}

// ── Push Notifications ────────────────────────────────────────────────────────

func (c *postgresClient) StorePushNotification(ctx context.Context, config *protocol.TaskPushNotificationConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification: %w", err)
	}
	return c.q.UpsertPushNotification(ctx, dbgen.UpsertPushNotificationParams{
		ID:     config.PushNotificationConfig.ID,
		TaskID: config.TaskID,
		Data:   string(data),
	})
}

func (c *postgresClient) GetPushNotification(ctx context.Context, taskID, configID string) (*protocol.TaskPushNotificationConfig, error) {
	row, err := c.q.GetPushNotification(ctx, dbgen.GetPushNotificationParams{TaskID: taskID, ID: configID})
	if err != nil {
		return nil, fmt.Errorf("failed to get push notification: %w", err)
	}
	var cfg protocol.TaskPushNotificationConfig
	if err := json.Unmarshal([]byte(row.Data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to deserialize push notification: %w", err)
	}
	return &cfg, nil
}

func (c *postgresClient) ListPushNotifications(ctx context.Context, taskID string) ([]*protocol.TaskPushNotificationConfig, error) {
	rows, err := c.q.ListPushNotifications(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list push notifications: %w", err)
	}
	result := make([]*protocol.TaskPushNotificationConfig, 0, len(rows))
	for _, row := range rows {
		var cfg protocol.TaskPushNotificationConfig
		if err := json.Unmarshal([]byte(row.Data), &cfg); err != nil {
			return nil, fmt.Errorf("failed to deserialize push notification: %w", err)
		}
		result = append(result, &cfg)
	}
	return result, nil
}

func (c *postgresClient) DeletePushNotification(ctx context.Context, taskID string) error {
	return c.q.SoftDeletePushNotification(ctx, taskID)
}

// ── Feedback ──────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreFeedback(ctx context.Context, feedback *dbpkg.Feedback) error {
	isPositive := feedback.IsPositive
	_, err := c.q.InsertFeedback(ctx, dbgen.InsertFeedbackParams{
		UserID:       feedback.UserID,
		MessageID:    feedback.MessageID,
		IsPositive:   &isPositive,
		FeedbackText: feedback.FeedbackText,
		IssueType:    feedback.IssueType,
	})
	return err
}

func (c *postgresClient) ListFeedback(ctx context.Context, userID string) ([]dbpkg.Feedback, error) {
	rows, err := c.q.ListFeedback(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list feedback: %w", err)
	}
	result := make([]dbpkg.Feedback, len(rows))
	for i, r := range rows {
		result[i] = *toFeedback(r)
	}
	return result, nil
}

// ── Tools ─────────────────────────────────────────────────────────────────────

func (c *postgresClient) GetTool(ctx context.Context, name string) (*dbpkg.Tool, error) {
	row, err := c.q.GetTool(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool %s: %w", name, err)
	}
	return toTool(row), nil
}

func (c *postgresClient) ListTools(ctx context.Context) ([]dbpkg.Tool, error) {
	rows, err := c.q.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	tools := make([]dbpkg.Tool, len(rows))
	for i, r := range rows {
		tools[i] = *toTool(r)
	}
	return tools, nil
}

func (c *postgresClient) ListToolsForServer(ctx context.Context, serverName, groupKind string) ([]dbpkg.Tool, error) {
	rows, err := c.q.ListToolsForServer(ctx, dbgen.ListToolsForServerParams{ServerName: serverName, GroupKind: groupKind})
	if err != nil {
		return nil, fmt.Errorf("failed to list tools for server: %w", err)
	}
	tools := make([]dbpkg.Tool, len(rows))
	for i, r := range rows {
		tools[i] = *toTool(r)
	}
	return tools, nil
}

func (c *postgresClient) DeleteToolsForServer(ctx context.Context, serverName, groupKind string) error {
	return c.q.SoftDeleteToolsForServer(ctx, dbgen.SoftDeleteToolsForServerParams{ServerName: serverName, GroupKind: groupKind})
}

func (c *postgresClient) RefreshToolsForServer(ctx context.Context, serverName, groupKind string, tools ...*v1alpha2.MCPTool) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		if err := q.SoftDeleteToolsForServer(ctx, dbgen.SoftDeleteToolsForServerParams{
			ServerName: serverName, GroupKind: groupKind,
		}); err != nil {
			return fmt.Errorf("failed to delete existing tools: %w", err)
		}
		for _, tool := range tools {
			if err := q.UpsertTool(ctx, dbgen.UpsertToolParams{
				ID:          tool.Name,
				ServerName:  serverName,
				GroupKind:   groupKind,
				Description: &tool.Description,
			}); err != nil {
				return fmt.Errorf("failed to upsert tool %s: %w", tool.Name, err)
			}
		}
		return nil
	})
}

func (c *postgresClient) GetToolServer(ctx context.Context, name string) (*dbpkg.ToolServer, error) {
	row, err := c.q.GetToolServer(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool server %s: %w", name, err)
	}
	return toToolServer(row), nil
}

func (c *postgresClient) ListToolServers(ctx context.Context) ([]dbpkg.ToolServer, error) {
	rows, err := c.q.ListToolServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tool servers: %w", err)
	}
	servers := make([]dbpkg.ToolServer, len(rows))
	for i, r := range rows {
		servers[i] = *toToolServer(r)
	}
	return servers, nil
}

func (c *postgresClient) StoreToolServer(ctx context.Context, ts *dbpkg.ToolServer) (*dbpkg.ToolServer, error) {
	row, err := c.q.UpsertToolServer(ctx, dbgen.UpsertToolServerParams{
		Name:          ts.Name,
		GroupKind:     ts.GroupKind,
		Description:   &ts.Description,
		LastConnected: ts.LastConnected,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store tool server: %w", err)
	}
	return toToolServer(row), nil
}

func (c *postgresClient) DeleteToolServer(ctx context.Context, serverName, groupKind string) error {
	return c.q.SoftDeleteToolServer(ctx, dbgen.SoftDeleteToolServerParams{Name: serverName, GroupKind: groupKind})
}

// ── LangGraph Checkpoints ─────────────────────────────────────────────────────

func (c *postgresClient) StoreCheckpoint(ctx context.Context, cp *dbpkg.LangGraphCheckpoint) error {
	version := cp.Version
	return c.q.UpsertCheckpoint(ctx, dbgen.UpsertCheckpointParams{
		UserID:             cp.UserID,
		ThreadID:           cp.ThreadID,
		CheckpointNs:       cp.CheckpointNS,
		CheckpointID:       cp.CheckpointID,
		ParentCheckpointID: cp.ParentCheckpointID,
		Metadata:           cp.Metadata,
		Checkpoint:         cp.Checkpoint,
		CheckpointType:     cp.CheckpointType,
		Version:            &version,
	})
}

func (c *postgresClient) StoreCheckpointWrites(ctx context.Context, writes []*dbpkg.LangGraphCheckpointWrite) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		for _, w := range writes {
			if err := q.UpsertCheckpointWrite(ctx, dbgen.UpsertCheckpointWriteParams{
				UserID:       w.UserID,
				ThreadID:     w.ThreadID,
				CheckpointNs: w.CheckpointNS,
				CheckpointID: w.CheckpointID,
				WriteIdx:     w.WriteIdx,
				Value:        w.Value,
				ValueType:    w.ValueType,
				Channel:      w.Channel,
				TaskID:       w.TaskID,
			}); err != nil {
				return fmt.Errorf("failed to store checkpoint write: %w", err)
			}
		}
		return nil
	})
}

func (c *postgresClient) ListCheckpoints(ctx context.Context, userID, threadID, checkpointNS string, checkpointID *string, limit int) ([]*dbpkg.LangGraphCheckpointTuple, error) {
	var tuples []*dbpkg.LangGraphCheckpointTuple
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		var checkpoints []dbgen.LgCheckpoint
		var err error
		if checkpointID != nil {
			cp, err := q.GetCheckpoint(ctx, dbgen.GetCheckpointParams{
				UserID: userID, ThreadID: threadID, CheckpointNs: checkpointNS, CheckpointID: *checkpointID,
			})
			if err != nil {
				return fmt.Errorf("failed to get checkpoint: %w", err)
			}
			checkpoints = []dbgen.LgCheckpoint{cp}
		} else if limit > 0 {
			checkpoints, err = q.ListCheckpointsLimit(ctx, dbgen.ListCheckpointsLimitParams{
				UserID: userID, ThreadID: threadID, CheckpointNs: checkpointNS, Limit: int32(limit),
			})
			if err != nil {
				return fmt.Errorf("failed to list checkpoints: %w", err)
			}
		} else {
			checkpoints, err = q.ListCheckpoints(ctx, dbgen.ListCheckpointsParams{
				UserID: userID, ThreadID: threadID, CheckpointNs: checkpointNS,
			})
			if err != nil {
				return fmt.Errorf("failed to list checkpoints: %w", err)
			}
		}

		tuples = make([]*dbpkg.LangGraphCheckpointTuple, 0, len(checkpoints))
		for _, cp := range checkpoints {
			writes, err := q.ListCheckpointWrites(ctx, dbgen.ListCheckpointWritesParams{
				UserID: userID, ThreadID: threadID, CheckpointNs: checkpointNS, CheckpointID: cp.CheckpointID,
			})
			if err != nil {
				return fmt.Errorf("failed to get checkpoint writes: %w", err)
			}
			dbWrites := make([]*dbpkg.LangGraphCheckpointWrite, len(writes))
			for i, w := range writes {
				dbWrites[i] = toCheckpointWrite(w)
			}
			tuples = append(tuples, &dbpkg.LangGraphCheckpointTuple{
				Checkpoint: toCheckpoint(cp),
				Writes:     dbWrites,
			})
		}
		return nil
	})
	return tuples, err
}

func (c *postgresClient) DeleteCheckpoint(ctx context.Context, userID, threadID string) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		if err := q.SoftDeleteCheckpoints(ctx, dbgen.SoftDeleteCheckpointsParams{UserID: userID, ThreadID: threadID}); err != nil {
			return fmt.Errorf("failed to delete checkpoints: %w", err)
		}
		if err := q.SoftDeleteCheckpointWrites(ctx, dbgen.SoftDeleteCheckpointWritesParams{UserID: userID, ThreadID: threadID}); err != nil {
			return fmt.Errorf("failed to delete checkpoint writes: %w", err)
		}
		return nil
	})
}

// ── CrewAI ────────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreCrewAIMemory(ctx context.Context, memory *dbpkg.CrewAIAgentMemory) error {
	return c.q.UpsertCrewAIMemory(ctx, dbgen.UpsertCrewAIMemoryParams{
		UserID:     memory.UserID,
		ThreadID:   memory.ThreadID,
		MemoryData: memory.MemoryData,
	})
}

func (c *postgresClient) SearchCrewAIMemoryByTask(ctx context.Context, userID, threadID, taskDescription string, limit int) ([]*dbpkg.CrewAIAgentMemory, error) {
	pattern := "%" + taskDescription + "%"
	var rows []dbgen.CrewaiAgentMemory
	var err error

	if limit > 0 {
		rows, err = c.q.SearchCrewAIMemoryByTaskLimit(ctx, dbgen.SearchCrewAIMemoryByTaskLimitParams{
			UserID: userID, ThreadID: threadID, MemoryData: pattern, Limit: int32(limit),
		})
	} else {
		rows, err = c.q.SearchCrewAIMemoryByTask(ctx, dbgen.SearchCrewAIMemoryByTaskParams{
			UserID: userID, ThreadID: threadID, MemoryData: pattern,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to search CrewAI memory: %w", err)
	}

	result := make([]*dbpkg.CrewAIAgentMemory, len(rows))
	for i, r := range rows {
		result[i] = toCrewAIMemory(r)
	}
	return result, nil
}

func (c *postgresClient) ResetCrewAIMemory(ctx context.Context, userID, threadID string) error {
	return c.q.HardDeleteCrewAIMemory(ctx, dbgen.HardDeleteCrewAIMemoryParams{UserID: userID, ThreadID: threadID})
}

func (c *postgresClient) StoreCrewAIFlowState(ctx context.Context, state *dbpkg.CrewAIFlowState) error {
	return c.q.UpsertCrewAIFlowState(ctx, dbgen.UpsertCrewAIFlowStateParams{
		UserID:     state.UserID,
		ThreadID:   state.ThreadID,
		MethodName: state.MethodName,
		StateData:  state.StateData,
	})
}

func (c *postgresClient) GetCrewAIFlowState(ctx context.Context, userID, threadID string) (*dbpkg.CrewAIFlowState, error) {
	row, err := c.q.GetLatestCrewAIFlowState(ctx, dbgen.GetLatestCrewAIFlowStateParams{UserID: userID, ThreadID: threadID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get CrewAI flow state: %w", err)
	}
	return toCrewAIFlowState(row), nil
}

// ── Agent Memory (vector search) ──────────────────────────────────────────────

func (c *postgresClient) StoreAgentMemory(ctx context.Context, memory *dbpkg.Memory) error {
	id, err := c.q.InsertMemory(ctx, dbgen.InsertMemoryParams{
		AgentName:   &memory.AgentName,
		UserID:      &memory.UserID,
		Content:     &memory.Content,
		Embedding:   memory.Embedding,
		Metadata:    &memory.Metadata,
		ExpiresAt:   memory.ExpiresAt,
		AccessCount: &memory.AccessCount,
	})
	if err != nil {
		return err
	}
	memory.ID = id
	return nil
}

func (c *postgresClient) StoreAgentMemories(ctx context.Context, memories []*dbpkg.Memory) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		for _, m := range memories {
			id, err := q.InsertMemory(ctx, dbgen.InsertMemoryParams{
				AgentName:   &m.AgentName,
				UserID:      &m.UserID,
				Content:     &m.Content,
				Embedding:   m.Embedding,
				Metadata:    &m.Metadata,
				ExpiresAt:   m.ExpiresAt,
				AccessCount: &m.AccessCount,
			})
			if err != nil {
				return fmt.Errorf("failed to store memory: %w", err)
			}
			m.ID = id
		}
		return nil
	})
}

func (c *postgresClient) SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]dbpkg.AgentMemorySearchResult, error) {
	rows, err := c.q.SearchAgentMemory(ctx, dbgen.SearchAgentMemoryParams{
		Embedding: embedding,
		AgentName: &agentName,
		UserID:    &userID,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search agent memory: %w", err)
	}

	results := make([]dbpkg.AgentMemorySearchResult, len(rows))
	for i, r := range rows {
		score, _ := r.Score.(float64)
		results[i] = dbpkg.AgentMemorySearchResult{
			Memory: dbpkg.Memory{
				ID:          r.ID,
				AgentName:   derefStr(r.AgentName),
				UserID:      derefStr(r.UserID),
				Content:     derefStr(r.Content),
				Embedding:   r.Embedding,
				Metadata:    derefStr(r.Metadata),
				CreatedAt:   r.CreatedAt,
				ExpiresAt:   r.ExpiresAt,
				AccessCount: derefInt32(r.AccessCount),
			},
			Score: score,
		}
	}

	if len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		if err := c.q.IncrementMemoryAccessCount(ctx, ids); err != nil {
			return nil, fmt.Errorf("failed to increment access count: %w", err)
		}
	}

	return results, nil
}

func (c *postgresClient) ListAgentMemories(ctx context.Context, agentName, userID string) ([]dbpkg.Memory, error) {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	rows, err := c.q.ListAgentMemories(ctx, dbgen.ListAgentMemoriesParams{
		AgentName:   &agentName,
		AgentName_2: &normalized,
		UserID:      &userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list agent memories: %w", err)
	}
	memories := make([]dbpkg.Memory, len(rows))
	for i, r := range rows {
		memories[i] = *toMemory(r)
	}
	return memories, nil
}

func (c *postgresClient) DeleteAgentMemory(ctx context.Context, agentName, userID string) error {
	if err := c.q.DeleteAgentMemory(ctx, dbgen.DeleteAgentMemoryParams{
		AgentName: &agentName,
		UserID:    &userID,
	}); err != nil {
		return fmt.Errorf("failed to delete agent memory: %w", err)
	}
	normalized := strings.ReplaceAll(agentName, "-", "_")
	if normalized != agentName {
		if err := c.q.DeleteAgentMemory(ctx, dbgen.DeleteAgentMemoryParams{
			AgentName: &normalized,
			UserID:    &userID,
		}); err != nil {
			return fmt.Errorf("failed to delete normalized agent memory: %w", err)
		}
	}
	return nil
}

func (c *postgresClient) PruneExpiredMemories(ctx context.Context) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		if err := q.ExtendMemoryTTL(ctx); err != nil {
			return fmt.Errorf("failed to extend TTL for popular memories: %w", err)
		}
		if err := q.DeleteExpiredMemories(ctx); err != nil {
			return fmt.Errorf("failed to delete expired memories: %w", err)
		}
		return nil
	})
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func toAgent(r dbgen.Agent) *dbpkg.Agent {
	return &dbpkg.Agent{
		ID:        r.ID,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		DeletedAt: r.DeletedAt,
		Type:      r.Type,
		Config:    r.Config,
	}
}

func toSession(r dbgen.Session) *dbpkg.Session {
	s := &dbpkg.Session{
		ID:        r.ID,
		UserID:    r.UserID,
		Name:      r.Name,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		DeletedAt: r.DeletedAt,
		AgentID:   r.AgentID,
	}
	if r.Source != nil {
		src := dbpkg.SessionSource(*r.Source)
		s.Source = &src
	}
	return s
}

func toEvent(r dbgen.Event) *dbpkg.Event {
	return &dbpkg.Event{
		ID:        r.ID,
		UserID:    r.UserID,
		SessionID: derefStr(r.SessionID),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		DeletedAt: r.DeletedAt,
		Data:      r.Data,
	}
}

func toTask(r dbgen.Task) *dbpkg.Task {
	return &dbpkg.Task{
		ID:        r.ID,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		DeletedAt: r.DeletedAt,
		Data:      r.Data,
		SessionID: derefStr(r.SessionID),
	}
}

func toFeedback(r dbgen.Feedback) *dbpkg.Feedback {
	return &dbpkg.Feedback{
		ID:           r.ID,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    r.DeletedAt,
		UserID:       r.UserID,
		MessageID:    r.MessageID,
		IsPositive:   derefBool(r.IsPositive),
		FeedbackText: r.FeedbackText,
		IssueType:    r.IssueType,
	}
}

func toTool(r dbgen.Tool) *dbpkg.Tool {
	return &dbpkg.Tool{
		ID:          r.ID,
		ServerName:  r.ServerName,
		GroupKind:   r.GroupKind,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		DeletedAt:   r.DeletedAt,
		Description: derefStr(r.Description),
	}
}

func toToolServer(r dbgen.Toolserver) *dbpkg.ToolServer {
	return &dbpkg.ToolServer{
		Name:          r.Name,
		GroupKind:     r.GroupKind,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		DeletedAt:     r.DeletedAt,
		Description:   derefStr(r.Description),
		LastConnected: r.LastConnected,
	}
}

func toCheckpoint(r dbgen.LgCheckpoint) *dbpkg.LangGraphCheckpoint {
	return &dbpkg.LangGraphCheckpoint{
		UserID:             r.UserID,
		ThreadID:           r.ThreadID,
		CheckpointNS:       r.CheckpointNs,
		CheckpointID:       r.CheckpointID,
		ParentCheckpointID: r.ParentCheckpointID,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
		DeletedAt:          r.DeletedAt,
		Metadata:           r.Metadata,
		Checkpoint:         r.Checkpoint,
		CheckpointType:     r.CheckpointType,
		Version:            derefInt32(r.Version),
	}
}

func toCheckpointWrite(r dbgen.LgCheckpointWrite) *dbpkg.LangGraphCheckpointWrite {
	return &dbpkg.LangGraphCheckpointWrite{
		UserID:       r.UserID,
		ThreadID:     r.ThreadID,
		CheckpointNS: r.CheckpointNs,
		CheckpointID: r.CheckpointID,
		WriteIdx:     r.WriteIdx,
		Value:        r.Value,
		ValueType:    r.ValueType,
		Channel:      r.Channel,
		TaskID:       r.TaskID,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    r.DeletedAt,
	}
}

func toCrewAIMemory(r dbgen.CrewaiAgentMemory) *dbpkg.CrewAIAgentMemory {
	return &dbpkg.CrewAIAgentMemory{
		UserID:     r.UserID,
		ThreadID:   r.ThreadID,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
		DeletedAt:  r.DeletedAt,
		MemoryData: r.MemoryData,
	}
}

func toCrewAIFlowState(r dbgen.CrewaiFlowState) *dbpkg.CrewAIFlowState {
	return &dbpkg.CrewAIFlowState{
		UserID:     r.UserID,
		ThreadID:   r.ThreadID,
		MethodName: r.MethodName,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
		DeletedAt:  r.DeletedAt,
		StateData:  r.StateData,
	}
}

func toMemory(r dbgen.Memory) *dbpkg.Memory {
	return &dbpkg.Memory{
		ID:          r.ID,
		AgentName:   derefStr(r.AgentName),
		UserID:      derefStr(r.UserID),
		Content:     derefStr(r.Content),
		Embedding:   r.Embedding,
		Metadata:    derefStr(r.Metadata),
		CreatedAt:   r.CreatedAt,
		ExpiresAt:   r.ExpiresAt,
		AccessCount: derefInt32(r.AccessCount),
	}
}

// ── Pointer helpers ───────────────────────────────────────────────────────────

func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

func derefInt32(n *int32) int32 {
	if n != nil {
		return *n
	}
	return 0
}

func derefBool(b *bool) bool {
	if b != nil {
		return *b
	}
	return false
}

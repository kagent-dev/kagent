package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/gen"
	"github.com/pgvector/pgvector-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		ID:           agent.ID,
		Type:         agent.Type,
		WorkloadType: string(agent.WorkloadType),
		Config:       agent.Config,
	})
}

// notFoundOr maps the driver's no-rows error to dbpkg.ErrNotFound so callers
// outside this package match on the exported sentinel, never on pgx.
func notFoundOr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return dbpkg.ErrNotFound
	}
	return err
}

func (c *postgresClient) GetAgent(ctx context.Context, id string) (*dbpkg.Agent, error) {
	row, err := c.q.GetAgent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %s: %w", id, notFoundOr(err))
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
	return c.withTx(ctx, func(q *dbgen.Queries) error {
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
		return q.UpsertSession(ctx, params)
	})
}

func (c *postgresClient) GetSession(ctx context.Context, sessionID, userID string) (*dbpkg.Session, error) {
	row, err := c.q.GetSession(ctx, dbgen.GetSessionParams{ID: sessionID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get session %s: %w", sessionID, notFoundOr(err))
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

func (c *postgresClient) ListSessionsForAgent(ctx context.Context, agentID, userID string) ([]dbpkg.SessionWithShareToken, error) {
	rows, err := c.q.ListSessionsForAgent(ctx, dbgen.ListSessionsForAgentParams{
		AgentID: &agentID,
		UserID:  userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions for agent: %w", err)
	}
	sessions := make([]dbpkg.SessionWithShareToken, len(rows))
	for i, r := range rows {
		sessions[i] = toSessionWithShareToken(r)
	}
	return sessions, nil
}

func (c *postgresClient) ListSessionsForAgentAllUsers(ctx context.Context, agentID string) ([]dbpkg.Session, error) {
	rows, err := c.q.ListSessionsForAgentAllUsers(ctx, &agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions for agent across all users: %w", err)
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

// ── Session Shares ─────────────────────────────────────────────────────────────

func toSessionShare(row dbgen.SessionShare) dbpkg.SessionShare {
	return dbpkg.SessionShare{
		ID:        row.ID,
		Token:     row.Token,
		SessionID: row.SessionID,
		UserID:    row.UserID,
		ReadOnly:  row.ReadOnly,
		CreatedAt: row.CreatedAt.Time,
	}
}

func (c *postgresClient) CreateSessionShare(ctx context.Context, share *dbpkg.SessionShare) (*dbpkg.SessionShare, error) {
	row, err := c.q.CreateSessionShare(ctx, dbgen.CreateSessionShareParams{
		Token:     share.Token,
		SessionID: share.SessionID,
		UserID:    share.UserID,
		ReadOnly:  share.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("create session share: %w", err)
	}
	result := toSessionShare(row)
	return &result, nil
}

func (c *postgresClient) GetSessionShareByToken(ctx context.Context, token string) (*dbpkg.SessionShare, error) {
	row, err := c.q.GetSessionShareByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("get session share by token: %w", notFoundOr(err))
	}
	result := toSessionShare(row)
	return &result, nil
}

func (c *postgresClient) ListSessionSharesBySession(ctx context.Context, sessionID string) ([]dbpkg.SessionShare, error) {
	rows, err := c.q.ListSessionSharesBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session shares by session: %w", err)
	}
	shares := make([]dbpkg.SessionShare, 0, len(rows))
	for _, row := range rows {
		shares = append(shares, toSessionShare(row))
	}
	return shares, nil
}

func (c *postgresClient) DeleteSessionShare(ctx context.Context, token, sessionID, userID string) error {
	if err := c.q.DeleteSessionShare(ctx, dbgen.DeleteSessionShareParams{
		Token:     token,
		SessionID: sessionID,
		UserID:    userID,
	}); err != nil {
		return fmt.Errorf("delete session share: %w", err)
	}
	return nil
}

func (c *postgresClient) RecordShareAccess(ctx context.Context, userID string, shareID int64) error {
	if err := c.q.UpsertShareAccess(ctx, dbgen.UpsertShareAccessParams{
		UserID:  userID,
		ShareID: shareID,
	}); err != nil {
		return fmt.Errorf("record share access: %w", err)
	}
	return nil
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

func (c *postgresClient) StoreTask(ctx context.Context, task *a2a.Task, userID string) error {
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to serialize task: %w", err)
	}
	protocolVersion := string(a2a.Version)
	// UpsertTask returns no rows when the write was rejected: the id belongs
	// to another user, or to a soft-deleted task (deleted ids stay burned).
	if _, err := c.q.UpsertTask(ctx, dbgen.UpsertTaskParams{
		ID:              string(task.ID),
		Data:            string(data),
		SessionID:       strPtrIfNotEmpty(task.ContextID),
		ProtocolVersion: &protocolVersion,
		UserID:          &userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dbpkg.ErrTaskOwnedByAnotherUser
		}
		return fmt.Errorf("failed to store task %s: %w", task.ID, err)
	}
	return nil
}

// checkTaskOwner returns ErrTaskOwnedByAnotherUser if taskID still exists but
// does not belong to userID after an owner-scoped write. A NULL owner counts
// as foreign too: a successful write always stamps the caller's user_id, so a
// surviving NULL means the write was rejected. A missing task is not an
// error: the caller decides what that means (nothing to own yet, or already
// deleted).
func (c *postgresClient) checkTaskOwner(ctx context.Context, taskID, userID string) error {
	owner, err := c.q.GetTaskOwner(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("failed to check task owner for %s: %w", taskID, err)
	}
	if owner == nil || *owner != userID {
		return dbpkg.ErrTaskOwnedByAnotherUser
	}
	return nil
}

func (c *postgresClient) GetTask(ctx context.Context, taskID, userID string) (*a2a.Task, error) {
	row, err := c.q.GetTask(ctx, dbgen.GetTaskParams{ID: taskID, UserID: &userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get task %s: %w", taskID, notFoundOr(err))
	}
	return parseVersionedTask(row.Data, row.ProtocolVersion)
}

func (c *postgresClient) ListTasksForSession(ctx context.Context, sessionID, userID string) ([]*a2a.Task, error) {
	rows, err := c.q.ListTasksForSession(ctx, dbgen.ListTasksForSessionParams{SessionID: &sessionID, UserID: &userID})
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks for session: %w", err)
	}
	tasks := make([]*a2a.Task, 0, len(rows))
	for i, r := range rows {
		task, err := parseVersionedTask(r.Data, r.ProtocolVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to parse task row %d: %w", i, err)
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (c *postgresClient) DeleteTask(ctx context.Context, taskID, userID string) error {
	if err := c.q.SoftDeleteTask(ctx, dbgen.SoftDeleteTaskParams{ID: taskID, UserID: &userID}); err != nil {
		return fmt.Errorf("failed to delete task %s: %w", taskID, err)
	}
	// The delete only takes effect if user_id matched; if the task is still
	// there and owned by someone else, say so instead of a misleading success.
	return c.checkTaskOwner(ctx, taskID, userID)
}

// ── AgentTemplate runtime revisions ──────────────────────────────────────────

func (c *postgresClient) UpsertAgentTemplateHarnessPair(ctx context.Context, pair dbpkg.AgentTemplateHarnessPair) error {
	if pair.AgentTemplateLabels == nil {
		pair.AgentTemplateLabels = map[string]string{}
	}
	labels, err := json.Marshal(pair.AgentTemplateLabels)
	if err != nil {
		return fmt.Errorf("marshal AgentTemplate labels: %w", err)
	}
	return c.q.UpsertAgentTemplateHarnessPair(ctx, dbgen.UpsertAgentTemplateHarnessPairParams{
		Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName,
		AgentTemplateUid: pair.AgentTemplateUID, HarnessName: pair.HarnessName,
		HarnessUid: pair.HarnessUID, DesiredRevision: pair.DesiredRevision,
		AgentTemplateLabels: labels,
	})
}

func (c *postgresClient) UpsertRuntimeRevision(ctx context.Context, revision dbpkg.RuntimeRevision) error {
	if err := c.q.UpsertRuntimeRevision(ctx, dbgen.UpsertRuntimeRevisionParams{
		Revision: revision.Revision, Namespace: revision.Namespace,
		AgentTemplateName: revision.AgentTemplateName, AgentTemplateUid: revision.AgentTemplateUID,
		HarnessName: revision.HarnessName, HarnessUid: revision.HarnessUID,
		SourceSnapshot: revision.SourceSnapshot, AgentCard: revision.AgentCard,
		EgressDestinations:     revision.EgressDestinations,
		ActorTemplateNamespace: revision.ActorTemplateNamespace, ActorTemplateName: revision.ActorTemplateName,
		ActorTemplateUid: revision.ActorTemplateUID, Phase: revision.Phase, GoldenSnapshot: revision.GoldenSnapshot,
	}); err != nil {
		return fmt.Errorf("upsert runtime revision %s: %w", revision.Revision, err)
	}
	return nil
}

func (c *postgresClient) GetRuntimeRevision(ctx context.Context, revision string) (*dbpkg.RuntimeRevision, error) {
	row, err := c.q.GetRuntimeRevision(ctx, revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime revision %s: %w", revision, notFoundOr(err))
	}
	return &dbpkg.RuntimeRevision{
		Revision: row.Revision, Namespace: row.Namespace,
		AgentTemplateName: row.AgentTemplateName, AgentTemplateUID: row.AgentTemplateUid,
		HarnessName: row.HarnessName, HarnessUID: row.HarnessUid,
		SourceSnapshot: row.SourceSnapshot, AgentCard: row.AgentCard,
		EgressDestinations:     row.EgressDestinations,
		ActorTemplateNamespace: row.ActorTemplateNamespace, ActorTemplateName: row.ActorTemplateName,
		ActorTemplateUID: row.ActorTemplateUid, Phase: row.Phase, GoldenSnapshot: row.GoldenSnapshot,
	}, nil
}

func (c *postgresClient) MarkRuntimeRevisionSuccessful(ctx context.Context, pair dbpkg.AgentTemplateHarnessPair) error {
	revision := pair.DesiredRevision
	return c.q.MarkRuntimeRevisionSuccessful(ctx, dbgen.MarkRuntimeRevisionSuccessfulParams{
		Revision: &revision, Namespace: pair.Namespace,
		AgentTemplateUid: pair.AgentTemplateUID, HarnessUid: pair.HarnessUID,
	})
}

func (c *postgresClient) RetireAgentTemplateHarnessPairs(ctx context.Context, namespace, name string) error {
	return c.q.RetireAgentTemplateHarnessPairs(ctx, dbgen.RetireAgentTemplateHarnessPairsParams{Namespace: namespace, AgentTemplateName: name})
}

func (c *postgresClient) RetireAgentTemplateHarnessPair(ctx context.Context, namespace, template, harness string) error {
	return c.q.RetireAgentTemplateHarnessPair(ctx, dbgen.RetireAgentTemplateHarnessPairParams{Namespace: namespace, AgentTemplateName: template, HarnessName: harness})
}

func (c *postgresClient) RetireOtherAgentTemplateHarnessPairs(ctx context.Context, namespace, templateUID string, harnesses []string) error {
	return c.q.RetireOtherAgentTemplateHarnessPairs(ctx, dbgen.RetireOtherAgentTemplateHarnessPairsParams{
		Namespace: namespace, AgentTemplateUid: templateUID, HarnessNames: harnesses,
	})
}

func (c *postgresClient) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]dbpkg.RuntimeRevision, error) {
	rows, err := c.q.ListUnreferencedRuntimeRevisions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unreferenced runtime revisions: %w", err)
	}
	result := make([]dbpkg.RuntimeRevision, 0, len(rows))
	for _, row := range rows {
		result = append(result, dbpkg.RuntimeRevision{
			Revision: row.Revision, Namespace: row.Namespace,
			AgentTemplateName: row.AgentTemplateName, AgentTemplateUID: row.AgentTemplateUid,
			HarnessName: row.HarnessName, HarnessUID: row.HarnessUid,
			SourceSnapshot: row.SourceSnapshot, AgentCard: row.AgentCard,
			EgressDestinations:     row.EgressDestinations,
			ActorTemplateNamespace: row.ActorTemplateNamespace, ActorTemplateName: row.ActorTemplateName,
			ActorTemplateUID: row.ActorTemplateUid, Phase: row.Phase, GoldenSnapshot: row.GoldenSnapshot,
		})
	}
	return result, nil
}

func (c *postgresClient) DeleteUnreferencedRuntimeRevision(ctx context.Context, revision string) error {
	return c.q.DeleteUnreferencedRuntimeRevision(ctx, revision)
}

// ── AgentInstances ───────────────────────────────────────────────────────────

func toAgentInstance(row dbgen.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	instance := &apiv1alpha1.AgentInstance{}
	if err := proto.Unmarshal(row.Data, instance); err != nil {
		return nil, fmt.Errorf("decode AgentInstance %s: %w", row.ID, err)
	}
	state, ok := apiv1alpha1.AgentInstanceState_value["AGENT_INSTANCE_STATE_"+row.State]
	if !ok {
		return nil, fmt.Errorf("decode AgentInstance %s state %q", row.ID, row.State)
	}
	operation := row.Operation
	if operation == "NONE" {
		operation = "UNSPECIFIED"
	}
	operationValue, ok := apiv1alpha1.AgentInstanceOperation_value["AGENT_INSTANCE_OPERATION_"+operation]
	if !ok {
		return nil, fmt.Errorf("decode AgentInstance %s operation %q", row.ID, row.Operation)
	}
	// These indexed columns are the lifecycle source of truth. In particular,
	// migrations can backfill them without rewriting the protobuf blob.
	instance.State = apiv1alpha1.AgentInstanceState(state)
	instance.Operation = apiv1alpha1.AgentInstanceOperation(operationValue)
	// The name is a column for the same reason, and because a rename writes only
	// the column: reading it from the blob would serve the name the row was
	// created with for the rest of the instance's life.
	instance.Name = row.Name
	return instance, nil
}

func marshalAgentInstance(instance *apiv1alpha1.AgentInstance) ([]byte, error) {
	data, err := proto.Marshal(instance)
	if err != nil {
		return nil, fmt.Errorf("encode AgentInstance %s: %w", instance.GetId(), err)
	}
	return data, nil
}

func sameAgentInstanceRequest(instance, request *apiv1alpha1.AgentInstance) bool {
	return instance.GetHarness().GetName() == request.GetHarness().GetName() && instance.GetAgentTemplate().GetName() == request.GetAgentTemplate().GetName()
}

func (c *postgresClient) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.AgentInstance, requestID string) (*apiv1alpha1.AgentInstance, bool, error) {
	requestKey := dbgen.GetAgentInstanceByRequestParams{
		UserID: request.GetCreator(), Namespace: request.GetNamespace(), RequestID: requestID,
	}
	existing, err := c.q.GetAgentInstanceByRequest(ctx, requestKey)
	if err == nil {
		instance, err := toAgentInstance(existing)
		if err == nil && !sameAgentInstanceRequest(instance, request) {
			return nil, false, dbpkg.ErrIdempotencyConflict
		}
		return instance, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get AgentInstance request: %w", err)
	}

	revision, err := c.q.GetLatestRuntimeRevisionForInstance(ctx, dbgen.GetLatestRuntimeRevisionForInstanceParams{
		Namespace: request.GetNamespace(), AgentTemplateName: request.GetAgentTemplate().GetName(), HarnessName: request.GetHarness().GetName(),
	})
	if err != nil {
		return nil, false, fmt.Errorf("get latest successful runtime revision: %w", notFoundOr(err))
	}
	labels := map[string]string{}
	if err := json.Unmarshal(revision.AgentTemplateLabels, &labels); err != nil {
		return nil, false, fmt.Errorf("decode AgentTemplate labels: %w", err)
	}

	now := timestamppb.Now()
	instance := proto.Clone(request).(*apiv1alpha1.AgentInstance)
	instance.PreparedRevision = revision.Revision
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE
	instance.Labels = labels
	instance.CreatedAt = now
	instance.UpdatedAt = now
	data, err := marshalAgentInstance(instance)
	if err != nil {
		return nil, false, err
	}
	var row dbgen.AgentInstance
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		if err := q.InsertA2AContext(ctx, dbgen.InsertA2AContextParams{
			ID: request.GetId(), Namespace: request.GetNamespace(), UserID: request.GetCreator(),
		}); err != nil {
			return fmt.Errorf("insert A2A context: %w", err)
		}
		row, err = q.InsertAgentInstance(ctx, dbgen.InsertAgentInstanceParams{
			ID: request.GetId(), Namespace: request.GetNamespace(), UserID: request.GetCreator(), RequestID: requestID,
			ContextID: request.GetId(), PreparedRevision: &revision.Revision, Labels: revision.AgentTemplateLabels,
			Name: request.GetName(), Data: data,
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = c.q.GetAgentInstanceByRequest(ctx, requestKey)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent AgentInstance request: %w", err)
		}
		instance, err = toAgentInstance(existing)
		if err == nil && !sameAgentInstanceRequest(instance, request) {
			return nil, false, dbpkg.ErrIdempotencyConflict
		}
		return instance, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert AgentInstance: %w", err)
	}
	instance, err = toAgentInstance(row)
	return instance, err == nil, err
}

func (c *postgresClient) ForkAgentInstance(ctx context.Context, namespace, checkpointID, userID, requestID, instanceID string) (*apiv1alpha1.AgentInstance, bool, error) {
	requestKey := dbgen.GetAgentInstanceByRequestParams{UserID: userID, Namespace: namespace, RequestID: requestID}
	existing, err := c.q.GetAgentInstanceByRequest(ctx, requestKey)
	if err == nil {
		if derefStr(existing.SourceCheckpointID) != checkpointID {
			return nil, false, dbpkg.ErrIdempotencyConflict
		}
		instance, err := toAgentInstance(existing)
		return instance, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get fork request: %w", err)
	}

	var row dbgen.AgentInstance
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		checkpoint, err := q.LockReadyAgentInstanceCheckpoint(ctx, dbgen.LockReadyAgentInstanceCheckpointParams{
			Namespace: namespace, ID: checkpointID, UserID: userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return dbpkg.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock checkpoint: %w", err)
		}
		if checkpoint.PreparedRevision == nil {
			return fmt.Errorf("checkpoint %s has no fork source", checkpointID)
		}

		revision, err := q.GetRuntimeRevision(ctx, *checkpoint.PreparedRevision)
		if err != nil {
			return fmt.Errorf("get checkpoint runtime revision: %w", err)
		}
		labels := map[string]string{}
		if err := json.Unmarshal(checkpoint.SourceLabels, &labels); err != nil {
			return fmt.Errorf("decode checkpoint labels: %w", err)
		}
		now := timestamppb.Now()
		instance := &apiv1alpha1.AgentInstance{
			Id: instanceID, Namespace: namespace, Creator: userID,
			Harness:          &apiv1alpha1.ResourceReference{Namespace: revision.Namespace, Name: revision.HarnessName},
			AgentTemplate:    &apiv1alpha1.ResourceReference{Namespace: revision.Namespace, Name: revision.AgentTemplateName},
			PreparedRevision: *checkpoint.PreparedRevision,
			State:            apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING,
			Operation:        apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE,
			CreatedAt:        now, UpdatedAt: now, Labels: labels,
		}
		data, err := marshalAgentInstance(instance)
		if err != nil {
			return err
		}
		encodedLabels, err := json.Marshal(labels)
		if err != nil {
			return fmt.Errorf("encode fork labels: %w", err)
		}
		if err := q.InsertA2AContext(ctx, dbgen.InsertA2AContextParams{ID: instanceID, Namespace: namespace, UserID: userID}); err != nil {
			return fmt.Errorf("insert fork A2A context: %w", err)
		}
		row, err = q.InsertForkedAgentInstance(ctx, dbgen.InsertForkedAgentInstanceParams{
			ID: instanceID, Namespace: namespace, UserID: userID, RequestID: requestID,
			ContextID: instanceID, PreparedRevision: checkpoint.PreparedRevision,
			SourceCheckpointID: &checkpoint.ID, Labels: encodedLabels, Data: data,
		})
		if err != nil {
			return err
		}

		tasks, err := q.ListAgentInstanceCheckpointTasks(ctx, checkpoint.ID)
		if err != nil {
			return fmt.Errorf("list checkpoint tasks: %w", err)
		}
		ids := newForkIDs(instanceID)
		var copiedHistorySequence int64
		for _, source := range tasks {
			task, err := unmarshalAgentInstanceTask(source.Data)
			if err != nil {
				return fmt.Errorf("decode checkpoint task %s: %w", source.ID, err)
			}
			reidentifyForkTask(task, instanceID, ids)
			if len(task.History) > 0 {
				copiedHistorySequence, err = storeAgentInstanceTaskMessages(ctx, q, instanceID, string(task.ID), task.History)
				if err != nil {
					return fmt.Errorf("copy checkpoint task %s history: %w", source.ID, err)
				}
			}
			data, err := marshalAgentInstanceTask(task)
			if err != nil {
				return fmt.Errorf("encode fork task %s: %w", task.ID, err)
			}
			copy := dbgen.InsertCopiedAgentInstanceTaskParams{
				ContextID: instanceID, ID: string(task.ID), State: string(task.Status.State),
				StatusTimestamp: task.Status.Timestamp, Data: data,
				CreatedAt: source.CreatedAt, UpdatedAt: source.UpdatedAt,
			}
			if err := q.InsertCopiedAgentInstanceTask(ctx, copy); err != nil {
				return fmt.Errorf("copy checkpoint task %s: %w", source.ID, err)
			}
		}
		events, err := q.ListAgentInstanceCheckpointEvents(ctx, checkpoint.ID)
		if err != nil {
			return fmt.Errorf("list checkpoint events: %w", err)
		}
		for _, source := range events {
			event, err := unmarshalAgentInstanceTaskEvent(source.Data)
			if err != nil {
				return fmt.Errorf("decode checkpoint event %d: %w", source.Sequence, err)
			}
			reidentifyForkEvent(event, derefStr(source.TaskID), instanceID, ids)
			data, err := marshalAgentInstanceTaskEvent(event)
			if err != nil {
				return fmt.Errorf("encode checkpoint event %d: %w", source.Sequence, err)
			}
			messageID := source.MessageID
			if messageID != nil {
				mapped := ids.message(*messageID)
				messageID = &mapped
			}
			copiedHistorySequence, err = q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
				ContextID: instanceID, TaskID: strPtrIfNotEmpty(string(event.TaskInfo().TaskID)),
				MessageID: messageID, Data: data,
			})
			if err != nil {
				return fmt.Errorf("copy checkpoint event %d: %w", source.Sequence, err)
			}
		}
		if copiedHistorySequence == 0 {
			return fmt.Errorf("checkpoint %s has no history events", checkpointID)
		}
		headID := string(ids.task(a2a.TaskID(checkpoint.HeadTaskID)))
		if err := q.SetAgentInstanceTaskSnapshot(ctx, dbgen.SetAgentInstanceTaskSnapshotParams{
			ContextID: instanceID, ID: headID, SnapshotAtespace: &checkpoint.SnapshotAtespace,
			SnapshotName: &checkpoint.SnapshotName, SnapshotUid: &checkpoint.SnapshotUid,
			SnapshotContentScope: &checkpoint.SnapshotContentScope, HistorySequence: &copiedHistorySequence,
		}); err != nil {
			return fmt.Errorf("store fork history boundary: %w", err)
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = c.q.GetAgentInstanceByRequest(ctx, requestKey)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent fork request: %w", err)
		}
		if derefStr(existing.SourceCheckpointID) != checkpointID {
			return nil, false, dbpkg.ErrIdempotencyConflict
		}
		instance, err := toAgentInstance(existing)
		return instance, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("fork AgentInstance: %w", err)
	}
	instance, err := toAgentInstance(row)
	return instance, err == nil, err
}

type forkIDs struct {
	namespace uuid.UUID
	tasks     map[a2a.TaskID]a2a.TaskID
	messages  map[string]string
}

func newForkIDs(instanceID string) *forkIDs {
	return &forkIDs{
		namespace: uuid.NewSHA1(uuid.NameSpaceOID, []byte(instanceID)),
		tasks:     map[a2a.TaskID]a2a.TaskID{},
		messages:  map[string]string{},
	}
}

func (i *forkIDs) task(id a2a.TaskID) a2a.TaskID {
	if mapped, ok := i.tasks[id]; ok {
		return mapped
	}
	mapped := a2a.TaskID(uuid.NewSHA1(i.namespace, []byte("task:"+string(id))).String())
	i.tasks[id] = mapped
	return mapped
}

func (i *forkIDs) message(id string) string {
	if mapped, ok := i.messages[id]; ok {
		return mapped
	}
	mapped := uuid.NewSHA1(i.namespace, []byte("message:"+id)).String()
	i.messages[id] = mapped
	return mapped
}

func reidentifyForkTask(task *a2a.Task, contextID string, ids *forkIDs) {
	task.ID = ids.task(task.ID)
	task.ContextID = contextID
	for _, message := range task.History {
		reidentifyForkMessage(message, task.ID, contextID, ids)
	}
	if task.Status.Message != nil {
		reidentifyForkMessage(task.Status.Message, task.ID, contextID, ids)
	}
}

func reidentifyForkMessage(message *a2a.Message, taskID a2a.TaskID, contextID string, ids *forkIDs) {
	if message == nil {
		return
	}
	message.ID = ids.message(message.ID)
	message.ContextID = contextID
	message.TaskID = taskID
	for index, reference := range message.ReferenceTasks {
		message.ReferenceTasks[index] = ids.task(reference)
	}
}

func reidentifyForkEvent(event a2a.Event, sourceTaskID, contextID string, ids *forkIDs) {
	taskID := event.TaskInfo().TaskID
	if taskID == "" {
		taskID = a2a.TaskID(sourceTaskID)
	}
	switch event := event.(type) {
	case *a2a.Message:
		reidentifyForkMessage(event, ids.task(taskID), contextID, ids)
	case *a2a.Task:
		event.ID = taskID
		reidentifyForkTask(event, contextID, ids)
	case *a2a.TaskStatusUpdateEvent:
		event.TaskID = ids.task(taskID)
		event.ContextID = contextID
		if event.Status.Message != nil {
			reidentifyForkMessage(event.Status.Message, event.TaskID, contextID, ids)
		}
	case *a2a.TaskArtifactUpdateEvent:
		event.TaskID = ids.task(taskID)
		event.ContextID = contextID
	}
}

func (c *postgresClient) GetAgentInstance(ctx context.Context, namespace, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	row, err := c.q.GetAgentInstanceForUser(ctx, dbgen.GetAgentInstanceForUserParams{Namespace: namespace, ID: id, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s/%s: %w", namespace, id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *postgresClient) ListAgentInstances(ctx context.Context, query dbpkg.AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error) {
	matchLabels := query.MatchLabels
	if matchLabels == nil {
		matchLabels = map[string]string{}
	}
	labels, err := json.Marshal(matchLabels)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentInstance label selector: %w", err)
	}
	rows, err := c.q.ListAgentInstances(ctx, dbgen.ListAgentInstancesParams{
		Namespace: query.Namespace, UserID: query.UserID, AllUsers: query.AllUsers,
		AfterID: query.AfterID, MatchLabels: labels,
		AgentTemplate: query.AgentTemplate, Harness: query.Harness,
		PageSize: int32(query.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentInstances: %w", err)
	}
	result := make([]*apiv1alpha1.AgentInstance, 0, len(rows))
	for _, row := range rows {
		instance, err := toAgentInstance(row)
		if err != nil {
			return nil, err
		}
		result = append(result, instance)
	}
	return result, nil
}

// RenameAgentInstance writes only the name column, scoped to the instance's
// owner so a rename cannot reach another reader's conversation.
func (c *postgresClient) RenameAgentInstance(ctx context.Context, namespace, id, userID, name string) (*apiv1alpha1.AgentInstance, error) {
	row, err := c.q.RenameAgentInstance(ctx, dbgen.RenameAgentInstanceParams{
		Namespace: namespace, ID: id, UserID: userID, Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("rename AgentInstance %s/%s: %w", namespace, id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *postgresClient) MarkAgentInstanceReady(ctx context.Context, id, authority string) (*apiv1alpha1.AgentInstance, error) {
	row, err := c.q.GetAgentInstanceByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s before ready: %w", id, notFoundOr(err))
	}
	instance, err := toAgentInstance(row)
	if err != nil || instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING {
		return instance, err
	}
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
	instance.A2AAuthority = authority
	instance.Failure = nil
	instance.UpdatedAt = timestamppb.Now()
	data, err := marshalAgentInstance(instance)
	if err != nil {
		return nil, err
	}
	row, err = c.q.MarkAgentInstanceReady(ctx, dbgen.MarkAgentInstanceReadyParams{ID: id, Data: data})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = c.q.GetAgentInstanceByID(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("mark AgentInstance %s ready: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *postgresClient) TransitionAgentInstance(
	ctx context.Context,
	instance *apiv1alpha1.AgentInstance,
	expectedState apiv1alpha1.AgentInstanceState,
	expectedOperation apiv1alpha1.AgentInstanceOperation,
) (*apiv1alpha1.AgentInstance, error) {
	data, err := marshalAgentInstance(instance)
	if err != nil {
		return nil, err
	}
	row, err := c.q.TransitionAgentInstance(ctx, dbgen.TransitionAgentInstanceParams{
		ID: instance.GetId(), Data: data,
		ExpectedState: agentInstanceStateName(expectedState), ExpectedOperation: agentInstanceOperationName(expectedOperation),
		NextState: agentInstanceStateName(instance.GetState()), NextOperation: agentInstanceOperationName(instance.GetOperation()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = c.q.GetAgentInstanceByID(ctx, instance.GetId())
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("transition AgentInstance %s: %w", instance.GetId(), dbpkg.ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("get conflicting AgentInstance %s: %w", instance.GetId(), err)
		}
		current, decodeErr := toAgentInstance(row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		return current, dbpkg.ErrAgentInstanceConflict
	}
	if err != nil {
		return nil, fmt.Errorf("transition AgentInstance %s: %w", instance.GetId(), err)
	}
	return toAgentInstance(row)
}

func agentInstanceStateName(state apiv1alpha1.AgentInstanceState) string {
	return strings.TrimPrefix(state.String(), "AGENT_INSTANCE_STATE_")
}

func agentInstanceOperationName(operation apiv1alpha1.AgentInstanceOperation) string {
	if operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return "NONE"
	}
	return strings.TrimPrefix(operation.String(), "AGENT_INSTANCE_OPERATION_")
}

func (c *postgresClient) DeleteAgentInstance(ctx context.Context, id string) error {
	if err := c.q.DeleteAgentInstance(ctx, id); err != nil {
		return fmt.Errorf("delete AgentInstance %s: %w", id, err)
	}
	return nil
}

func toAgentInstanceShare(row dbgen.AgentInstanceShare) dbpkg.AgentInstanceShare {
	return dbpkg.AgentInstanceShare{
		ID: row.ID, Namespace: row.Namespace, InstanceID: row.InstanceID,
		Creator: row.Creator, Permission: row.Permission,
		TokenHash: row.TokenHash, CreatedAt: row.CreatedAt,
	}
}

func (c *postgresClient) CreateAgentInstanceShare(ctx context.Context, share dbpkg.AgentInstanceShare) (*dbpkg.AgentInstanceShare, error) {
	row, err := c.q.CreateAgentInstanceShare(ctx, dbgen.CreateAgentInstanceShareParams{
		ID: share.ID, Namespace: share.Namespace, InstanceID: share.InstanceID,
		Creator: share.Creator, Permission: share.Permission, TokenHash: share.TokenHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create AgentInstance share: %w", err)
	}
	result := toAgentInstanceShare(row)
	return &result, nil
}

// GetAgentInstanceShareByTokenHash resolves a share token to its share.
//
// Takes the hash rather than the token: only the digest is stored, which is what
// keeps a database dump from being a set of working share links.
func (c *postgresClient) GetAgentInstanceShareByTokenHash(ctx context.Context, tokenHash []byte) (*dbpkg.AgentInstanceShare, error) {
	row, err := c.q.GetAgentInstanceShareByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance share by token: %w", notFoundOr(err))
	}
	return &dbpkg.AgentInstanceShare{
		ID: row.ID, Namespace: row.Namespace, InstanceID: row.InstanceID,
		Creator: row.Creator, Permission: row.Permission,
		TokenHash: row.TokenHash, CreatedAt: row.CreatedAt,
		OwnerUserID: row.OwnerUserID,
	}, nil
}

func (c *postgresClient) ListAgentInstanceShares(ctx context.Context, namespace, instanceID, creator, afterID string, limit int) ([]dbpkg.AgentInstanceShare, error) {
	rows, err := c.q.ListAgentInstanceShares(ctx, dbgen.ListAgentInstanceSharesParams{
		Namespace: namespace, InstanceID: instanceID, UserID: creator,
		AfterID: afterID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentInstance shares: %w", err)
	}
	result := make([]dbpkg.AgentInstanceShare, 0, len(rows))
	for _, row := range rows {
		result = append(result, toAgentInstanceShare(row))
	}
	return result, nil
}

func (c *postgresClient) DeleteAgentInstanceShare(ctx context.Context, namespace, id, creator string) error {
	count, err := c.q.DeleteAgentInstanceShare(ctx, dbgen.DeleteAgentInstanceShareParams{Namespace: namespace, ID: id, UserID: creator})
	if err != nil {
		return fmt.Errorf("delete AgentInstance share %s/%s: %w", namespace, id, err)
	}
	if count == 0 {
		return dbpkg.ErrNotFound
	}
	return nil
}

func (c *postgresClient) CreateAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, task *a2a.Task) (*a2a.Task, bool, error) {
	if task == nil || len(task.History) == 0 || task.History[0] == nil || task.History[0].ID == "" {
		return nil, false, fmt.Errorf("AgentInstance task requires an initial message")
	}
	message := task.History[0]
	taskData, err := marshalAgentInstanceTask(task)
	if err != nil {
		return nil, false, err
	}

	result := task
	created := false
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		instance, err := q.LockAgentInstance(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", instanceID, err)
		}
		if instance.State != "READY" || instance.Operation != "NONE" {
			return dbpkg.ErrAgentInstanceTaskConflict
		}
		rows, err := q.CreateAgentInstanceTask(ctx, dbgen.CreateAgentInstanceTaskParams{
			ContextID: instanceID, ID: string(task.ID), State: string(task.Status.State),
			StatusTimestamp: task.Status.Timestamp, Data: taskData,
			InitialMessageID: &message.ID, RequestHash: requestHash,
		})
		if err != nil {
			if isActiveTaskConflict(err) {
				return dbpkg.ErrAgentInstanceTaskConflict
			}
			return fmt.Errorf("create AgentInstance task %s: %w", task.ID, err)
		}
		if rows == 0 {
			row, err := q.GetAgentInstanceTaskByMessageID(ctx, dbgen.GetAgentInstanceTaskByMessageIDParams{
				ContextID: instanceID, InitialMessageID: &message.ID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return dbpkg.ErrAgentInstanceTaskConflict
			}
			if err != nil {
				return fmt.Errorf("get AgentInstance task for message %s: %w", message.ID, err)
			}
			if !bytes.Equal(row.RequestHash, requestHash) {
				return dbpkg.ErrIdempotencyConflict
			}
			result, err = unmarshalAgentInstanceTask(row.Data)
			if err != nil {
				return err
			}
			return loadAgentInstanceTaskHistories(ctx, q, instanceID, []*a2a.Task{result})
		}
		created = true
		_, err = storeAgentInstanceTaskMessages(ctx, q, instanceID, string(task.ID), task.History)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("create AgentInstance task: %w", err)
	}
	return result, created, nil
}

// taskInterruptedMessage explains a task terminated because its runtime no
// longer has an active execution for it.
const taskInterruptedMessage = "The turn was interrupted before it completed, and the process running it is no longer reporting progress."

// taskAbandonedMessage explains a task closed because it was waiting on the
// reader and the reader started a new turn instead. It is deliberately not the
// interrupted wording: nothing went wrong, so saying the runtime stopped
// reporting progress would be a falsehood in the transcript.
const taskAbandonedMessage = "This turn was waiting for a reply and was closed when a new message started the next turn."

func (c *postgresClient) GetActiveAgentInstanceTask(ctx context.Context, instanceID string) (*a2a.Task, error) {
	row, err := c.q.GetActiveAgentInstanceTask(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get active AgentInstance task: %w", notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err == nil {
		err = loadAgentInstanceTaskHistories(ctx, c.q, instanceID, []*a2a.Task{task})
	}
	return task, err
}

// InterruptActiveAgentInstanceTask atomically fails taskID only if it is still the
// instance's active task.
func (c *postgresClient) InterruptActiveAgentInstanceTask(ctx context.Context, instanceID, taskID string) (bool, error) {
	return c.terminateAgentInstanceTask(ctx, instanceID, taskID, a2a.TaskStateFailed, taskInterruptedMessage, true)
}

// AbandonActiveAgentInstanceTask closes a task that was parked awaiting the
// reader, so the instance's single active-task slot is released. It is canceled
// rather than failed because nothing failed: the turn was waiting for input
// that never came.
func (c *postgresClient) AbandonActiveAgentInstanceTask(ctx context.Context, instanceID, taskID string) (bool, error) {
	return c.terminateAgentInstanceTask(ctx, instanceID, taskID, a2a.TaskStateCanceled, taskAbandonedMessage, false)
}

// ClaimParkedAgentInstanceTask moves a task that is waiting on the reader into
// TASK_STATE_WORKING so a reply can be delivered, and reports whether this call
// is the one that did it.
//
// The row lock is what makes it a replay guard: a duplicate reply serialises
// behind the first, sees a task that is no longer parked, and is refused rather
// than delivered twice. The returned task is the parked one as it stood *before*
// the claim, so a caller whose delivery fails can put the question back.
func (c *postgresClient) ClaimParkedAgentInstanceTask(ctx context.Context, instanceID, taskID string) (*a2a.Task, bool, error) {
	var parked *a2a.Task
	claimed := false
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		// By id, not "the active task". A parked turn no longer holds the instance's
		// slot — an unanswered question must not stop the next turn — so asking for the
		// active task finds something else, or nothing, and the reply lands nowhere.
		row, err := q.LockAgentInstanceTask(ctx, dbgen.LockAgentInstanceTaskParams{
			ContextID: instanceID, ID: taskID,
		})
		// Not claimed rather than an error: a reply naming a task this instance does
		// not have is the same answer as one naming a task that is not parked — there
		// is nothing here to reply to. Both leave `claimed` false, and the caller
		// refuses the reply on that alone.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock AgentInstance task: %w", err)
		}
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		if !dbpkg.TaskParkedAwaitingUser(task.Status.State) {
			return nil
		}
		working := *task
		now := time.Now()
		working.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: &now}
		data, err := marshalAgentInstanceTask(&working)
		if err != nil {
			return err
		}
		if err := q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{
			ContextID: instanceID, ID: string(working.ID), State: string(working.Status.State),
			StatusTimestamp: working.Status.Timestamp, Data: data,
		}); err != nil {
			return fmt.Errorf("claim parked AgentInstance task %s: %w", working.ID, err)
		}
		parked, claimed = task, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return parked, claimed, nil
}

// RestoreParkedAgentInstanceTask puts a claimed task back exactly as it was, for
// a reply that never reached the runtime. The question stays answerable, which
// failing the task would not allow.
func (c *postgresClient) RestoreParkedAgentInstanceTask(ctx context.Context, instanceID string, task *a2a.Task) error {
	data, err := marshalAgentInstanceTask(task)
	if err != nil {
		return err
	}
	if err := c.q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{
		ContextID: instanceID, ID: string(task.ID), State: string(task.Status.State),
		StatusTimestamp: task.Status.Timestamp, Data: data,
	}); err != nil {
		return fmt.Errorf("restore parked AgentInstance task %s: %w", task.ID, err)
	}
	return nil
}

// terminateAgentInstanceTask atomically moves taskID to a terminal state.
//
// `requireActive` says which task the caller means, and the two callers mean
// different things. Interrupting is about the turn *in flight*, so it must not touch a
// task that a concurrent turn has already replaced — it asks for the active task and
// gives up unless that is the one named. Abandoning is about a turn parked awaiting an
// answer, and a parked task is no longer the active one: it stopped holding the
// instance's slot when the active-task query began excluding INPUT_REQUIRED, so asking
// for the active task would find something else, or nothing, and quietly do nothing.
func (c *postgresClient) terminateAgentInstanceTask(
	ctx context.Context, instanceID, taskID string, state a2a.TaskState, reason string,
	requireActive bool,
) (bool, error) {
	interruptedTask := false
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		var row dbgen.AgentInstanceTask
		var err error
		if requireActive {
			row, err = q.LockActiveAgentInstanceTask(ctx, instanceID)
		} else {
			row, err = q.LockAgentInstanceTask(ctx, dbgen.LockAgentInstanceTaskParams{
				ContextID: instanceID, ID: taskID,
			})
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock AgentInstance task: %w", err)
		}
		if row.ID != taskID {
			return nil
		}
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		if err := loadAgentInstanceTaskHistories(ctx, q, instanceID, []*a2a.Task{task}); err != nil {
			return err
		}
		interrupted := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart(reason))
		now := time.Now()
		task.History = append(task.History, interrupted)
		task.Status = a2a.TaskStatus{State: state, Message: interrupted, Timestamp: &now}
		data, err := marshalAgentInstanceTask(task)
		if err != nil {
			return err
		}
		if err := q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{
			ContextID: instanceID, ID: string(task.ID), State: string(task.Status.State),
			StatusTimestamp: task.Status.Timestamp, Data: data,
		}); err != nil {
			return fmt.Errorf("interrupt AgentInstance task %s: %w", task.ID, err)
		}
		if _, err := storeAgentInstanceTaskMessages(ctx, q, instanceID, string(task.ID), task.History); err != nil {
			return fmt.Errorf("record AgentInstance task interruption: %w", err)
		}
		interruptedTask = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return interruptedTask, nil
}

func (c *postgresClient) StoreAgentInstanceTaskEvent(ctx context.Context, instanceID string, task *a2a.Task, event a2a.Event, snapshot *dbpkg.AgentInstanceTaskSnapshot) error {
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		var sequence int64
		if task != nil {
			if row, err := q.GetAgentInstanceTask(ctx, dbgen.GetAgentInstanceTaskParams{ContextID: instanceID, ID: string(task.ID)}); err == nil {
				previous, err := unmarshalAgentInstanceTask(row.Data)
				if err != nil {
					return err
				}
				if len(previous.History) > 0 {
					sequence, err = storeAgentInstanceTaskMessages(ctx, q, instanceID, string(task.ID), previous.History)
					if err != nil {
						return fmt.Errorf("normalize legacy AgentInstance task history: %w", err)
					}
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("get AgentInstance task %s: %w", task.ID, err)
			}
			data, err := marshalAgentInstanceTask(task)
			if err != nil {
				return err
			}
			if err := q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{
				ContextID: instanceID, ID: string(task.ID), State: string(task.Status.State),
				StatusTimestamp: task.Status.Timestamp, Data: data,
			}); err != nil {
				if isActiveTaskConflict(err) {
					return dbpkg.ErrAgentInstanceTaskConflict
				}
				return fmt.Errorf("store AgentInstance task %s: %w", task.ID, err)
			}
		}
		messages := agentInstanceTaskEventMessages(task, event)
		if len(messages) > 0 {
			var err error
			sequence, err = storeAgentInstanceTaskMessages(ctx, q, instanceID, string(event.TaskInfo().TaskID), messages)
			if err != nil {
				return fmt.Errorf("store AgentInstance task history: %w", err)
			}
		}
		if _, ok := event.(*a2a.Message); !ok {
			eventData, err := marshalAgentInstanceTaskEvent(event)
			if err != nil {
				return err
			}
			sequence, err = q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
				ContextID: instanceID, TaskID: strPtrIfNotEmpty(string(event.TaskInfo().TaskID)), Data: eventData,
			})
			if err != nil {
				return fmt.Errorf("store AgentInstance task event: %w", err)
			}
		}
		if snapshot != nil {
			if sequence == 0 {
				return fmt.Errorf("snapshot has no history boundary")
			}
			if err := q.SetAgentInstanceTaskSnapshot(ctx, dbgen.SetAgentInstanceTaskSnapshotParams{
				ContextID: instanceID, ID: string(task.ID), SnapshotAtespace: &snapshot.Atespace,
				SnapshotName: &snapshot.Name, SnapshotUid: &snapshot.UID,
				SnapshotContentScope: &snapshot.ContentScope, HistorySequence: &sequence,
			}); err != nil {
				return fmt.Errorf("store AgentInstance task snapshot: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store AgentInstance task update: %w", err)
	}
	return nil
}

func (c *postgresClient) GetAgentInstanceTask(ctx context.Context, instanceID, taskID string) (*a2a.Task, error) {
	row, err := c.q.GetAgentInstanceTask(ctx, dbgen.GetAgentInstanceTaskParams{ContextID: instanceID, ID: taskID})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance task %s: %w", taskID, notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err == nil {
		err = loadAgentInstanceTaskHistories(ctx, c.q, instanceID, []*a2a.Task{task})
	}
	return task, err
}

func (c *postgresClient) ListAgentInstanceTasks(ctx context.Context, instanceID, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int) ([]*a2a.Task, int, error) {
	params := dbgen.CountAgentInstanceTasksParams{
		ContextID: instanceID, State: string(state), StatusTimestampAfter: statusTimestampAfter,
	}
	total, err := c.q.CountAgentInstanceTasks(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("count AgentInstance tasks: %w", err)
	}
	rows, err := c.q.ListAgentInstanceTasks(ctx, dbgen.ListAgentInstanceTasksParams{
		ContextID: instanceID, AfterID: afterID, State: params.State,
		StatusTimestampAfter: statusTimestampAfter, PageSize: int32(limit),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list AgentInstance tasks: %w", err)
	}
	tasks := make([]*a2a.Task, 0, len(rows))
	for _, row := range rows {
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return nil, 0, fmt.Errorf("decode AgentInstance task %s: %w", row.ID, err)
		}
		tasks = append(tasks, task)
	}
	if err := loadAgentInstanceTaskHistories(ctx, c.q, instanceID, tasks); err != nil {
		return nil, 0, err
	}
	return tasks, int(total), nil
}

func (c *postgresClient) ReserveAgentInstanceCheckpoint(ctx context.Context, checkpoint dbpkg.AgentInstanceCheckpoint) (*dbpkg.AgentInstanceCheckpoint, error) {
	var result *dbpkg.AgentInstanceCheckpoint
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		existing, err := q.GetAgentInstanceCheckpointByRequest(ctx, dbgen.GetAgentInstanceCheckpointByRequestParams{
			UserID: checkpoint.UserID, Namespace: checkpoint.Namespace, RequestID: checkpoint.RequestID,
		})
		if err == nil {
			if existing.SourceInstanceID != checkpoint.SourceInstanceID {
				return dbpkg.ErrIdempotencyConflict
			}
			result = toAgentInstanceCheckpoint(existing)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get AgentInstance checkpoint by request: %w", err)
		}

		instance, err := q.LockAgentInstance(ctx, checkpoint.SourceInstanceID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (instance.Namespace != checkpoint.Namespace || instance.UserID != checkpoint.UserID)) {
			return dbpkg.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", checkpoint.SourceInstanceID, err)
		}
		if instance.State != "READY" || instance.Operation != "NONE" {
			return dbpkg.ErrAgentInstanceConflict
		}
		boundary, err := q.GetLatestQuiescentAgentInstanceTask(ctx, instance.ContextID)
		if errors.Is(err, pgx.ErrNoRows) {
			return dbpkg.ErrAgentInstanceNotQuiescent
		}
		if err != nil {
			return fmt.Errorf("get latest AgentInstance task boundary: %w", err)
		}
		if boundary.SnapshotAtespace == nil || boundary.SnapshotName == nil || boundary.SnapshotUid == nil ||
			boundary.SnapshotContentScope == nil || boundary.HistorySequence == nil {
			return dbpkg.ErrAgentInstanceNotQuiescent
		}

		row, err := q.InsertAgentInstanceCheckpoint(ctx, dbgen.InsertAgentInstanceCheckpointParams{
			ID: checkpoint.ID, Namespace: checkpoint.Namespace, SourceInstanceID: checkpoint.SourceInstanceID,
			UserID: checkpoint.UserID, RequestID: checkpoint.RequestID, HeadTaskID: boundary.ID,
			HistorySequence: *boundary.HistorySequence, SnapshotAtespace: *boundary.SnapshotAtespace,
			SnapshotName: *boundary.SnapshotName, SnapshotUid: *boundary.SnapshotUid,
			SnapshotContentScope: *boundary.SnapshotContentScope,
			SourceContextID:      instance.ContextID, PreparedRevision: instance.PreparedRevision,
			SourceLabels: instance.Labels,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			existing, existingErr := q.GetAgentInstanceCheckpointByRequest(ctx, dbgen.GetAgentInstanceCheckpointByRequestParams{
				UserID: checkpoint.UserID, Namespace: checkpoint.Namespace, RequestID: checkpoint.RequestID,
			})
			if existingErr == nil {
				if existing.SourceInstanceID != checkpoint.SourceInstanceID {
					return dbpkg.ErrIdempotencyConflict
				}
				result = toAgentInstanceCheckpoint(existing)
				return nil
			}
			if errors.Is(existingErr, pgx.ErrNoRows) {
				return dbpkg.ErrAgentInstanceConflict
			}
			return fmt.Errorf("get conflicting AgentInstance checkpoint request: %w", existingErr)
		}
		if err != nil {
			return fmt.Errorf("insert AgentInstance checkpoint: %w", err)
		}
		result = toAgentInstanceCheckpoint(row)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reserve AgentInstance checkpoint: %w", err)
	}
	return result, nil
}

func (c *postgresClient) FinalizeAgentInstanceCheckpoint(ctx context.Context, id, tagUID, failure string) (*dbpkg.AgentInstanceCheckpoint, error) {
	if (tagUID == "") == (failure == "") {
		return nil, fmt.Errorf("finalize AgentInstance checkpoint requires exactly one of tag UID or failure")
	}
	row, err := c.q.FinalizeAgentInstanceCheckpoint(ctx, dbgen.FinalizeAgentInstanceCheckpointParams{
		ID: id, TagUid: tagUID, Failure: failure,
	})
	if err != nil {
		return nil, fmt.Errorf("finalize AgentInstance checkpoint: %w", notFoundOr(err))
	}
	return toAgentInstanceCheckpoint(row), nil
}

func (c *postgresClient) GetAgentInstanceCheckpoint(ctx context.Context, namespace, id, userID string) (*dbpkg.AgentInstanceCheckpoint, error) {
	row, err := c.q.GetAgentInstanceCheckpoint(ctx, dbgen.GetAgentInstanceCheckpointParams{Namespace: namespace, ID: id, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance checkpoint: %w", notFoundOr(err))
	}
	return toAgentInstanceCheckpoint(row), nil
}

func (c *postgresClient) ListAgentInstanceCheckpoints(ctx context.Context, namespace, instanceID, userID, afterID string, limit int) ([]dbpkg.AgentInstanceCheckpoint, error) {
	rows, err := c.q.ListAgentInstanceCheckpoints(ctx, dbgen.ListAgentInstanceCheckpointsParams{
		Namespace: namespace, SourceInstanceID: instanceID, UserID: userID, AfterID: afterID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentInstance checkpoints: %w", err)
	}
	result := make([]dbpkg.AgentInstanceCheckpoint, len(rows))
	for i := range rows {
		result[i] = *toAgentInstanceCheckpoint(rows[i])
	}
	return result, nil
}

func (c *postgresClient) BeginDeleteAgentInstanceCheckpoint(ctx context.Context, namespace, id, userID string) (*dbpkg.AgentInstanceCheckpoint, error) {
	row, err := c.q.BeginDeleteAgentInstanceCheckpoint(ctx, dbgen.BeginDeleteAgentInstanceCheckpointParams{
		Namespace: namespace, ID: id, UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("begin delete AgentInstance checkpoint: %w", notFoundOr(err))
	}
	return toAgentInstanceCheckpoint(row), nil
}

func (c *postgresClient) DeleteAgentInstanceCheckpoint(ctx context.Context, namespace, id, userID string) error {
	_, err := c.q.DeleteAgentInstanceCheckpoint(ctx, dbgen.DeleteAgentInstanceCheckpointParams{Namespace: namespace, ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete AgentInstance checkpoint: %w", err)
	}
	return nil
}

func marshalAgentInstanceTask(task *a2a.Task) ([]byte, error) {
	projection := *task
	projection.History = nil
	pb, err := pbconv.ToProtoTask(&projection)
	if err != nil {
		return nil, fmt.Errorf("convert AgentInstance task: %w", err)
	}
	data, err := proto.Marshal(pb)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentInstance task: %w", err)
	}
	return data, nil
}

func marshalAgentInstanceTaskEvent(event a2a.Event) ([]byte, error) {
	if task, ok := event.(*a2a.Task); ok {
		projection := *task
		projection.History = nil
		event = &projection
	}
	pb, err := pbconv.ToProtoStreamResponse(event)
	if err != nil {
		return nil, fmt.Errorf("convert AgentInstance task event: %w", err)
	}
	data, err := proto.Marshal(pb)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentInstance task event: %w", err)
	}
	return data, nil
}

func unmarshalAgentInstanceTaskEvent(data []byte) (a2a.Event, error) {
	var pb a2apb.StreamResponse
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("unmarshal AgentInstance task event: %w", err)
	}
	event, err := pbconv.FromProtoStreamResponse(&pb)
	if err != nil {
		return nil, fmt.Errorf("convert AgentInstance task event: %w", err)
	}
	return event, nil
}

func agentInstanceTaskEventMessages(task *a2a.Task, event a2a.Event) []*a2a.Message {
	switch event := event.(type) {
	case *a2a.Message:
		return []*a2a.Message{event}
	case *a2a.Task:
		return event.History
	case *a2a.TaskStatusUpdateEvent:
		if task != nil && len(task.History) > 0 {
			return task.History[len(task.History)-1:]
		}
	}
	return nil
}

func storeAgentInstanceTaskMessages(ctx context.Context, q *dbgen.Queries, contextID, taskID string, messages []*a2a.Message) (int64, error) {
	var sequence int64
	for _, message := range messages {
		if message == nil || message.ID == "" {
			return 0, fmt.Errorf("AgentInstance task history contains a message without an ID")
		}
		data, err := marshalAgentInstanceTaskEvent(message)
		if err != nil {
			return 0, err
		}
		sequence, err = q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
			ContextID: contextID, TaskID: &taskID, MessageID: &message.ID, Data: data,
		})
		if err != nil {
			return 0, err
		}
	}
	return sequence, nil
}

func loadAgentInstanceTaskHistories(ctx context.Context, q *dbgen.Queries, contextID string, tasks []*a2a.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, len(tasks))
	byID := make(map[string]*a2a.Task, len(tasks))
	for index, task := range tasks {
		ids[index] = string(task.ID)
		byID[string(task.ID)] = task
	}
	rows, err := q.ListAgentInstanceTaskHistory(ctx, dbgen.ListAgentInstanceTaskHistoryParams{ContextID: contextID, TaskIds: ids})
	if err != nil {
		return fmt.Errorf("list AgentInstance task history: %w", err)
	}
	histories := make(map[string][]*a2a.Message, len(tasks))
	for _, row := range rows {
		if row.TaskID == nil {
			continue
		}
		event, err := unmarshalAgentInstanceTaskEvent(row.Data)
		if err != nil {
			return err
		}
		message, ok := event.(*a2a.Message)
		if !ok {
			return fmt.Errorf("AgentInstance task history event is %T, not a message", event)
		}
		histories[*row.TaskID] = append(histories[*row.TaskID], message)
	}
	for taskID, history := range histories {
		if task := byID[taskID]; task != nil {
			task.History = history
		}
	}
	return nil
}

func isActiveTaskConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "agent_instance_one_active_task_idx"
}

func unmarshalAgentInstanceTask(data []byte) (*a2a.Task, error) {
	var pb a2apb.Task
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("unmarshal AgentInstance task: %w", err)
	}
	task, err := pbconv.FromProtoTask(&pb)
	if err != nil {
		return nil, fmt.Errorf("convert AgentInstance task: %w", err)
	}
	return task, nil
}

// ── Push Notifications ────────────────────────────────────────────────────────

func (c *postgresClient) StorePushNotification(ctx context.Context, config *a2a.PushConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize push notification: %w", err)
	}
	protocolVersion := string(a2a.Version)
	return c.q.UpsertPushNotification(ctx, dbgen.UpsertPushNotificationParams{
		ID:              config.ID,
		TaskID:          string(config.TaskID),
		Data:            string(data),
		ProtocolVersion: &protocolVersion,
	})
}

func (c *postgresClient) GetPushNotification(ctx context.Context, taskID, configID string) (*a2a.PushConfig, error) {
	row, err := c.q.GetPushNotification(ctx, dbgen.GetPushNotificationParams{TaskID: taskID, ID: configID})
	if err != nil {
		return nil, fmt.Errorf("failed to get push notification: %w", notFoundOr(err))
	}
	return parseVersionedPushConfig(row.Data, row.ProtocolVersion)
}

func (c *postgresClient) ListPushNotifications(ctx context.Context, taskID string) ([]*a2a.PushConfig, error) {
	rows, err := c.q.ListPushNotifications(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list push notifications: %w", err)
	}
	result := make([]*a2a.PushConfig, 0, len(rows))
	for i, row := range rows {
		cfg, err := parseVersionedPushConfig(row.Data, row.ProtocolVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize push notification row %d: %w", i, err)
		}
		result = append(result, cfg)
	}
	return result, nil
}

func (c *postgresClient) DeletePushNotification(ctx context.Context, taskID string) error {
	return c.q.SoftDeletePushNotification(ctx, taskID)
}

// ── Feedback ──────────────────────────────────────────────────────────────────

func (c *postgresClient) StoreFeedback(ctx context.Context, feedback *dbpkg.Feedback) error {
	err := c.q.InsertFeedback(ctx, dbgen.InsertFeedbackParams{
		UserID:       feedback.UserID,
		MessageID:    feedback.MessageID,
		IsPositive:   feedback.IsPositive,
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
		return nil, fmt.Errorf("failed to get tool %s: %w", name, notFoundOr(err))
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

func (c *postgresClient) RefreshToolsForServer(ctx context.Context, serverName, groupKind string, tools ...*v1alpha3.MCPTool) error {
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
		return nil, fmt.Errorf("failed to get tool server %s: %w", name, notFoundOr(err))
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
	return c.q.UpsertCheckpoint(ctx, dbgen.UpsertCheckpointParams{
		UserID:             cp.UserID,
		ThreadID:           cp.ThreadID,
		CheckpointNs:       cp.CheckpointNS,
		CheckpointID:       cp.CheckpointID,
		ParentCheckpointID: cp.ParentCheckpointID,
		Metadata:           cp.Metadata,
		Checkpoint:         cp.Checkpoint,
		CheckpointType:     cp.CheckpointType,
		Version:            cp.Version,
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
				return fmt.Errorf("failed to get checkpoint: %w", notFoundOr(err))
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

		// Fetch all writes for the returned checkpoints in a single query and
		// bucket them by checkpoint ID, instead of issuing one query per
		// checkpoint (which turned reading a thread's history into 1+N round
		// trips that grew with the conversation length).
		checkpointIDs := make([]string, len(checkpoints))
		for i, cp := range checkpoints {
			checkpointIDs[i] = cp.CheckpointID
		}

		writesByCheckpoint := make(map[string][]*dbpkg.LangGraphCheckpointWrite, len(checkpoints))
		if len(checkpointIDs) > 0 {
			writes, err := q.ListCheckpointWritesForCheckpoints(ctx, dbgen.ListCheckpointWritesForCheckpointsParams{
				UserID: userID, ThreadID: threadID, CheckpointNs: checkpointNS, CheckpointIds: checkpointIDs,
			})
			if err != nil {
				return fmt.Errorf("failed to get checkpoint writes: %w", err)
			}
			for _, w := range writes {
				writesByCheckpoint[w.CheckpointID] = append(writesByCheckpoint[w.CheckpointID], toCheckpointWrite(w))
			}
		}

		tuples = make([]*dbpkg.LangGraphCheckpointTuple, 0, len(checkpoints))
		for _, cp := range checkpoints {
			dbWrites := writesByCheckpoint[cp.CheckpointID]
			if dbWrites == nil {
				dbWrites = []*dbpkg.LangGraphCheckpointWrite{}
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
	normalized := strings.ReplaceAll(agentName, "-", "_")
	rows, err := c.q.SearchAgentMemory(ctx, dbgen.SearchAgentMemoryParams{
		Embedding:   embedding,
		AgentName:   &agentName,
		AgentName_2: &normalized,
		UserID:      &userID,
		Limit:       int32(limit),
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
				CreatedAt:   derefTime(r.CreatedAt),
				ExpiresAt:   r.ExpiresAt,
				AccessCount: derefInt64(r.AccessCount),
			},
			Score: score,
		}
	}

	// Access-count bookkeeping is best-effort: a failure must not fail the search.
	if len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		if err := c.q.IncrementMemoryAccessCount(ctx, ids); err != nil {
			log.Printf("failed to increment memory access count: %v", err)
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

const sessionRetentionBatchSize int32 = 1000

func (c *postgresClient) PruneExpiredSessions(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	var total int64
	for {
		var n int64
		err := c.withTx(ctx, func(q *dbgen.Queries) error {
			var err error
			n, err = q.DeleteExpiredSessionsBatch(ctx, dbgen.DeleteExpiredSessionsBatchParams{
				RetentionDays: int32(retentionDays),
				BatchSize:     sessionRetentionBatchSize,
			})
			if err != nil {
				return fmt.Errorf("failed to delete expired sessions batch: %w", err)
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += n
		if n == 0 {
			break
		}
	}
	return total, nil
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func toAgent(r dbgen.Agent) *dbpkg.Agent {
	return &dbpkg.Agent{
		ID:           r.ID,
		CreatedAt:    derefTime(r.CreatedAt),
		UpdatedAt:    derefTime(r.UpdatedAt),
		DeletedAt:    r.DeletedAt,
		Type:         r.Type,
		WorkloadType: v1alpha3.WorkloadMode(r.WorkloadType),
		Config:       r.Config,
	}
}

func toSession(r dbgen.Session) *dbpkg.Session {
	s := &dbpkg.Session{
		ID:        r.ID,
		UserID:    r.UserID,
		Name:      r.Name,
		CreatedAt: derefTime(r.CreatedAt),
		UpdatedAt: derefTime(r.UpdatedAt),
		DeletedAt: r.DeletedAt,
		AgentID:   r.AgentID,
	}
	if r.Source != nil {
		src := dbpkg.SessionSource(*r.Source)
		s.Source = &src
	}
	return s
}

func toSessionWithShareToken(r dbgen.ListSessionsForAgentRow) dbpkg.SessionWithShareToken {
	s := dbpkg.SessionWithShareToken{
		Session: *toSession(dbgen.Session{
			ID:        r.ID,
			UserID:    r.UserID,
			Name:      r.Name,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			DeletedAt: r.DeletedAt,
			AgentID:   r.AgentID,
			Source:    r.Source,
		}),
	}
	switch v := r.ShareToken.(type) {
	case string:
		s.ShareToken = &v
	case pgtype.Text:
		if v.Valid {
			s.ShareToken = &v.String
		}
	}
	switch v := r.ShareReadOnly.(type) {
	case bool:
		s.ShareReadOnly = &v
	case pgtype.Bool:
		if v.Valid {
			s.ShareReadOnly = &v.Bool
		}
	}
	return s
}

func toEvent(r dbgen.Event) *dbpkg.Event {
	return &dbpkg.Event{
		ID:        r.ID,
		UserID:    r.UserID,
		SessionID: derefStr(r.SessionID),
		CreatedAt: derefTime(r.CreatedAt),
		UpdatedAt: derefTime(r.UpdatedAt),
		DeletedAt: r.DeletedAt,
		Data:      r.Data,
	}
}

//nolint:unused // Kept for parity with other row mappers and future raw task DB APIs.
func toTask(r dbgen.Task) *dbpkg.Task {
	return &dbpkg.Task{
		ID:              r.ID,
		CreatedAt:       derefTime(r.CreatedAt),
		UpdatedAt:       derefTime(r.UpdatedAt),
		DeletedAt:       r.DeletedAt,
		Data:            r.Data,
		ProtocolVersion: r.ProtocolVersion,
		SessionID:       derefStr(r.SessionID),
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
		IsPositive:   r.IsPositive,
		FeedbackText: r.FeedbackText,
		IssueType:    r.IssueType,
	}
}

func toTool(r dbgen.Tool) *dbpkg.Tool {
	return &dbpkg.Tool{
		ID:          r.ID,
		ServerName:  r.ServerName,
		GroupKind:   r.GroupKind,
		CreatedAt:   derefTime(r.CreatedAt),
		UpdatedAt:   derefTime(r.UpdatedAt),
		DeletedAt:   r.DeletedAt,
		Description: derefStr(r.Description),
	}
}

func toToolServer(r dbgen.Toolserver) *dbpkg.ToolServer {
	return &dbpkg.ToolServer{
		Name:          r.Name,
		GroupKind:     r.GroupKind,
		CreatedAt:     derefTime(r.CreatedAt),
		UpdatedAt:     derefTime(r.UpdatedAt),
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
		CreatedAt:          derefTime(r.CreatedAt),
		UpdatedAt:          derefTime(r.UpdatedAt),
		DeletedAt:          r.DeletedAt,
		Metadata:           r.Metadata,
		Checkpoint:         r.Checkpoint,
		CheckpointType:     r.CheckpointType,
		Version:            r.Version,
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
		CreatedAt:    derefTime(r.CreatedAt),
		UpdatedAt:    derefTime(r.UpdatedAt),
		DeletedAt:    r.DeletedAt,
	}
}

func toAgentInstanceCheckpoint(row dbgen.AgentInstanceCheckpoint) *dbpkg.AgentInstanceCheckpoint {
	return &dbpkg.AgentInstanceCheckpoint{
		ID: row.ID, Namespace: row.Namespace, SourceInstanceID: row.SourceInstanceID,
		SourceContextID: row.SourceContextID, UserID: row.UserID,
		RequestID: row.RequestID, HeadTaskID: row.HeadTaskID, HistorySequence: row.HistorySequence,
		SnapshotAtespace: row.SnapshotAtespace, SnapshotName: row.SnapshotName, SnapshotUID: row.SnapshotUid,
		SnapshotContentScope: row.SnapshotContentScope, PreparedRevision: derefStr(row.PreparedRevision),
		TagUID: row.TagUid, State: row.State, Failure: row.Failure, CreatedAt: row.CreatedAt,
	}
}

func toCrewAIMemory(r dbgen.CrewaiAgentMemory) *dbpkg.CrewAIAgentMemory {
	return &dbpkg.CrewAIAgentMemory{
		UserID:     r.UserID,
		ThreadID:   r.ThreadID,
		CreatedAt:  derefTime(r.CreatedAt),
		UpdatedAt:  derefTime(r.UpdatedAt),
		DeletedAt:  r.DeletedAt,
		MemoryData: r.MemoryData,
	}
}

func toCrewAIFlowState(r dbgen.CrewaiFlowState) *dbpkg.CrewAIFlowState {
	return &dbpkg.CrewAIFlowState{
		UserID:     r.UserID,
		ThreadID:   r.ThreadID,
		MethodName: r.MethodName,
		CreatedAt:  derefTime(r.CreatedAt),
		UpdatedAt:  derefTime(r.UpdatedAt),
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
		CreatedAt:   derefTime(r.CreatedAt),
		ExpiresAt:   r.ExpiresAt,
		AccessCount: derefInt64(r.AccessCount),
	}
}

// ── Pointer helpers ───────────────────────────────────────────────────────────

func strPtrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// parseVersionedTask accepts only official A2A v1 rows during the v1-only cutover.
func parseVersionedTask(data string, version *string) (*a2a.Task, error) {
	if version == nil || *version != string(a2a.Version) {
		return nil, fmt.Errorf("unsupported task protocol_version %q: expected %q", derefStr(version), a2a.Version)
	}
	var task a2a.Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		return nil, fmt.Errorf("failed to deserialize task: %w", err)
	}
	return &task, nil
}

// parseVersionedPushConfig accepts only official A2A v1 rows during the v1-only cutover.
func parseVersionedPushConfig(data string, version *string) (*a2a.PushConfig, error) {
	if version == nil || *version != string(a2a.Version) {
		return nil, fmt.Errorf("unsupported push_notification protocol_version %q: expected %q", derefStr(version), a2a.Version)
	}
	var cfg a2a.PushConfig
	if err := json.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to deserialize push notification: %w", err)
	}
	return &cfg, nil
}

func derefStr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

func derefInt64(n *int64) int64 {
	if n != nil {
		return *n
	}
	return 0
}

func derefTime(t *time.Time) time.Time {
	if t != nil {
		return *t
	}
	return time.Time{}
}

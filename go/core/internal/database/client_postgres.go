package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5"
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
		SourceSnapshot: revision.SourceSnapshot, EgressDestinations: revision.EgressDestinations,
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
		SourceSnapshot: row.SourceSnapshot, EgressDestinations: row.EgressDestinations,
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
			SourceSnapshot: row.SourceSnapshot, EgressDestinations: row.EgressDestinations,
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
	row, err := c.q.InsertAgentInstance(ctx, dbgen.InsertAgentInstanceParams{
		ID: request.GetId(), Namespace: request.GetNamespace(), UserID: request.GetCreator(), RequestID: requestID,
		PreparedRevision: &revision.Revision, Labels: revision.AgentTemplateLabels, Data: data,
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

func (c *postgresClient) GetAgentInstance(ctx context.Context, namespace, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	row, err := c.q.GetAgentInstanceForUser(ctx, dbgen.GetAgentInstanceForUserParams{Namespace: namespace, ID: id, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s/%s: %w", namespace, id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *postgresClient) ListAgentInstances(ctx context.Context, namespace, userID string, allUsers bool, matchLabels map[string]string, afterID string, limit int) ([]*apiv1alpha1.AgentInstance, error) {
	if matchLabels == nil {
		matchLabels = map[string]string{}
	}
	labels, err := json.Marshal(matchLabels)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentInstance label selector: %w", err)
	}
	rows, err := c.q.ListAgentInstances(ctx, dbgen.ListAgentInstancesParams{
		Namespace: namespace, UserID: userID, AllUsers: allUsers,
		AfterID: afterID, MatchLabels: labels, PageSize: int32(limit),
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

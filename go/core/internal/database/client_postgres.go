package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/internal/dbgen"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/pgvector/pgvector-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Client persists control-plane state in PostgreSQL. Callers define the narrow
// interfaces they need; SQL rows and protobuf encoding stay inside the store.
type Client struct {
	q  *dbgen.Queries
	db *pgxpool.Pool
}

func NewClient(db *pgxpool.Pool) *Client {
	return &Client{
		q:  dbgen.New(db),
		db: db,
	}
}

func (c *Client) withTx(ctx context.Context, fn func(*dbgen.Queries) error) error {
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

func runtimeRevisionError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "runtime_revision_not_deleting" {
		return ErrObjectDeleting
	}
	return err
}

// notFoundOr maps the driver's no-rows error to ErrNotFound so callers
// outside this package match on the exported sentinel, never on pgx.
func notFoundOr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ── AgentTemplate runtime revisions ──────────────────────────────────────────

func (c *Client) UpsertAgentTemplateHarnessPair(ctx context.Context, pair AgentTemplateHarnessPair) error {
	if pair.AgentTemplateLabels == nil {
		pair.AgentTemplateLabels = map[string]string{}
	}
	labels, err := json.Marshal(pair.AgentTemplateLabels)
	if err != nil {
		return fmt.Errorf("marshal AgentTemplate labels: %w", err)
	}
	// Replace historical identities atomically without retiring the current UID
	// or rewriting already-retired rows on every pending-template poll.
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		if err := q.RetirePairIdentitiesExcept(ctx, dbgen.RetirePairIdentitiesExceptParams{
			Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName, HarnessName: pair.HarnessName,
			KeepAgentTemplateUid: pair.AgentTemplateUID, KeepHarnessUid: pair.HarnessUID,
		}); err != nil {
			return fmt.Errorf("retire replaced AgentTemplate/Harness pair: %w", err)
		}
		return runtimeRevisionError(q.UpsertAgentTemplateHarnessPair(ctx, dbgen.UpsertAgentTemplateHarnessPairParams{
			Namespace: pair.Namespace, AgentTemplateName: pair.AgentTemplateName,
			AgentTemplateUid: pair.AgentTemplateUID, HarnessName: pair.HarnessName,
			HarnessUid: pair.HarnessUID, DesiredRevision: pair.DesiredRevision,
			AgentTemplateLabels: labels,
		}))
	})
}

func (c *Client) UpsertRuntimeRevision(ctx context.Context, revision RuntimeRevision) error {
	if revision.AgentCard == nil {
		return fmt.Errorf("runtime revision %s has no Agent Card", revision.Revision)
	}
	card, err := proto.Marshal(revision.AgentCard)
	if err != nil {
		return fmt.Errorf("encode runtime revision Agent Card: %w", err)
	}
	rows, err := c.q.UpsertRuntimeRevision(ctx, dbgen.UpsertRuntimeRevisionParams{
		Revision: revision.Revision, Namespace: revision.Namespace,
		AgentTemplateName: revision.AgentTemplateName, AgentTemplateUid: revision.AgentTemplateUID,
		HarnessName: revision.HarnessName, HarnessUid: revision.HarnessUID,
		SourceSnapshot: revision.SourceSnapshot, AgentCard: card,
		EgressDestinations:    revision.EgressDestinations,
		ActorTemplateAtespace: revision.ActorTemplateAtespace, ActorTemplateName: revision.ActorTemplateName,
		ActorTemplateUid: revision.ActorTemplateUID,
	})
	if err != nil {
		return fmt.Errorf("upsert runtime revision %s: %w", revision.Revision, err)
	}
	if rows == 0 {
		return ErrObjectDeleting
	}
	return nil
}

func (c *Client) GetRuntimeRevision(ctx context.Context, revision string) (*RuntimeRevision, error) {
	row, err := c.q.GetRuntimeRevision(ctx, revision)
	if err != nil {
		return nil, fmt.Errorf("get runtime revision %s: %w", revision, notFoundOr(err))
	}
	return toRuntimeRevision(row)
}

func toRuntimeRevision(row dbgen.RuntimeRevision) (*RuntimeRevision, error) {
	card := &a2apb.AgentCard{}
	if err := proto.Unmarshal(row.AgentCard, card); err != nil {
		return nil, fmt.Errorf("decode runtime revision %s Agent Card: %w", row.Revision, err)
	}
	return &RuntimeRevision{
		Revision: row.Revision, Namespace: row.Namespace,
		AgentTemplateName: row.AgentTemplateName, AgentTemplateUID: row.AgentTemplateUid,
		HarnessName: row.HarnessName, HarnessUID: row.HarnessUid,
		SourceSnapshot: row.SourceSnapshot, AgentCard: card,
		EgressDestinations:    row.EgressDestinations,
		ActorTemplateAtespace: row.ActorTemplateAtespace, ActorTemplateName: row.ActorTemplateName,
		ActorTemplateUID: row.ActorTemplateUid,
	}, nil
}

func (c *Client) ListActorTemplateHarnesses(ctx context.Context) ([]ActorTemplateHarness, error) {
	rows, err := c.q.ListActorTemplateHarnesses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ActorTemplate harnesses: %w", err)
	}
	result := make([]ActorTemplateHarness, 0, len(rows))
	for _, row := range rows {
		result = append(result, ActorTemplateHarness{
			Atespace: row.ActorTemplateAtespace, Name: row.ActorTemplateName,
			UID: row.ActorTemplateUid, HarnessName: row.HarnessName,
		})
	}
	return result, nil
}

func (c *Client) MarkRuntimeRevisionSuccessful(ctx context.Context, pair AgentTemplateHarnessPair) error {
	revision := pair.DesiredRevision
	return runtimeRevisionError(c.q.MarkRuntimeRevisionSuccessful(ctx, dbgen.MarkRuntimeRevisionSuccessfulParams{
		Revision: &revision, Namespace: pair.Namespace,
		AgentTemplateUid: pair.AgentTemplateUID, HarnessUid: pair.HarnessUID,
	}))
}

// RetirePairIdentitiesExcept retires identities at keep's template/harness names
// except the supplied UID pair, preserving its last-good revision.
func (c *Client) RetirePairIdentitiesExcept(ctx context.Context, keep AgentTemplateHarnessPair) error {
	return c.q.RetirePairIdentitiesExcept(ctx, dbgen.RetirePairIdentitiesExceptParams{
		Namespace: keep.Namespace, AgentTemplateName: keep.AgentTemplateName, HarnessName: keep.HarnessName,
		KeepAgentTemplateUid: keep.AgentTemplateUID, KeepHarnessUid: keep.HarnessUID,
	})
}

func (c *Client) RetireAgentTemplateHarnessPairs(ctx context.Context, namespace, name string) error {
	return c.q.RetireAgentTemplateHarnessPairs(ctx, dbgen.RetireAgentTemplateHarnessPairsParams{Namespace: namespace, AgentTemplateName: name})
}

// RetireAllPairIdentities retires every UID pair at the given template/harness
// names. Use when the pair no longer exists.
func (c *Client) RetireAllPairIdentities(ctx context.Context, namespace, templateName, harnessName string) error {
	return c.q.RetireAllPairIdentities(ctx, dbgen.RetireAllPairIdentitiesParams{Namespace: namespace, AgentTemplateName: templateName, HarnessName: harnessName})
}

func (c *Client) RetireOtherAgentTemplateHarnessPairs(ctx context.Context, namespace, templateUID string, harnesses []string) error {
	return c.q.RetireOtherAgentTemplateHarnessPairs(ctx, dbgen.RetireOtherAgentTemplateHarnessPairsParams{
		Namespace: namespace, AgentTemplateUid: templateUID, HarnessNames: harnesses,
	})
}

func (c *Client) ListUnreferencedRuntimeRevisions(ctx context.Context) ([]RuntimeRevision, error) {
	rows, err := c.q.ListUnreferencedRuntimeRevisions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unreferenced runtime revisions: %w", err)
	}
	result := make([]RuntimeRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := toRuntimeRevision(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *revision)
	}
	return result, nil
}

// BeginRuntimeRevisionDeletion marks an unreferenced revision as deleting,
// preventing new references before runtime cleanup. Retrying returns the pending
// revision; nil means the revision is missing or still referenced.
func (c *Client) BeginRuntimeRevisionDeletion(ctx context.Context, revision string) (*RuntimeRevision, error) {
	var result *RuntimeRevision
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetRuntimeRevisionForUpdate(ctx, revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		rows, err := q.BeginRuntimeRevisionDeletion(ctx, revision)
		if err != nil || rows == 0 {
			return err
		}
		result, err = toRuntimeRevision(row)
		return err
	})
	return result, err
}

func (c *Client) DeleteRuntimeRevision(ctx context.Context, revision, actorTemplateUID string) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetRuntimeRevisionForUpdate(ctx, revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// A delayed collector must not finalize a newly recreated runtime at
		// the same digest after another collector finished the previous one.
		if row.DeletionStartedAt == nil || row.ActorTemplateUid != actorTemplateUID {
			return nil
		}
		if err := q.ReleaseRetiredRuntimeRevisionReferences(ctx, &revision); err != nil {
			return fmt.Errorf("release retired runtime revision references: %w", err)
		}
		return q.DeleteRuntimeRevision(ctx, revision)
	})
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
	// Columns own identity, authorization, revision retention, query labels and lifecycle.
	// Store updates write the same values to the payload in the same transaction.
	instance.State = apiv1alpha1.AgentInstanceState(state)
	instance.Operation = apiv1alpha1.AgentInstanceOperation(operationValue)
	instance.Id = row.ID.String()
	instance.ContextId = row.ContextID.String()
	instance.Creator = row.UserID
	instance.PreparedRevision = derefStr(row.PreparedRevision)
	instance.Labels = nil
	if len(row.Labels) > 0 {
		if err := json.Unmarshal(row.Labels, &instance.Labels); err != nil {
			return nil, fmt.Errorf("decode AgentInstance labels: %w", err)
		}
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
	return proto.Equal(instance.GetHarness(), request.GetHarness()) && proto.Equal(instance.GetAgentTemplate(), request.GetAgentTemplate())
}

func (c *Client) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.AgentInstance, requestID string) (*apiv1alpha1.AgentInstance, bool, error) {
	requestKey := dbgen.GetAgentInstanceByRequestParams{
		UserID: request.GetCreator(), RequestID: requestID,
	}
	existing, err := c.q.GetAgentInstanceByRequest(ctx, requestKey)
	if err == nil {
		instance, err := toAgentInstance(existing)
		if err == nil && !sameAgentInstanceRequest(instance, request) {
			return nil, false, ErrIdempotencyConflict
		}
		return instance, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get AgentInstance request: %w", err)
	}

	var row dbgen.AgentInstance
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err = insertAgentInstance(ctx, q, request, requestID)
		return runtimeRevisionError(err)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = c.q.GetAgentInstanceByRequest(ctx, requestKey)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent AgentInstance request: %w", err)
		}
		instance, err := toAgentInstance(existing)
		if err == nil && !sameAgentInstanceRequest(instance, request) {
			return nil, false, ErrIdempotencyConflict
		}
		return instance, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert AgentInstance: %w", err)
	}
	instance, err := toAgentInstance(row)
	return instance, err == nil, err
}

// insertAgentInstance reserves the conversation and pins its prepared revision.
// Callers own the transaction so execution linkage can commit with creation.
func insertAgentInstance(ctx context.Context, q *dbgen.Queries, request *apiv1alpha1.AgentInstance, requestID string) (dbgen.AgentInstance, error) {
	revision, err := q.GetLatestRuntimeRevisionForInstance(ctx, dbgen.GetLatestRuntimeRevisionForInstanceParams{
		HarnessNamespace: request.GetHarness().GetNamespace(), AgentTemplateNamespace: request.GetAgentTemplate().GetNamespace(), AgentTemplateName: request.GetAgentTemplate().GetName(), HarnessName: request.GetHarness().GetName(),
	})
	if err != nil {
		return dbgen.AgentInstance{}, fmt.Errorf("get latest successful runtime revision: %w", notFoundOr(err))
	}
	labels := map[string]string{}
	if err := json.Unmarshal(revision.AgentTemplateLabels, &labels); err != nil {
		return dbgen.AgentInstance{}, fmt.Errorf("decode AgentTemplate labels: %w", err)
	}
	instance := proto.CloneOf(request)
	contextID, historyID := uuid.New(), uuid.New()
	instance.ContextId = contextID.String()
	instance.PreparedRevision = revision.Revision
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE
	instance.Labels = labels
	instance.CreatedAt = timestamppb.New(revision.DbTime)
	instance.UpdatedAt = timestamppb.New(revision.DbTime)
	data, err := marshalAgentInstance(instance)
	if err != nil {
		return dbgen.AgentInstance{}, err
	}
	instanceID := uuid.MustParse(request.GetId())
	params := dbgen.InsertAgentInstanceParams{
		ID: instanceID, UserID: instance.Creator, RequestID: requestID,
		ContextID: contextID, HistoryID: historyID, PreparedRevision: &revision.Revision, Labels: revision.AgentTemplateLabels,
		Data: data,
	}
	if err := q.InsertA2AContext(ctx, dbgen.InsertA2AContextParams{
		ID: historyID, UserID: instance.Creator, ContextID: contextID,
	}); err != nil {
		return dbgen.AgentInstance{}, fmt.Errorf("insert A2A context: %w", err)
	}
	return q.InsertAgentInstance(ctx, params)
}

func (c *Client) ForkAgentInstance(ctx context.Context, checkpointID, userID, requestID, instanceID string) (*apiv1alpha1.AgentInstance, bool, error) {
	checkpointUUID := uuid.MustParse(checkpointID)
	requestKey := dbgen.GetAgentInstanceByRequestParams{UserID: userID, RequestID: requestID}
	existing, err := c.q.GetAgentInstanceByRequest(ctx, requestKey)
	if err == nil {
		if existing.SourceCheckpointID == nil || *existing.SourceCheckpointID != checkpointUUID {
			return nil, false, ErrIdempotencyConflict
		}
		instance, err := toAgentInstance(existing)
		return instance, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get fork request: %w", err)
	}

	instanceUUID := uuid.MustParse(instanceID)
	var row dbgen.AgentInstance
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		checkpoint, err := q.GetAgentInstanceCheckpointForUpdate(ctx, dbgen.GetAgentInstanceCheckpointForUpdateParams{
			ID: checkpointUUID, UserID: userID, State: new("READY"),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock checkpoint: %w", err)
		}
		if _, err := toAgentInstanceCheckpoint(checkpoint); err != nil {
			return err
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
		sourceContext, err := q.GetA2AContext(ctx, checkpoint.SourceHistoryID)
		if err != nil {
			return fmt.Errorf("get checkpoint context: %w", err)
		}
		historyID := uuid.New()
		now := timestamppb.Now()
		instance := &apiv1alpha1.AgentInstance{
			Id: instanceID, Creator: userID, ContextId: sourceContext.ContextID.String(),
			Name:             checkpoint.SourceName,
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
		if err := q.InsertA2AContext(ctx, dbgen.InsertA2AContextParams{ID: historyID, UserID: userID, ContextID: sourceContext.ContextID}); err != nil {
			return fmt.Errorf("insert fork A2A context: %w", err)
		}
		row, err = q.InsertAgentInstance(ctx, dbgen.InsertAgentInstanceParams{
			ID: instanceUUID, UserID: userID, RequestID: requestID,
			ContextID: sourceContext.ContextID, HistoryID: historyID, PreparedRevision: checkpoint.PreparedRevision,
			SourceCheckpointID: &checkpoint.ID, Labels: encodedLabels, Data: data,
		})
		if err != nil {
			return runtimeRevisionError(err)
		}

		events, err := q.ListAgentInstanceCheckpointEvents(ctx, checkpoint.ID)
		if err != nil {
			return fmt.Errorf("list checkpoint events: %w", err)
		}
		if len(events) == 0 || events[len(events)-1].Sequence != checkpoint.HistorySequence {
			return fmt.Errorf("checkpoint history boundary is missing")
		}
		// The fork owns the retained Tag, not the source's replaceable snapshot.
		boundaryEvent := &events[len(events)-1]
		if boundaryEvent.TaskID == nil || *boundaryEvent.TaskID != checkpoint.HeadTaskID || boundaryEvent.SnapshotUri == nil {
			return fmt.Errorf("checkpoint runtime boundary is inconsistent")
		}
		boundaryEvent.SnapshotAtespace = &checkpoint.SnapshotAtespace
		boundaryEvent.SnapshotUri = &checkpoint.SnapshotUri
		boundaryEvent.SnapshotContentScope = &checkpoint.SnapshotContentScope
		tasks, err := replayTaskEvents(events, sourceContext.ContextID.String())
		if err != nil {
			return fmt.Errorf("replay checkpoint events: %w", err)
		}
		if !slices.ContainsFunc(tasks, func(task dbgen.InsertCopiedAgentInstanceTaskParams) bool { return task.ID == checkpoint.HeadTaskID }) {
			return fmt.Errorf("checkpoint has no head task in its events")
		}
		var copiedHistorySequence int64
		sequences := make(map[int64]int64, len(events))
		for _, source := range events {
			copiedHistorySequence, err = q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
				HistoryID: historyID, TaskID: source.TaskID, MessageID: source.MessageID,
				Data: source.Data, CreatedAt: &source.CreatedAt, TaskPosition: source.TaskPosition,
				InitialMessageID: source.InitialMessageID, RequestHash: source.RequestHash,
				SnapshotAtespace: source.SnapshotAtespace, SnapshotUri: source.SnapshotUri, SnapshotContentScope: source.SnapshotContentScope,
			})
			if err != nil {
				return fmt.Errorf("copy checkpoint event %d: %w", source.Sequence, err)
			}
			sequences[source.Sequence] = copiedHistorySequence
		}
		for _, task := range tasks {
			task.HistoryID = historyID
			if task.HistorySequence != nil {
				sequence := sequences[*task.HistorySequence]
				task.HistorySequence = &sequence
			}
			if err := q.InsertCopiedAgentInstanceTask(ctx, task); err != nil {
				return fmt.Errorf("rebuild fork task %s: %w", task.ID, err)
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = c.q.GetAgentInstanceByRequest(ctx, requestKey)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent fork request: %w", err)
		}
		if existing.SourceCheckpointID == nil || *existing.SourceCheckpointID != checkpointUUID {
			return nil, false, ErrIdempotencyConflict
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

func (c *Client) GetAgentInstanceByID(ctx context.Context, id string) (*apiv1alpha1.AgentInstance, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid AgentInstance ID: %w", err)
	}
	row, err := c.q.GetAgentInstanceByID(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *Client) GetAgentInstance(ctx context.Context, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	row, err := c.q.GetAgentInstanceForUser(ctx, dbgen.GetAgentInstanceForUserParams{ID: uuid.MustParse(id), UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *Client) ListAgentInstances(ctx context.Context, query AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error) {
	matchLabels := query.MatchLabels
	if matchLabels == nil {
		matchLabels = map[string]string{}
	}
	labels, err := json.Marshal(matchLabels)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentInstance label selector: %w", err)
	}
	rows, err := c.q.ListAgentInstances(ctx, dbgen.ListAgentInstancesParams{
		UserID: query.UserID, AllUsers: query.AllUsers,
		AfterID: query.AfterID, MatchLabels: labels,
		AgentTemplate: query.AgentTemplate.GetName(), AgentTemplateNamespace: query.AgentTemplate.GetNamespace(), Harness: query.Harness.GetName(), HarnessNamespace: query.Harness.GetNamespace(),
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

// UpdateAgentInstanceName serializes with lifecycle updates so neither loses the other's fields.
func (c *Client) UpdateAgentInstanceName(ctx context.Context, id, userID, name string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetAgentInstanceForUpdate(ctx, uuid.MustParse(id))
		if err != nil {
			return notFoundOr(err)
		}
		if row.UserID != userID {
			return ErrNotFound
		}
		instance, err := toAgentInstance(row)
		if err != nil {
			return err
		}
		instance.Name = name
		instance.UpdatedAt = timestamppb.Now()
		data, err := marshalAgentInstance(instance)
		if err != nil {
			return err
		}
		row, err = q.UpdateAgentInstanceName(ctx, dbgen.UpdateAgentInstanceNameParams{ID: row.ID, UserID: userID, Data: data})
		if err != nil {
			return err
		}
		result, err = toAgentInstance(row)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("rename AgentInstance %s: %w", id, err)
	}
	return result, nil
}

func (c *Client) MarkAgentInstanceReady(ctx context.Context, id, authority string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetAgentInstanceForUpdate(ctx, uuid.MustParse(id))
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstance(row)
		if err != nil || row.State != "CREATING" || row.Operation != "CREATE" {
			return err
		}
		result.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
		result.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		result.A2AAuthority = authority
		result.Failure = nil
		result.UpdatedAt = timestamppb.Now()
		data, err := marshalAgentInstance(result)
		if err != nil {
			return err
		}
		_, err = q.MarkAgentInstanceReady(ctx, dbgen.MarkAgentInstanceReadyParams{ID: row.ID, Data: data})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("mark AgentInstance %s ready: %w", id, err)
	}
	return result, nil
}

func (c *Client) TransitionAgentInstance(
	ctx context.Context,
	instance *apiv1alpha1.AgentInstance,
	expectedState apiv1alpha1.AgentInstanceState,
	expectedOperation apiv1alpha1.AgentInstanceOperation,
) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetAgentInstanceForUpdate(ctx, uuid.MustParse(instance.GetId()))
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstance(row)
		if err != nil {
			return err
		}
		if result.State != expectedState || result.Operation != expectedOperation {
			return ErrAgentInstanceConflict
		}
		// Only lifecycle fields belong to this operation. Keep concurrent renames,
		// immutable indexed fields and unknown protobuf fields from the locked row.
		next := proto.Clone(result).(*apiv1alpha1.AgentInstance)
		next.State, next.Operation = instance.State, instance.Operation
		next.A2AAuthority = instance.A2AAuthority
		next.Failure = instance.Failure
		next.UpdatedAt = timestamppb.Now()
		data, err := marshalAgentInstance(next)
		if err != nil {
			return err
		}
		row, err = q.TransitionAgentInstance(ctx, dbgen.TransitionAgentInstanceParams{
			ID: row.ID, Data: data,
			ExpectedState: agentInstanceStateName(expectedState), ExpectedOperation: agentInstanceOperationName(expectedOperation),
			NextState: agentInstanceStateName(next.State), NextOperation: agentInstanceOperationName(next.Operation),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentInstanceConflict
		}
		if err != nil {
			return err
		}
		result, err = toAgentInstance(row)
		return err
	})
	if err != nil {
		return result, fmt.Errorf("transition AgentInstance %s: %w", instance.GetId(), err)
	}
	return result, nil
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

func (c *Client) DeleteAgentInstance(ctx context.Context, id string) error {
	if err := c.q.DeleteAgentInstance(ctx, uuid.MustParse(id)); err != nil {
		return fmt.Errorf("delete AgentInstance %s: %w", id, err)
	}
	return nil
}

func toAgentInstanceShare(row dbgen.AgentInstanceShare) (*apiv1alpha1.AgentInstanceShare, error) {
	share := &apiv1alpha1.AgentInstanceShare{}
	if err := proto.Unmarshal(row.Data, share); err != nil {
		return nil, fmt.Errorf("decode AgentInstance share %s: %w", row.ID, err)
	}
	if share.GetId() != row.ID.String() || share.GetAgentInstanceId() != row.InstanceID.String() ||
		strings.TrimPrefix(share.GetPermission().String(), "AGENT_INSTANCE_SHARE_PERMISSION_") != row.Permission {
		return nil, fmt.Errorf("AgentInstance share %s payload disagrees with indexed columns", row.ID)
	}
	return share, nil
}

func (c *Client) CreateAgentInstanceShare(ctx context.Context, share *apiv1alpha1.AgentInstanceShare, tokenHash []byte) (*apiv1alpha1.AgentInstanceShare, error) {
	if share == nil {
		return nil, fmt.Errorf("missing AgentInstance share")
	}
	value := proto.Clone(share).(*apiv1alpha1.AgentInstanceShare)
	value.CreatedAt = timestamppb.Now()
	data, err := proto.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode AgentInstance share: %w", err)
	}
	row, err := c.q.CreateAgentInstanceShare(ctx, dbgen.CreateAgentInstanceShareParams{
		ID: uuid.MustParse(value.Id), InstanceID: uuid.MustParse(value.AgentInstanceId),
		Permission: strings.TrimPrefix(value.Permission.String(), "AGENT_INSTANCE_SHARE_PERMISSION_"), TokenHash: tokenHash, Data: data,
	})
	if err != nil {
		return nil, fmt.Errorf("create AgentInstance share: %w", err)
	}
	return toAgentInstanceShare(row)
}

// GetAgentInstanceShareByTokenHash returns the share and its instance's owner ID.
// Only the digest is stored; the plaintext token is returned once by the service.
func (c *Client) GetAgentInstanceShareByTokenHash(ctx context.Context, tokenHash []byte) (*apiv1alpha1.AgentInstanceShare, string, error) {
	row, err := c.q.GetAgentInstanceShareByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, "", fmt.Errorf("get AgentInstance share by token: %w", notFoundOr(err))
	}
	share, err := toAgentInstanceShare(dbgen.AgentInstanceShare{ID: row.ID, InstanceID: row.InstanceID, Permission: row.Permission, Data: row.Data})
	if err != nil {
		return nil, "", err
	}
	return share, row.OwnerUserID, nil
}

func (c *Client) ListAgentInstanceShares(ctx context.Context, instanceID, userID, afterID string, limit int) ([]*apiv1alpha1.AgentInstanceShare, error) {
	rows, err := c.q.ListAgentInstanceShares(ctx, dbgen.ListAgentInstanceSharesParams{
		InstanceID: uuid.MustParse(instanceID), UserID: userID, AfterID: afterID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentInstance shares: %w", err)
	}
	result := make([]*apiv1alpha1.AgentInstanceShare, 0, len(rows))
	for _, row := range rows {
		share, err := toAgentInstanceShare(row)
		if err != nil {
			return nil, err
		}
		result = append(result, share)
	}
	return result, nil
}

func (c *Client) DeleteAgentInstanceShare(ctx context.Context, id, userID string) error {
	count, err := c.q.DeleteAgentInstanceShare(ctx, dbgen.DeleteAgentInstanceShareParams{ID: uuid.MustParse(id), UserID: userID})
	if err != nil {
		return fmt.Errorf("delete AgentInstance share %s: %w", id, err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateAgentInstanceTask reserves the instance's single active-task slot.
func (c *Client) CreateAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, task *a2a.Task) (*a2a.Task, bool, error) {
	if task == nil || len(task.History) == 0 || task.History[0] == nil || task.History[0].ID == "" {
		return nil, false, fmt.Errorf("AgentInstance task requires an initial message")
	}
	message := task.History[0]
	initial, creation, err := taskTransition(nil, task, task)
	if err != nil {
		return nil, false, err
	}
	taskData, err := proto.Marshal(initial)
	if err != nil {
		return nil, false, err
	}
	creationData, err := proto.Marshal(creation)
	if err != nil {
		return nil, false, err
	}

	result := task
	created := false
	var historyID uuid.UUID
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		instance, err := q.GetAgentInstanceForUpdate(ctx, uuid.MustParse(instanceID))
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", instanceID, err)
		}
		if instance.State != "READY" || instance.Operation != "NONE" {
			return ErrAgentInstanceTaskConflict
		}
		historyID = instance.HistoryID
		if task.ContextID != instance.ContextID.String() {
			return fmt.Errorf("task context does not match AgentInstance")
		}
		row, err := q.CreateAgentInstanceTask(ctx, dbgen.CreateAgentInstanceTaskParams{
			HistoryID: historyID, ID: string(task.ID), State: string(task.Status.State),
			StatusTimestamp: task.Status.Timestamp, Data: taskData,
			InitialMessageID: &message.ID, RequestHash: requestHash,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err := q.GetAgentInstanceTaskByMessageID(ctx, dbgen.GetAgentInstanceTaskByMessageIDParams{
				HistoryID: historyID, InitialMessageID: &message.ID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrAgentInstanceTaskConflict
			}
			if err != nil {
				return fmt.Errorf("get AgentInstance task for message %s: %w", message.ID, err)
			}
			if !bytes.Equal(row.RequestHash, requestHash) {
				return ErrIdempotencyConflict
			}
			result, err = unmarshalAgentInstanceTask(row.Data)
			if err != nil {
				return err
			}
			return loadAgentInstanceTaskHistories(ctx, q, historyID, []*a2a.Task{result})
		}
		if err != nil {
			if isActiveTaskConflict(err) {
				return ErrAgentInstanceTaskConflict
			}
			return fmt.Errorf("create AgentInstance task %s: %w", task.ID, err)
		}
		created = true
		if _, err := q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
			HistoryID: historyID, TaskID: &row.ID, Data: creationData, CreatedAt: &row.CreatedAt,
			TaskPosition: &row.Position, InitialMessageID: row.InitialMessageID, RequestHash: row.RequestHash,
		}); err != nil {
			return fmt.Errorf("record task creation: %w", err)
		}
		_, err = storeAgentInstanceTaskMessages(ctx, q, historyID, string(task.ID), task.ContextID, task.History)
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

func (c *Client) agentInstanceHistoryID(ctx context.Context, instanceID string) (uuid.UUID, error) {
	instance, err := c.q.GetAgentInstanceByID(ctx, uuid.MustParse(instanceID))
	if err != nil {
		return uuid.Nil, fmt.Errorf("get AgentInstance history: %w", notFoundOr(err))
	}
	return instance.HistoryID, nil
}

func (c *Client) GetActiveAgentInstanceTask(ctx context.Context, instanceID string) (*a2a.Task, error) {
	historyID, err := c.agentInstanceHistoryID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	row, err := c.q.GetActiveAgentInstanceTask(ctx, historyID)
	if err != nil {
		return nil, fmt.Errorf("get active AgentInstance task: %w", notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err == nil {
		err = loadAgentInstanceTaskHistories(ctx, c.q, historyID, []*a2a.Task{task})
	}
	return task, err
}

// InterruptActiveAgentInstanceTask atomically fails taskID only if it is still the
// instance's active task.
func (c *Client) InterruptActiveAgentInstanceTask(ctx context.Context, instanceID, taskID string) (bool, error) {
	interruptedTask := false
	historyID, err := c.agentInstanceHistoryID(ctx, instanceID)
	if err != nil {
		return false, err
	}
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetActiveAgentInstanceTaskForUpdate(ctx, historyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock active AgentInstance task: %w", err)
		}
		if row.ID != taskID {
			return nil
		}
		task := &a2apb.Task{}
		if err := proto.Unmarshal(row.Data, task); err != nil {
			return fmt.Errorf("decode interrupted task: %w", err)
		}
		if _, err := pbconv.FromProtoTask(task); err != nil {
			return err
		}
		interrupted := &a2apb.Message{
			MessageId: uuid.NewString(), TaskId: task.Id, ContextId: task.ContextId, Role: a2apb.Role_ROLE_AGENT,
			Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: taskInterruptedMessage}}},
		}
		now := time.Now()
		status := proto.Clone(task.Status).(*a2apb.TaskStatus)
		status.State = a2apb.TaskState_TASK_STATE_FAILED
		status.Message = interrupted
		status.Timestamp = timestamppb.New(now)
		messages := append(task.History, interrupted)
		event := &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_StatusUpdate{StatusUpdate: &a2apb.TaskStatusUpdateEvent{
			TaskId: task.Id, ContextId: task.ContextId, Status: status,
		}}}
		task, err = applyTaskEvent(task, event)
		if err != nil {
			return err
		}
		task.History = nil
		data, err := proto.Marshal(task)
		if err != nil {
			return err
		}
		if _, err := q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{
			HistoryID: historyID, ID: task.Id, State: string(a2a.TaskStateFailed),
			StatusTimestamp: &now, Data: data,
		}); err != nil {
			return fmt.Errorf("interrupt AgentInstance task %s: %w", task.Id, err)
		}
		if _, err := storeProtoTaskMessages(ctx, q, historyID, task.Id, task.ContextId, messages); err != nil {
			return fmt.Errorf("record AgentInstance task interruption: %w", err)
		}
		eventData, err := proto.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
			HistoryID: historyID, TaskID: &task.Id, Data: eventData,
		}); err != nil {
			return fmt.Errorf("record task interruption status: %w", err)
		}
		interruptedTask = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return interruptedTask, nil
}

func (c *Client) StoreAgentInstanceTaskEvent(ctx context.Context, instanceID string, task *a2a.Task, event a2a.Event, snapshot *AgentInstanceTaskSnapshot) error {
	if task == nil || event == nil {
		return fmt.Errorf("task and event are required")
	}
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		// Serialize task transitions with checkpoint reservation, without holding
		// a transaction across runtime or snapshot network calls.
		instance, err := q.GetAgentInstanceForUpdate(ctx, uuid.MustParse(instanceID))
		if err != nil {
			return notFoundOr(err)
		}
		historyID := instance.HistoryID
		if event == nil || event.TaskInfo().ContextID != instance.ContextID.String() ||
			(task != nil && task.ContextID != instance.ContextID.String()) {
			return fmt.Errorf("task event context does not match AgentInstance")
		}
		creating, err := q.HasCreatingAgentInstanceCheckpoint(ctx, historyID)
		if err != nil {
			return err
		}
		if creating {
			return ErrAgentInstanceConflict
		}
		var sequence int64
		var stored *a2apb.Task
		var durable *a2apb.StreamResponse
		var taskRow dbgen.AgentInstanceTask
		newTask := false
		if task != nil {
			if row, err := q.GetAgentInstanceTaskForUpdate(ctx, dbgen.GetAgentInstanceTaskForUpdateParams{HistoryID: historyID, ID: string(task.ID)}); err == nil {
				stored = &a2apb.Task{}
				if err := proto.Unmarshal(row.Data, stored); err != nil {
					return fmt.Errorf("decode stored task: %w", err)
				}
				if _, err := pbconv.FromProtoTask(stored); err != nil {
					return err
				}
				messages := stored.History
				// Replies and status updates archive the old status message before replacing it.
				// Use the stored protobuf so its nested fields survive in history too.
				switch event.(type) {
				case *a2a.Message, *a2a.TaskStatusUpdateEvent:
					if message := stored.Status.Message; message != nil {
						message.TaskId, message.ContextId = string(task.ID), task.ContextID
						messages = append(messages, message)
					}
				}
				if len(messages) > 0 {
					sequence, err = storeProtoTaskMessages(ctx, q, historyID, string(task.ID), task.ContextID, messages)
					if err != nil {
						return fmt.Errorf("archive AgentInstance task history: %w", err)
					}
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("get AgentInstance task %s: %w", task.ID, err)
			}
			newTask = stored == nil
			var err error
			stored, durable, err = taskTransition(stored, task, event)
			if err != nil {
				return err
			}
			data, err := proto.Marshal(stored)
			if err != nil {
				return err
			}
			taskRow, err = q.UpsertAgentInstanceTask(ctx, dbgen.UpsertAgentInstanceTaskParams{
				HistoryID: historyID, ID: string(task.ID), State: string(task.Status.State),
				StatusTimestamp: task.Status.Timestamp, Data: data,
			})
			if err != nil {
				if isActiveTaskConflict(err) {
					return ErrAgentInstanceTaskConflict
				}
				return fmt.Errorf("store AgentInstance task %s: %w", task.ID, err)
			}
		}
		if newTask {
			data, err := proto.Marshal(durable)
			if err != nil {
				return err
			}
			sequence, err = q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
				HistoryID: historyID, TaskID: &taskRow.ID, Data: data, CreatedAt: &taskRow.CreatedAt,
				TaskPosition: &taskRow.Position, InitialMessageID: taskRow.InitialMessageID, RequestHash: taskRow.RequestHash,
			})
			if err != nil {
				return fmt.Errorf("record task creation: %w", err)
			}
		}

		messages := agentInstanceTaskEventMessages(task, event)
		if len(messages) > 0 {
			var err error
			sequence, err = storeAgentInstanceTaskMessages(ctx, q, historyID, string(event.TaskInfo().TaskID), instance.ContextID.String(), messages)
			if err != nil {
				return fmt.Errorf("store AgentInstance task history: %w", err)
			}
		}
		if durable != nil && (!newTask || snapshot != nil) {
			eventData, err := proto.Marshal(durable)
			if err != nil {
				return err
			}
			insert := dbgen.InsertAgentInstanceTaskEventParams{
				HistoryID: historyID, TaskID: strPtrIfNotEmpty(string(event.TaskInfo().TaskID)), Data: eventData,
			}
			if snapshot != nil {
				insert.SnapshotAtespace, insert.SnapshotUri, insert.SnapshotContentScope = &snapshot.Atespace, &snapshot.URI, &snapshot.ContentScope
			}
			sequence, err = q.InsertAgentInstanceTaskEvent(ctx, insert)
			if err != nil {
				return fmt.Errorf("store AgentInstance task event: %w", err)
			}
		}

		if snapshot != nil {
			if sequence == 0 {
				return fmt.Errorf("snapshot has no history boundary")
			}
			if err := q.SetAgentInstanceTaskSnapshot(ctx, dbgen.SetAgentInstanceTaskSnapshotParams{
				HistoryID: historyID, ID: string(task.ID), SnapshotAtespace: &snapshot.Atespace,
				SnapshotUri:          &snapshot.URI,
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

func (c *Client) GetAgentInstanceTask(ctx context.Context, instanceID, taskID string) (*a2a.Task, error) {
	historyID, err := c.agentInstanceHistoryID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	row, err := c.q.GetAgentInstanceTask(ctx, dbgen.GetAgentInstanceTaskParams{HistoryID: historyID, ID: taskID})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance task %s: %w", taskID, notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err == nil {
		err = loadAgentInstanceTaskHistories(ctx, c.q, historyID, []*a2a.Task{task})
	}
	return task, err
}

func (c *Client) ListAgentInstanceTasks(ctx context.Context, instanceID, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int) ([]*a2a.Task, int, error) {
	historyID, err := c.agentInstanceHistoryID(ctx, instanceID)
	if err != nil {
		return nil, 0, err
	}
	params := dbgen.CountAgentInstanceTasksParams{
		HistoryID: historyID, State: string(state), StatusTimestampAfter: statusTimestampAfter,
	}
	total, err := c.q.CountAgentInstanceTasks(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("count AgentInstance tasks: %w", err)
	}
	rows, err := c.q.ListAgentInstanceTasks(ctx, dbgen.ListAgentInstanceTasksParams{
		HistoryID: historyID, AfterID: afterID, State: params.State,
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
	if err := loadAgentInstanceTaskHistories(ctx, c.q, historyID, tasks); err != nil {
		return nil, 0, err
	}
	return tasks, int(total), nil
}

// ReserveAgentInstanceCheckpoint returns the checkpoint and its immutable snapshot
// reference from the same transaction, including on idempotent retries.
func (c *Client) ReserveAgentInstanceCheckpoint(ctx context.Context, checkpoint *apiv1alpha1.Checkpoint, userID, requestID string) (*apiv1alpha1.Checkpoint, *AgentInstanceTaskSnapshot, error) {
	if checkpoint == nil {
		return nil, nil, fmt.Errorf("missing checkpoint")
	}
	var result *apiv1alpha1.Checkpoint
	var snapshot *AgentInstanceTaskSnapshot
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		existing, err := q.GetAgentInstanceCheckpointByRequest(ctx, dbgen.GetAgentInstanceCheckpointByRequestParams{
			UserID: userID, RequestID: requestID,
		})
		if err == nil {
			if existing.SourceInstanceID != uuid.MustParse(checkpoint.GetAgentInstanceId()) {
				return ErrIdempotencyConflict
			}
			snapshot = checkpointSnapshot(existing)
			result, err = toAgentInstanceCheckpoint(existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get AgentInstance checkpoint by request: %w", err)
		}

		instance, err := q.GetAgentInstanceForUpdate(ctx, uuid.MustParse(checkpoint.GetAgentInstanceId()))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (instance.UserID != userID)) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", uuid.MustParse(checkpoint.GetAgentInstanceId()), err)
		}
		if instance.State != "READY" || instance.Operation != "NONE" {
			return ErrAgentInstanceConflict
		}
		source, err := toAgentInstance(instance)
		if err != nil {
			return err
		}
		boundary, err := q.GetLatestQuiescentAgentInstanceTask(ctx, instance.HistoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentInstanceNotQuiescent
		}
		if err != nil {
			return fmt.Errorf("get latest AgentInstance task boundary: %w", err)
		}
		if boundary.SnapshotAtespace == nil || boundary.SnapshotUri == nil ||
			boundary.SnapshotContentScope == nil || boundary.HistorySequence == nil {
			return ErrAgentInstanceNotQuiescent
		}

		value := proto.Clone(checkpoint).(*apiv1alpha1.Checkpoint)
		value.HeadTaskId = boundary.ID
		value.HistorySequence = uint64(*boundary.HistorySequence)
		value.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING
		value.CreatedAt = timestamppb.Now()
		value.Failure = nil
		data, err := proto.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode checkpoint: %w", err)
		}
		row, err := q.InsertAgentInstanceCheckpoint(ctx, dbgen.InsertAgentInstanceCheckpointParams{
			ID: uuid.MustParse(checkpoint.GetId()), SourceInstanceID: uuid.MustParse(checkpoint.GetAgentInstanceId()),
			UserID: userID, RequestID: requestID, HeadTaskID: boundary.ID,
			HistorySequence: *boundary.HistorySequence, SnapshotAtespace: *boundary.SnapshotAtespace,
			SnapshotUri:          *boundary.SnapshotUri,
			SnapshotContentScope: *boundary.SnapshotContentScope,
			SourceHistoryID:      instance.HistoryID, PreparedRevision: instance.PreparedRevision,
			SourceLabels: instance.Labels, SourceName: source.GetName(), Data: data,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			existing, existingErr := q.GetAgentInstanceCheckpointByRequest(ctx, dbgen.GetAgentInstanceCheckpointByRequestParams{
				UserID: userID, RequestID: requestID,
			})
			if existingErr == nil {
				if existing.SourceInstanceID != uuid.MustParse(checkpoint.GetAgentInstanceId()) {
					return ErrIdempotencyConflict
				}
				snapshot = checkpointSnapshot(existing)
				result, existingErr = toAgentInstanceCheckpoint(existing)
				return existingErr
			}
			if errors.Is(existingErr, pgx.ErrNoRows) {
				return ErrAgentInstanceConflict
			}
			return fmt.Errorf("get conflicting AgentInstance checkpoint request: %w", existingErr)
		}
		if err != nil {
			return fmt.Errorf("insert AgentInstance checkpoint: %w", runtimeRevisionError(err))
		}
		snapshot = checkpointSnapshot(row)
		result, err = toAgentInstanceCheckpoint(row)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("reserve AgentInstance checkpoint: %w", err)
	}
	return result, snapshot, nil
}

func (c *Client) FinalizeAgentInstanceCheckpoint(ctx context.Context, id, tagUID, snapshotURI, failure string) (*apiv1alpha1.Checkpoint, error) {
	if (tagUID == "") == (failure == "") || (tagUID == "") != (snapshotURI == "") {
		return nil, fmt.Errorf("finalize AgentInstance checkpoint requires tag UID and snapshot URI, or failure")
	}
	var result *apiv1alpha1.Checkpoint
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetAgentInstanceCheckpointForUpdate(ctx, dbgen.GetAgentInstanceCheckpointForUpdateParams{ID: uuid.MustParse(id), AllUsers: true})
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstanceCheckpoint(row)
		if err != nil {
			return err
		}
		if row.State != "CREATING" {
			if (row.State == "READY" && row.TagUid == tagUID && row.SnapshotUri == snapshotURI && failure == "") ||
				(row.State == "FAILED" && tagUID == "" && result.GetFailure().GetMessage() == failure) {
				return nil
			}
			return ErrNotFound
		}
		result.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY
		if failure != "" {
			result.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_FAILED
			result.Failure = &apiv1alpha1.Failure{Reason: "SnapshotTagFailed", Message: failure}
		}
		data, err := proto.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode checkpoint: %w", err)
		}
		row, err = q.FinalizeAgentInstanceCheckpoint(ctx, dbgen.FinalizeAgentInstanceCheckpointParams{ID: row.ID, TagUid: tagUID, SnapshotUri: snapshotURI, Data: data})
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstanceCheckpoint(row)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("finalize AgentInstance checkpoint: %w", err)
	}
	return result, nil
}

func (c *Client) GetAgentInstanceCheckpoint(ctx context.Context, id, userID string) (*apiv1alpha1.Checkpoint, error) {
	row, err := c.q.GetAgentInstanceCheckpoint(ctx, dbgen.GetAgentInstanceCheckpointParams{ID: uuid.MustParse(id), UserID: userID, State: new("READY")})
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance checkpoint: %w", notFoundOr(err))
	}
	return toAgentInstanceCheckpoint(row)
}

// GetAgentInstanceCheckpointSnapshot returns the private snapshot reference and
// tag UID for lifecycle workflows. Finalization replaces the source URI with the
// retained Tag copy; ready references are immutable.
func (c *Client) GetAgentInstanceCheckpointSnapshot(ctx context.Context, id, userID string) (*AgentInstanceTaskSnapshot, string, error) {
	row, err := c.q.GetAgentInstanceCheckpoint(ctx, dbgen.GetAgentInstanceCheckpointParams{ID: uuid.MustParse(id), UserID: userID})
	if err != nil {
		return nil, "", fmt.Errorf("get checkpoint snapshot: %w", notFoundOr(err))
	}
	if _, err := toAgentInstanceCheckpoint(row); err != nil {
		return nil, "", err
	}
	return checkpointSnapshot(row), row.TagUid, nil
}

func checkpointSnapshot(row dbgen.AgentInstanceCheckpoint) *AgentInstanceTaskSnapshot {
	return &AgentInstanceTaskSnapshot{
		Atespace: row.SnapshotAtespace, URI: row.SnapshotUri, ContentScope: row.SnapshotContentScope,
	}
}

func (c *Client) ListAgentInstanceCheckpoints(ctx context.Context, instanceID, userID, afterID string, limit int) ([]*apiv1alpha1.Checkpoint, error) {
	rows, err := c.q.ListAgentInstanceCheckpoints(ctx, dbgen.ListAgentInstanceCheckpointsParams{
		SourceInstanceID: uuid.MustParse(instanceID), UserID: userID, AfterID: afterID, PageSize: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list AgentInstance checkpoints: %w", err)
	}
	result := make([]*apiv1alpha1.Checkpoint, len(rows))
	for i := range rows {
		checkpoint, err := toAgentInstanceCheckpoint(rows[i])
		if err != nil {
			return nil, err
		}
		result[i] = checkpoint
	}
	return result, nil
}

// BeginDeleteAgentInstanceCheckpoint hides the checkpoint and returns the snapshot
// and tag identity needed for cleanup after the transaction commits.
func (c *Client) BeginDeleteAgentInstanceCheckpoint(ctx context.Context, id, userID string) (*AgentInstanceTaskSnapshot, string, error) {
	var snapshot *AgentInstanceTaskSnapshot
	var tagUID string
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetAgentInstanceCheckpointForUpdate(ctx, dbgen.GetAgentInstanceCheckpointForUpdateParams{ID: uuid.MustParse(id), UserID: userID})
		if err != nil {
			return notFoundOr(err)
		}
		checkpoint, err := toAgentInstanceCheckpoint(row)
		if err != nil {
			return err
		}
		checkpoint.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING
		data, err := proto.Marshal(checkpoint)
		if err != nil {
			return fmt.Errorf("encode checkpoint: %w", err)
		}
		row, err = q.BeginDeleteAgentInstanceCheckpoint(ctx, dbgen.BeginDeleteAgentInstanceCheckpointParams{ID: row.ID, UserID: userID, Data: data})
		if err != nil {
			return notFoundOr(err)
		}
		snapshot, tagUID = checkpointSnapshot(row), row.TagUid
		_, err = toAgentInstanceCheckpoint(row)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("begin delete AgentInstance checkpoint: %w", err)
	}
	return snapshot, tagUID, nil
}

func (c *Client) DeleteAgentInstanceCheckpoint(ctx context.Context, id, userID string) error {
	_, err := c.q.DeleteAgentInstanceCheckpoint(ctx, dbgen.DeleteAgentInstanceCheckpointParams{ID: uuid.MustParse(id), UserID: userID})
	if err != nil {
		return fmt.Errorf("delete AgentInstance checkpoint: %w", err)
	}
	return nil
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

func storeAgentInstanceTaskMessages(ctx context.Context, q *dbgen.Queries, historyID uuid.UUID, taskID, contextID string, messages []*a2a.Message) (int64, error) {
	converted := make([]*a2apb.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil || message.ID == "" {
			return 0, fmt.Errorf("AgentInstance task history contains a message without an ID")
		}
		event, err := pbconv.ToProtoStreamResponse(message)
		if err != nil {
			return 0, err
		}
		converted = append(converted, event.GetMessage())
	}
	return storeProtoTaskMessages(ctx, q, historyID, taskID, contextID, converted)
}

func storeProtoTaskMessages(ctx context.Context, q *dbgen.Queries, historyID uuid.UUID, taskID, contextID string, messages []*a2apb.Message) (int64, error) {
	var sequence int64
	for _, message := range messages {
		if message.GetMessageId() == "" {
			return 0, fmt.Errorf("AgentInstance task history contains a message without an ID")
		}
		message = proto.Clone(message).(*a2apb.Message)
		if (message.TaskId != "" && message.TaskId != taskID) || (message.ContextId != "" && message.ContextId != contextID) {
			return 0, fmt.Errorf("history message changes task identity")
		}
		message.TaskId, message.ContextId = taskID, contextID
		data, err := proto.Marshal(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Message{Message: message}})
		if err != nil {
			return 0, err
		}
		sequence, err = q.InsertAgentInstanceTaskEvent(ctx, dbgen.InsertAgentInstanceTaskEventParams{
			HistoryID: historyID, TaskID: &taskID, MessageID: &message.MessageId, Data: data,
		})
		if err != nil {
			return 0, err
		}
	}
	return sequence, nil
}

func loadAgentInstanceTaskHistories(ctx context.Context, q *dbgen.Queries, historyID uuid.UUID, tasks []*a2a.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, len(tasks))
	byID := make(map[string]*a2a.Task, len(tasks))
	for index, task := range tasks {
		ids[index] = string(task.ID)
		byID[string(task.ID)] = task
	}
	rows, err := q.ListAgentInstanceTaskHistory(ctx, dbgen.ListAgentInstanceTaskHistoryParams{HistoryID: historyID, TaskIds: ids})
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

// ── Tools ─────────────────────────────────────────────────────────────────────

func (c *Client) GetTool(ctx context.Context, name string) (*Tool, error) {
	row, err := c.q.GetTool(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool %s: %w", name, notFoundOr(err))
	}
	return toTool(row), nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	return c.listTools(ctx, dbgen.ListToolsParams{})
}

func (c *Client) ListToolsForServer(ctx context.Context, serverName, groupKind string) ([]Tool, error) {
	return c.listTools(ctx, dbgen.ListToolsParams{ServerName: &serverName, GroupKind: &groupKind})
}

func (c *Client) listTools(ctx context.Context, params dbgen.ListToolsParams) ([]Tool, error) {
	rows, err := c.q.ListTools(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	tools := make([]Tool, len(rows))
	for i, r := range rows {
		tools[i] = *toTool(r)
	}
	return tools, nil
}

func (c *Client) DeleteToolsForServer(ctx context.Context, serverName, groupKind string) error {
	return c.q.SoftDeleteToolsForServer(ctx, dbgen.SoftDeleteToolsForServerParams{ServerName: serverName, GroupKind: groupKind})
}

func (c *Client) RefreshToolsForServer(ctx context.Context, serverName, groupKind string, tools ...*v1alpha3.MCPTool) error {
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

func (c *Client) GetToolServer(ctx context.Context, name string) (*ToolServer, error) {
	row, err := c.q.GetToolServer(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get tool server %s: %w", name, notFoundOr(err))
	}
	return toToolServer(row), nil
}

func (c *Client) ListToolServers(ctx context.Context) ([]ToolServer, error) {
	rows, err := c.q.ListToolServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tool servers: %w", err)
	}
	servers := make([]ToolServer, len(rows))
	for i, r := range rows {
		servers[i] = *toToolServer(r)
	}
	return servers, nil
}

func (c *Client) StoreToolServer(ctx context.Context, ts *ToolServer) (*ToolServer, error) {
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

func (c *Client) DeleteToolServer(ctx context.Context, serverName, groupKind string) error {
	return c.q.SoftDeleteToolServer(ctx, dbgen.SoftDeleteToolServerParams{Name: serverName, GroupKind: groupKind})
}

// ── Agent Memory (vector search) ──────────────────────────────────────────────

func (c *Client) StoreAgentMemory(ctx context.Context, memory *Memory) error {
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

func (c *Client) StoreAgentMemories(ctx context.Context, memories []*Memory) error {
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

func (c *Client) SearchAgentMemory(ctx context.Context, agentName, userID string, embedding pgvector.Vector, limit int) ([]AgentMemorySearchResult, error) {
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

	results := make([]AgentMemorySearchResult, len(rows))
	for i, r := range rows {
		score, _ := r.Score.(float64)
		results[i] = AgentMemorySearchResult{
			Memory: Memory{
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
		if err := c.q.IncrementMemoryAccessCountForUpdate(ctx, ids); err != nil {
			logging.FromContext(ctx).WarnContext(ctx, "failed to increment memory access count", "error", err)
		}
	}

	return results, nil
}

func (c *Client) ListAgentMemories(ctx context.Context, agentName, userID string) ([]Memory, error) {
	normalized := strings.ReplaceAll(agentName, "-", "_")
	rows, err := c.q.ListAgentMemories(ctx, dbgen.ListAgentMemoriesParams{
		AgentName:   &agentName,
		AgentName_2: &normalized,
		UserID:      &userID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list agent memories: %w", err)
	}
	memories := make([]Memory, len(rows))
	for i, r := range rows {
		memories[i] = *toMemory(r)
	}
	return memories, nil
}

func (c *Client) DeleteAgentMemory(ctx context.Context, agentName, userID string) error {
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

func (c *Client) PruneExpiredMemories(ctx context.Context) error {
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

func toTool(r dbgen.Tool) *Tool {
	return &Tool{
		ID:          r.ID,
		ServerName:  r.ServerName,
		GroupKind:   r.GroupKind,
		CreatedAt:   derefTime(r.CreatedAt),
		UpdatedAt:   derefTime(r.UpdatedAt),
		DeletedAt:   r.DeletedAt,
		Description: derefStr(r.Description),
	}
}

func toToolServer(r dbgen.Toolserver) *ToolServer {
	return &ToolServer{
		Name:          r.Name,
		GroupKind:     r.GroupKind,
		CreatedAt:     derefTime(r.CreatedAt),
		UpdatedAt:     derefTime(r.UpdatedAt),
		DeletedAt:     r.DeletedAt,
		Description:   derefStr(r.Description),
		LastConnected: r.LastConnected,
	}
}

func toAgentInstanceCheckpoint(row dbgen.AgentInstanceCheckpoint) (*apiv1alpha1.Checkpoint, error) {
	checkpoint := &apiv1alpha1.Checkpoint{}
	if err := proto.Unmarshal(row.Data, checkpoint); err != nil {
		return nil, fmt.Errorf("decode checkpoint %s: %w", row.ID, err)
	}
	if checkpoint.GetId() != row.ID.String() || checkpoint.GetAgentInstanceId() != row.SourceInstanceID.String() ||
		checkpoint.GetHeadTaskId() != row.HeadTaskID || checkpoint.GetHistorySequence() != uint64(row.HistorySequence) ||
		strings.TrimPrefix(checkpoint.GetState().String(), "CHECKPOINT_STATE_") != row.State {
		return nil, fmt.Errorf("checkpoint %s payload disagrees with indexed columns", row.ID)
	}
	return checkpoint, nil
}

func toMemory(r dbgen.Memory) *Memory {
	return &Memory{
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

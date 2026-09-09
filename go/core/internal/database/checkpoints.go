package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Client) ForkAgentInstance(ctx context.Context, checkpointID, userID, requestID, instanceID string) (*apiv1alpha1.AgentInstance, bool, error) {
	checkpointUUID := uuid.MustParse(checkpointID)

	existing, err := readAgentInstanceRequest(ctx, c.db, userID, requestID)
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
	var row agentInstanceRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		checkpoint, err := lockCheckpoint(ctx, tx, checkpointUUID, false, userID, new("READY"))
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

		revision, err := queryOne(ctx, tx, `
			SELECT revision, namespace, agent_template_name, agent_template_uid, harness_name, harness_uid,
			    source_snapshot, egress_destinations, actor_template_atespace, actor_template_name, actor_template_uid,
			    created_at, updated_at, agent_card FROM runtime_revision WHERE revision = $1
		`, pgx.RowToStructByName[runtimeRevisionRow], *checkpoint.PreparedRevision)
		if err != nil {
			return fmt.Errorf("get checkpoint runtime revision: %w", err)
		}
		labels := map[string]string{}
		if err := json.Unmarshal(checkpoint.SourceLabels, &labels); err != nil {
			return fmt.Errorf("decode checkpoint labels: %w", err)
		}
		sourceContext, err := queryOne(ctx, tx, `
			SELECT id, user_id, created_at, context_id FROM a2a_context WHERE id = $1
		`, pgx.RowToStructByName[a2aContextRow], checkpoint.SourceHistoryID)
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
		if err := execSQL(ctx, tx, `
			INSERT INTO a2a_context (id, user_id, context_id) VALUES ($1, $2, $3)
		`, historyID, userID, sourceContext.ContextID); err != nil {
			return fmt.Errorf("insert fork A2A context: %w", err)
		}
		row, err = queryOne(ctx, tx, `
			INSERT INTO agent_instance (id, user_id, request_id, context_id, history_id, prepared_revision,
			    source_checkpoint_id, state, operation, labels, data) VALUES ($1, $2, $3, $4, $5, $6, $9::uuid,
			    'CREATING', 'CREATE', $7, $8)
			ON CONFLICT (user_id, request_id) DO NOTHING
			RETURNING id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
			    source_checkpoint_id, history_id
		`,
			pgx.RowToStructByName[agentInstanceRow], instanceUUID, userID, requestID, sourceContext.ContextID,
			historyID, checkpoint.PreparedRevision, encodedLabels, data, &checkpoint.ID,
		)
		if err != nil {
			return err
		}

		events, err := queryMany(ctx, tx, `
			SELECT e.sequence, e.history_id, e.task_id, e.data, e.created_at, e.message_id, e.task_position,
			    e.initial_message_id, e.request_hash, e.snapshot_atespace, e.snapshot_uri, e.snapshot_content_scope
			FROM agent_instance_checkpoint c
			JOIN agent_instance_task_event e
			  ON e.history_id = c.source_history_id
			 AND e.sequence <= c.history_sequence
			WHERE c.id = $1
			ORDER BY e.sequence
		`, pgx.RowToStructByName[agentInstanceTaskEventRow], checkpoint.ID)
		if err != nil {
			return fmt.Errorf("list checkpoint events: %w", err)
		}
		if len(events) == 0 || events[len(events)-1].Sequence != checkpoint.HistorySequence {
			return fmt.Errorf("checkpoint history boundary is missing")
		}
		// The fork owns the retained Tag, not the source's replaceable snapshot.
		boundaryEvent := &events[len(events)-1]
		if boundaryEvent.TaskID == nil || *boundaryEvent.TaskID != checkpoint.HeadTaskID || boundaryEvent.SnapshotURI == nil {
			return fmt.Errorf("checkpoint runtime boundary is inconsistent")
		}
		boundaryEvent.SnapshotAtespace = &checkpoint.SnapshotAtespace
		boundaryEvent.SnapshotURI = &checkpoint.SnapshotURI
		boundaryEvent.SnapshotContentScope = &checkpoint.SnapshotContentScope
		tasks, err := replayTaskEvents(events, sourceContext.ContextID.String())
		if err != nil {
			return fmt.Errorf("replay checkpoint events: %w", err)
		}
		if !slices.ContainsFunc(tasks, func(task agentInstanceTaskRow) bool { return task.ID == checkpoint.HeadTaskID }) {
			return fmt.Errorf("checkpoint has no head task in its events")
		}
		var copiedHistorySequence int64
		sequences := make(map[int64]int64, len(events))
		for _, source := range events {
			copiedHistorySequence, err = insertTaskEvent(ctx, tx, taskEventWrite{
				HistoryID:            historyID,
				TaskID:               source.TaskID,
				MessageID:            source.MessageID,
				Data:                 source.Data,
				SnapshotAtespace:     source.SnapshotAtespace,
				SnapshotURI:          source.SnapshotURI,
				SnapshotContentScope: source.SnapshotContentScope,
				TaskPosition:         source.TaskPosition,
				InitialMessageID:     source.InitialMessageID,
				RequestHash:          source.RequestHash,
				CreatedAt:            &source.CreatedAt,
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
			if err := execSQL(ctx, tx, `
				INSERT INTO agent_instance_task (
				    history_id, id, state, status_timestamp, data, created_at, updated_at,
				    initial_message_id, request_hash, snapshot_atespace, snapshot_uri,
				    snapshot_content_scope, history_sequence, position
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			`,
				task.HistoryID, task.ID, task.State, task.StatusTimestamp, task.Data, task.CreatedAt, task.UpdatedAt,
				task.InitialMessageID, task.RequestHash, task.SnapshotAtespace, task.SnapshotURI,
				task.SnapshotContentScope, task.HistorySequence, task.Position,
			); err != nil {
				return fmt.Errorf("rebuild fork task %s: %w", task.ID, err)
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = readAgentInstanceRequest(ctx, c.db, userID, requestID)
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

// ReserveAgentInstanceCheckpoint returns the checkpoint and its immutable snapshot
// reference from the same transaction, including on idempotent retries.
func (c *Client) ReserveAgentInstanceCheckpoint(ctx context.Context, checkpoint *apiv1alpha1.Checkpoint, userID, requestID string) (*apiv1alpha1.Checkpoint, *AgentInstanceTaskSnapshot, error) {
	if checkpoint == nil {
		return nil, nil, fmt.Errorf("missing checkpoint")
	}
	var result *apiv1alpha1.Checkpoint
	var snapshot *AgentInstanceTaskSnapshot
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := queryOne(ctx, tx, `
			SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
			    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
			    source_labels, source_name FROM agent_instance_checkpoint
			WHERE user_id = $1 AND request_id = $2
		`, pgx.RowToStructByName[agentInstanceCheckpointRow], userID, requestID)
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

		instance, err := lockAgentInstance(ctx, tx, uuid.MustParse(checkpoint.GetAgentInstanceId()))
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
		boundary, err := queryOne(ctx, tx, `
			SELECT latest.history_id, latest.id, latest.state, latest.status_timestamp, latest.data, latest.created_at,
			    latest.updated_at, latest.initial_message_id, latest.request_hash, latest.snapshot_atespace,
			    latest.snapshot_uri, latest.snapshot_content_scope, latest.history_sequence, latest.position
			FROM (
			    SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id, request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM agent_instance_task
			    WHERE agent_instance_task.history_id = $1
			    ORDER BY history_sequence DESC NULLS LAST
			    LIMIT 1
			) latest
			WHERE latest.history_sequence = (SELECT MAX(sequence) FROM agent_instance_task_event WHERE history_id = $1)
			AND NOT EXISTS (
			    SELECT 1 FROM agent_instance_task active
			    WHERE active.history_id = $1
			      AND active.state NOT IN (
			          'TASK_STATE_COMPLETED',
			          'TASK_STATE_CANCELED',
			          'TASK_STATE_FAILED',
			          'TASK_STATE_REJECTED',
			          'TASK_STATE_INPUT_REQUIRED',
			          'TASK_STATE_AUTH_REQUIRED'
			      )
			)
		`, pgx.RowToStructByName[agentInstanceTaskRow], instance.HistoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentInstanceNotQuiescent
		}
		if err != nil {
			return fmt.Errorf("get latest AgentInstance task boundary: %w", err)
		}
		if boundary.SnapshotAtespace == nil || boundary.SnapshotURI == nil ||
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
		row, err := queryOne(ctx, tx, `
			INSERT INTO agent_instance_checkpoint (id, source_instance_id, user_id, request_id, head_task_id,
			    history_sequence, snapshot_atespace, snapshot_uri, snapshot_content_scope, source_history_id,
			    prepared_revision, source_labels, data, source_name, state) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
			    $10, $11, $12, $13, $14, 'CREATING')
			ON CONFLICT DO NOTHING
			RETURNING id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
			    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
			    source_labels, source_name
		`,
			pgx.RowToStructByName[agentInstanceCheckpointRow], uuid.MustParse(checkpoint.GetId()),
			uuid.MustParse(checkpoint.GetAgentInstanceId()), userID, requestID, boundary.ID, *boundary.HistorySequence,
			*boundary.SnapshotAtespace, *boundary.SnapshotURI, *boundary.SnapshotContentScope, instance.HistoryID,
			instance.PreparedRevision, instance.Labels, data, source.GetName(),
		)
		if errors.Is(err, pgx.ErrNoRows) {
			existing, existingErr := queryOne(ctx, tx, `
				SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
				    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
				    source_labels, source_name FROM agent_instance_checkpoint
				WHERE user_id = $1 AND request_id = $2
			`, pgx.RowToStructByName[agentInstanceCheckpointRow], userID, requestID)
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
			return fmt.Errorf("insert AgentInstance checkpoint: %w", err)
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
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, uuid.MustParse(id), true, "", nil)
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstanceCheckpoint(row)
		if err != nil {
			return err
		}
		if row.State != "CREATING" {
			if (row.State == "READY" && row.TagUID == tagUID && row.SnapshotURI == snapshotURI && failure == "") ||
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
		row, err = queryOne(ctx, tx, `
			UPDATE agent_instance_checkpoint
			SET state = CASE WHEN $2::text <> '' THEN 'READY' ELSE 'FAILED' END,
			    tag_uid = $2,
			    snapshot_uri = CASE WHEN $2::text <> '' THEN $3::text ELSE snapshot_uri END,
			    data = $4
			WHERE id = $1
			  AND state = 'CREATING'
			RETURNING id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
			    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
			    source_labels, source_name
		`, pgx.RowToStructByName[agentInstanceCheckpointRow], row.ID, tagUID, snapshotURI, data)
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
	row, err := queryOne(ctx, c.db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_labels, source_name FROM agent_instance_checkpoint
		WHERE id = $1 AND user_id = $2
		  -- Lifecycle work also reads creating and deleting checkpoints.
		  AND ($3::text IS NULL OR state = $3)
	`, pgx.RowToStructByName[agentInstanceCheckpointRow], uuid.MustParse(id), userID, new("READY"))
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance checkpoint: %w", notFoundOr(err))
	}
	return toAgentInstanceCheckpoint(row)
}

// GetAgentInstanceCheckpointSnapshot returns the private snapshot reference and
// tag UID for lifecycle workflows. Finalization replaces the source URI with the
// retained Tag copy; ready references are immutable.
func (c *Client) GetAgentInstanceCheckpointSnapshot(ctx context.Context, id, userID string) (*AgentInstanceTaskSnapshot, string, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_labels, source_name FROM agent_instance_checkpoint
		WHERE id = $1 AND user_id = $2
		  -- Lifecycle work also reads creating and deleting checkpoints.
		  AND ($3::text IS NULL OR state = $3)
	`, pgx.RowToStructByName[agentInstanceCheckpointRow], uuid.MustParse(id), userID, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get checkpoint snapshot: %w", notFoundOr(err))
	}
	if _, err := toAgentInstanceCheckpoint(row); err != nil {
		return nil, "", err
	}
	return checkpointSnapshot(row), row.TagUID, nil
}

func checkpointSnapshot(row agentInstanceCheckpointRow) *AgentInstanceTaskSnapshot {
	return &AgentInstanceTaskSnapshot{
		Atespace: row.SnapshotAtespace, URI: row.SnapshotURI, ContentScope: row.SnapshotContentScope,
	}
}

func (c *Client) ListAgentInstanceCheckpoints(ctx context.Context, instanceID, userID, afterID string, limit int) ([]*apiv1alpha1.Checkpoint, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_labels, source_name FROM agent_instance_checkpoint
		WHERE source_instance_id = $1
		  AND user_id = $2
		  AND state = 'READY'
		  AND (NULLIF($3::text, '') IS NULL OR id > NULLIF($3::text, '')::uuid)
		ORDER BY id
		LIMIT $4
	`,
		pgx.RowToStructByName[agentInstanceCheckpointRow], uuid.MustParse(instanceID), userID, afterID, int32(limit),
	)
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
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, uuid.MustParse(id), false, userID, nil)
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
		row, err = queryOne(ctx, tx, `
			UPDATE agent_instance_checkpoint
			SET state = 'DELETING', data = $3
			WHERE agent_instance_checkpoint.id = $1 AND agent_instance_checkpoint.user_id = $2
			  AND agent_instance_checkpoint.state IN ('READY', 'DELETING')
			  AND NOT EXISTS (
			      SELECT 1 FROM agent_instance i WHERE i.source_checkpoint_id = agent_instance_checkpoint.id
			  )
			RETURNING id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
			    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
			    source_labels, source_name
		`, pgx.RowToStructByName[agentInstanceCheckpointRow], row.ID, userID, data)
		if err != nil {
			return notFoundOr(err)
		}
		snapshot, tagUID = checkpointSnapshot(row), row.TagUID
		_, err = toAgentInstanceCheckpoint(row)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("begin delete AgentInstance checkpoint: %w", err)
	}
	return snapshot, tagUID, nil
}

func (c *Client) DeleteAgentInstanceCheckpoint(ctx context.Context, id, userID string) error {
	_, err := c.db.Exec(ctx, `
		DELETE FROM agent_instance_checkpoint
		WHERE id = $1 AND user_id = $2 AND state = 'DELETING'
	`, uuid.MustParse(id), userID)
	if err != nil {
		return fmt.Errorf("delete AgentInstance checkpoint: %w", err)
	}
	return nil
}

func toAgentInstanceCheckpoint(row agentInstanceCheckpointRow) (*apiv1alpha1.Checkpoint, error) {
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

type agentInstanceCheckpointRow struct {
	ID                   uuid.UUID
	SourceInstanceID     uuid.UUID
	UserID               string
	RequestID            string
	HeadTaskID           string
	HistorySequence      int64
	SnapshotAtespace     string
	SnapshotURI          string
	SnapshotContentScope string
	TagUID               string
	State                string
	Data                 []byte
	SourceHistoryID      uuid.UUID
	PreparedRevision     *string
	SourceLabels         []byte
	SourceName           string
}

func lockCheckpoint(ctx context.Context, db pgx.Tx, id uuid.UUID, allUsers bool, userID string, state *string) (agentInstanceCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_labels, source_name FROM agent_instance_checkpoint
		WHERE id = $1
		  -- Only internal finalization explicitly opts out of owner filtering.
		  AND ($2::boolean OR user_id = $3)
		  AND ($4::text IS NULL OR state = $4)
		FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceCheckpointRow], id, allUsers, userID, state)
}

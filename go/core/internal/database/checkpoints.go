package database

import (
	"context"
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

// ForkAgentInstance atomically creates an instance and independent history from an owned
// READY checkpoint, replaying only events through its saved boundary. The fork retains the
// source context ID, revision, and snapshot reference. A repeated owner/requestID returns
// the existing fork for the same checkpoint, or ErrIdempotencyConflict otherwise. The
// boolean reports creation; callers provision the runtime separately.
func (c *Client) ForkAgentInstance(ctx context.Context, checkpointID, userID, requestID, instanceID string) (*apiv1alpha1.AgentInstance, bool, error) {
	checkpointUUID, err := uuid.Parse(checkpointID)
	if err != nil {
		return nil, false, fmt.Errorf("invalid checkpoint ID: %w", err)
	}

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

	var row agentInstanceRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		checkpoint, err := lockCheckpoint(ctx, tx, checkpointID, false, userID, new("READY"))
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

		revision, err := getAvailableRuntimeRevisionForUpdate(ctx, tx, *checkpoint.PreparedRevision)
		if err != nil {
			return fmt.Errorf("get checkpoint runtime revision: %w", err)
		}
		sourceContextID, err := queryOne(ctx, tx, `
			SELECT context_id FROM a2a_context WHERE id = $1
		`, pgx.RowTo[uuid.UUID], checkpoint.SourceHistoryID)
		if err != nil {
			return fmt.Errorf("get checkpoint context: %w", err)
		}
		historyID := uuid.New()
		now := timestamppb.Now()
		instance := &apiv1alpha1.AgentInstance{
			Id: instanceID, Creator: userID, ContextId: sourceContextID.String(),
			Name:             checkpoint.SourceName,
			Harness:          &apiv1alpha1.ResourceReference{Namespace: revision.Namespace, Name: revision.HarnessName},
			AgentTemplate:    &apiv1alpha1.ResourceReference{Namespace: revision.Namespace, Name: revision.AgentTemplateName},
			PreparedRevision: *checkpoint.PreparedRevision,
			State:            apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING,
			Operation:        apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE,
			CreatedAt:        now, UpdatedAt: now,
		}
		row, err = insertAgentInstanceRecords(ctx, tx, instance, requestID, historyID, &checkpoint.ID)
		if err != nil {
			return err
		}

		events, err := readCheckpointEvents(ctx, tx, checkpoint.ID)
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
		tasks, err := replayTaskEvents(events, sourceContextID.String())
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
			if err := insertReplayedTask(ctx, tx, task); err != nil {
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

// ReserveAgentInstanceCheckpoint reserves an owned instance's latest quiescent task
// boundary and returns its snapshot reference atomically. The instance must be READY with
// no lifecycle operation and a usable snapshot boundary. A repeated owner/requestID
// returns the same reservation for the same source, or ErrIdempotencyConflict otherwise. A
// CREATING reservation blocks new task writes and lifecycle operations while the caller
// retains the external snapshot.
func (c *Client) ReserveAgentInstanceCheckpoint(ctx context.Context, checkpoint *apiv1alpha1.Checkpoint, userID, requestID string) (*apiv1alpha1.Checkpoint, *AgentInstanceTaskSnapshot, error) {
	if checkpoint == nil {
		return nil, nil, fmt.Errorf("missing checkpoint")
	}
	sourceID, err := uuid.Parse(checkpoint.GetAgentInstanceId())
	if err != nil {
		return nil, nil, fmt.Errorf("invalid source instance ID: %w", err)
	}
	var result *apiv1alpha1.Checkpoint
	var snapshot *AgentInstanceTaskSnapshot
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := readCheckpointRequest(ctx, tx, userID, requestID)
		if err == nil {
			if existing.SourceInstanceID != sourceID {
				return ErrIdempotencyConflict
			}
			snapshot = checkpointSnapshot(existing)
			result, err = toAgentInstanceCheckpoint(existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get AgentInstance checkpoint by request: %w", err)
		}

		instance, err := lockAgentInstance(ctx, tx, checkpoint.GetAgentInstanceId())
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (instance.UserID != userID)) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", checkpoint.GetAgentInstanceId(), err)
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("AgentInstance %s cannot checkpoint in state %s with operation %s: %w", checkpoint.GetAgentInstanceId(), instance.State, instance.Operation, ErrConflict)
		}
		source, err := toAgentInstance(instance)
		if err != nil {
			return err
		}
		if instance.PreparedRevision != nil {
			if _, err := getAvailableRuntimeRevisionForUpdate(ctx, tx, *instance.PreparedRevision); err != nil {
				return err
			}
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
			AND latest.state IN (
			    'TASK_STATE_COMPLETED',
			    'TASK_STATE_CANCELED',
			    'TASK_STATE_FAILED',
			    'TASK_STATE_REJECTED'
			)
			AND NOT EXISTS (
			    SELECT 1 FROM agent_instance_task active
			    WHERE active.history_id = $1
			      AND active.state NOT IN (
			          'TASK_STATE_COMPLETED',
			          'TASK_STATE_CANCELED',
			          'TASK_STATE_FAILED',
			          'TASK_STATE_REJECTED'
			      )
			)
		`, pgx.RowToStructByName[agentInstanceTaskRow], instance.HistoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("AgentInstance %s has no quiescent turn boundary: %w", checkpoint.GetAgentInstanceId(), ErrFailedPrecondition)
		}
		if err != nil {
			return fmt.Errorf("get latest AgentInstance task boundary: %w", err)
		}
		if boundary.SnapshotAtespace == nil || boundary.SnapshotURI == nil ||
			boundary.SnapshotContentScope == nil || boundary.HistorySequence == nil {
			return fmt.Errorf("AgentInstance %s has no quiescent turn boundary: %w", checkpoint.GetAgentInstanceId(), ErrFailedPrecondition)
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
			    prepared_revision, data, source_name, state) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
			    $10, $11, $12, $13, 'CREATING')
			ON CONFLICT DO NOTHING
			RETURNING id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
			    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
			    source_name
		`,
			pgx.RowToStructByName[agentInstanceCheckpointRow], checkpoint.GetId(),
			checkpoint.GetAgentInstanceId(), userID, requestID, boundary.ID, *boundary.HistorySequence,
			*boundary.SnapshotAtespace, *boundary.SnapshotURI, *boundary.SnapshotContentScope, instance.HistoryID,
			instance.PreparedRevision, data, source.GetName(),
		)
		if errors.Is(err, pgx.ErrNoRows) {
			existing, existingErr := readCheckpointRequest(ctx, tx, userID, requestID)
			if existingErr == nil {
				if existing.SourceInstanceID != sourceID {
					return ErrIdempotencyConflict
				}
				snapshot = checkpointSnapshot(existing)
				result, existingErr = toAgentInstanceCheckpoint(existing)
				return existingErr
			}
			if errors.Is(existingErr, pgx.ErrNoRows) {
				return fmt.Errorf("AgentInstance %s has a checkpoint being created: %w", checkpoint.GetAgentInstanceId(), ErrConflict)
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

// FinalizeAgentInstanceCheckpoint records either a retained tag UID and snapshot URI as
// READY, or a failure as FAILED. An identical terminal retry returns the existing
// checkpoint; a different terminal result returns ErrNotFound. This internal operation
// does not check ownership or create the external snapshot.
func (c *Client) FinalizeAgentInstanceCheckpoint(ctx context.Context, id, tagUID, snapshotURI, failure string) (*apiv1alpha1.Checkpoint, error) {
	if (tagUID == "") == (failure == "") || (tagUID == "") != (snapshotURI == "") {
		return nil, fmt.Errorf("finalize AgentInstance checkpoint requires tag UID and snapshot URI, or failure")
	}
	var result *apiv1alpha1.Checkpoint
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, id, true, "", nil)
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
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance_checkpoint
			SET state = CASE WHEN $2::text <> '' THEN 'READY' ELSE 'FAILED' END,
			    tag_uid = $2,
			    snapshot_uri = CASE WHEN $2::text <> '' THEN $3::text ELSE snapshot_uri END,
			    data = $4
			WHERE id = $1
			  AND state = 'CREATING'
		`, row.ID, tagUID, snapshotURI, data)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("finalize AgentInstance checkpoint: %w", err)
	}
	return result, nil
}

// GetAgentInstanceCheckpoint returns an owned READY checkpoint. Missing checkpoints, other
// owners, and other lifecycle states return ErrNotFound.
func (c *Client) GetAgentInstanceCheckpoint(ctx context.Context, id, userID string) (*apiv1alpha1.Checkpoint, error) {
	row, err := readCheckpoint(ctx, c.db, id, userID, new("READY"))
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance checkpoint: %w", notFoundOr(err))
	}
	return toAgentInstanceCheckpoint(row)
}

// GetAgentInstanceCheckpointSnapshot returns an owned checkpoint's snapshot reference and
// tag UID in any lifecycle state. Before finalization the URI refers to the source
// snapshot; afterward it refers to the retained copy. Missing checkpoints and other owners
// return ErrNotFound.
func (c *Client) GetAgentInstanceCheckpointSnapshot(ctx context.Context, id, userID string) (*AgentInstanceTaskSnapshot, string, error) {
	row, err := readCheckpoint(ctx, c.db, id, userID, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get checkpoint snapshot: %w", notFoundOr(err))
	}
	if _, err := toAgentInstanceCheckpoint(row); err != nil {
		return nil, "", err
	}
	return checkpointSnapshot(row), row.TagUID, nil
}

// checkpointSnapshot extracts the external snapshot reference without reading or
// validating the external snapshot.
func checkpointSnapshot(row agentInstanceCheckpointRow) *AgentInstanceTaskSnapshot {
	return &AgentInstanceTaskSnapshot{
		Atespace: row.SnapshotAtespace, URI: row.SnapshotURI, ContentScope: row.SnapshotContentScope,
	}
}

// CheckpointQuery narrows and orders a caller's checkpoints. An empty InstanceID spans
// every conversation they own; an empty Filter matches everything; an empty SortField
// is newest first.
type CheckpointQuery struct {
	InstanceID string
	UserID     string
	Filter     string
	SortField  string
	Descending bool
	Offset     int
	Limit      int
}

// ErrCheckpointQuery reports a listing that cannot be run, as opposed to one that failed.
var ErrCheckpointQuery = errors.New("invalid checkpoint query")

// The sortable columns, which are the indexed ones: a checkpoint's timestamp and a
// conversation's current name both live inside encoded payloads. "created_at" orders by
// id, which is UUIDv7 and so already chronological. Not state — a listing is READY
// checkpoints only, so it would order every row by the same value.
var checkpointSortFields = map[string]bool{"created_at": true, "conversation": true}

// The page, and the count the filter matched, in one read.
type checkpointPageRow struct {
	agentInstanceCheckpointRow
	TotalSize int
}

// ListAgentInstanceCheckpoints reads one page of a caller's READY checkpoints and the
// count the filter matched. Retained checkpoints stay listable after their conversation
// is deleted. A total of zero over an empty page means the offset is past the end.
//
// The ordering is passed as parameters rather than concatenated in, so the statement
// stays preparable by TestInlineSQLPrepares and there is no ORDER BY to inject into.
func (c *Client) ListAgentInstanceCheckpoints(ctx context.Context, query CheckpointQuery) ([]*apiv1alpha1.Checkpoint, int, error) {
	if query.SortField != "" && !checkpointSortFields[query.SortField] {
		return nil, 0, fmt.Errorf("%w: unsortable column %q", ErrCheckpointQuery, query.SortField)
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence,
		    snapshot_atespace, snapshot_uri, snapshot_content_scope, tag_uid, state, data,
		    source_history_id, prepared_revision, source_name,
		    (count(*) OVER ())::int AS total_size
		FROM agent_instance_checkpoint
		WHERE user_id = $1 AND state = 'READY'
		  AND (NULLIF($2::text, '') IS NULL OR source_instance_id = NULLIF($2::text, '')::uuid)
		  AND ($3::text = '' OR position(lower($3) IN lower(source_name)) > 0
		                     OR position(lower($3) IN id::text) > 0)
		ORDER BY
		    (CASE WHEN $6::text = 'conversation' AND NOT $7::bool THEN lower(source_name) END) ASC,
		    (CASE WHEN $6::text = 'conversation' AND $7::bool THEN lower(source_name) END) DESC,
		    (CASE WHEN $6::text = 'created_at' AND NOT $7::bool THEN id END) ASC,
		    (CASE WHEN $6::text = 'created_at' AND $7::bool THEN id END) DESC,
		    -- Newest first when nothing was asked for, and the tiebreak when it was.
		    id DESC
		LIMIT $4 OFFSET $5
	`, pgx.RowToStructByName[checkpointPageRow],
		query.UserID, query.InstanceID, query.Filter,
		int32(query.Limit), int32(query.Offset), query.SortField, query.Descending)
	if err != nil {
		return nil, 0, fmt.Errorf("list AgentInstance checkpoints: %w", err)
	}

	result := make([]*apiv1alpha1.Checkpoint, len(rows))
	for i := range rows {
		checkpoint, err := toAgentInstanceCheckpoint(rows[i].agentInstanceCheckpointRow)
		if err != nil {
			return nil, 0, err
		}
		// Set after decoding: the stored payload predates the field.
		checkpoint.ConversationName = rows[i].SourceName
		result[i] = checkpoint
	}
	if len(rows) == 0 {
		return result, 0, nil
	}
	return result, rows[0].TotalSize, nil
}

// BeginDeleteAgentInstanceCheckpoint marks an owned READY checkpoint DELETING and returns
// the snapshot and tag identity for external cleanup. Retrying a DELETING checkpoint is
// allowed. A fork reference, another lifecycle state, or a missing/unowned checkpoint
// returns ErrNotFound.
func (c *Client) BeginDeleteAgentInstanceCheckpoint(ctx context.Context, id, userID string) (*AgentInstanceTaskSnapshot, string, error) {
	var snapshot *AgentInstanceTaskSnapshot
	var tagUID string
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, id, false, userID, nil)
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
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance_checkpoint
			SET state = 'DELETING', data = $3
			WHERE agent_instance_checkpoint.id = $1 AND agent_instance_checkpoint.user_id = $2
			  AND agent_instance_checkpoint.state IN ('READY', 'DELETING')
			  AND NOT EXISTS (
			      SELECT 1 FROM agent_instance i WHERE i.source_checkpoint_id = agent_instance_checkpoint.id
			  )
		`, row.ID, userID, data)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		snapshot, tagUID = checkpointSnapshot(row), row.TagUID
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("begin delete AgentInstance checkpoint: %w", err)
	}
	return snapshot, tagUID, nil
}

// DeleteAgentInstanceCheckpoint removes an owned DELETING checkpoint after the caller
// cleans up its external tag. No matching row is a successful no-op.
func (c *Client) DeleteAgentInstanceCheckpoint(ctx context.Context, id, userID string) error {
	_, err := c.db.Exec(ctx, `
		DELETE FROM agent_instance_checkpoint
		WHERE id = $1 AND user_id = $2 AND state = 'DELETING'
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete AgentInstance checkpoint: %w", err)
	}
	return nil
}

// toAgentInstanceCheckpoint decodes a checkpoint and rejects disagreement between its
// payload and indexed identity, history boundary, or lifecycle state.
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
	SourceName           string
}

// lockCheckpoint locks a checkpoint until the caller's transaction ends, optionally
// filtering by state. Ownership is required unless internal finalization explicitly sets
// allUsers; no match returns pgx.ErrNoRows.
func lockCheckpoint(ctx context.Context, db pgx.Tx, id string, allUsers bool, userID string, state *string) (agentInstanceCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_name FROM agent_instance_checkpoint
		WHERE id = $1
		  -- Only internal finalization explicitly opts out of owner filtering.
		  AND ($2::boolean OR user_id = $3)
		  AND ($4::text IS NULL OR state = $4)
		FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceCheckpointRow], id, allUsers, userID, state)
}

// readCheckpointEvents returns the immutable events through a checkpoint's saved history
// boundary in sequence order. Callers authorize access and hold the checkpoint lock when
// forking.
func readCheckpointEvents(ctx context.Context, db dbExecutor, checkpointID uuid.UUID) ([]agentInstanceTaskEventRow, error) {
	return queryMany(ctx, db, `
		SELECT e.sequence, e.history_id, e.task_id, e.data, e.created_at, e.message_id, e.task_position,
		    e.initial_message_id, e.request_hash, e.snapshot_atespace, e.snapshot_uri, e.snapshot_content_scope
		FROM agent_instance_checkpoint c
		JOIN agent_instance_task_event e
		  ON e.history_id = c.source_history_id
		 AND e.sequence <= c.history_sequence
		WHERE c.id = $1
		ORDER BY e.sequence
	`, pgx.RowToStructByName[agentInstanceTaskEventRow], checkpointID)
}

// insertReplayedTask restores a task projection with its original ordering, timestamps,
// retry metadata, and snapshot boundary. Callers provide the destination history and
// transaction after validating the replay.
func insertReplayedTask(ctx context.Context, db dbExecutor, task agentInstanceTaskRow) error {
	return execSQL(ctx, db, `
		INSERT INTO agent_instance_task (
		    history_id, id, state, status_timestamp, data, created_at, updated_at,
		    initial_message_id, request_hash, snapshot_atespace, snapshot_uri,
		    snapshot_content_scope, history_sequence, position
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`,
		task.HistoryID, task.ID, task.State, task.StatusTimestamp, task.Data, task.CreatedAt, task.UpdatedAt,
		task.InitialMessageID, task.RequestHash, task.SnapshotAtespace, task.SnapshotURI,
		task.SnapshotContentScope, task.HistorySequence, task.Position,
	)
}

// readCheckpoint reads an owned checkpoint, optionally restricted to one lifecycle state.
// Missing or unowned checkpoints return pgx.ErrNoRows; the read does not lock the row.
func readCheckpoint(ctx context.Context, db dbExecutor, id, userID string, state *string) (agentInstanceCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_name FROM agent_instance_checkpoint
		WHERE id = $1 AND user_id = $2
		  -- Lifecycle work also reads creating and deleting checkpoints.
		  AND ($3::text IS NULL OR state = $3)
	`, pgx.RowToStructByName[agentInstanceCheckpointRow], id, userID, state)
}

// readCheckpointRequest returns a checkpoint reserved by the owner/requestID pair, in
// any lifecycle state, or pgx.ErrNoRows. Callers check its source before accepting a retry.
func readCheckpointRequest(ctx context.Context, db dbExecutor, userID, requestID string) (agentInstanceCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_instance_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision,
		    source_name FROM agent_instance_checkpoint
		WHERE user_id = $1 AND request_id = $2
	`, pgx.RowToStructByName[agentInstanceCheckpointRow], userID, requestID)
}

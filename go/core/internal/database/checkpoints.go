package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrCheckpointAdvanced requires the caller to select a new terminal task.
	ErrCheckpointAdvanced = fmt.Errorf("conversation advanced beyond the expected task: %w", ErrFailedPrecondition)
	// ErrSnapshotPending permits retrying the same checkpoint request and task.
	ErrSnapshotPending = fmt.Errorf("snapshot for the expected task is not ready: %w", ErrFailedPrecondition)
)

// ForkSession atomically creates a session and independent history from an owned
// READY checkpoint, replaying only events through its saved boundary. The fork retains the
// source revision and snapshot, with fresh context and task IDs. A repeated owner/requestID returns
// the existing fork for the same checkpoint, or ErrIdempotencyConflict otherwise. The
// boolean reports creation; callers provision the runtime separately. A deleted
// fork returns ErrFailedPrecondition and retains its request identity.
func (c *Client) ForkSession(ctx context.Context, checkpointID, userID, requestID, sessionID string) (*apiv1alpha1.Session, bool, error) {
	checkpointUUID, err := uuid.Parse(checkpointID)
	if err != nil {
		return nil, false, fmt.Errorf("invalid checkpoint ID: %w", err)
	}

	existing, err := readSessionRequest(ctx, c.db, userID, requestID)
	if err == nil {
		if existing.SourceCheckpointID == nil || *existing.SourceCheckpointID != checkpointUUID {
			return nil, false, ErrIdempotencyConflict
		}
		session, err := toSession(existing)
		if err == nil && session.State == apiv1alpha1.SessionState_SESSION_STATE_DELETED {
			return nil, false, ErrFailedPrecondition
		}
		return session, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get fork request: %w", err)
	}

	var row sessionRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		checkpoint, err := lockCheckpoint(ctx, tx, checkpointID, false, userID, new("READY"))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock checkpoint: %w", err)
		}
		source, err := toSessionCheckpoint(checkpoint)
		if err != nil {
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
		session := &apiv1alpha1.Session{
			Id: sessionID, Creator: userID, ContextId: sessionID,
			Name:             source.GetName(),
			Agent:            &apiv1alpha1.ResourceReference{Namespace: revision.Namespace, Name: revision.AgentName},
			PreparedRevision: *checkpoint.PreparedRevision,
			State:            apiv1alpha1.SessionState_SESSION_STATE_CREATING,
			Operation:        apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE,
			CreatedAt:        now, UpdatedAt: now,
		}
		row, err = insertSessionRecords(ctx, tx, session, requestID, historyID, &checkpoint.ID)
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
		if boundaryEvent.TaskID != checkpoint.HeadTaskID || boundaryEvent.SnapshotURI == nil {
			return fmt.Errorf("checkpoint runtime boundary is inconsistent")
		}
		boundaryEvent.SnapshotAtespace = &checkpoint.SnapshotAtespace
		boundaryEvent.SnapshotURI = &checkpoint.SnapshotURI
		boundaryEvent.SnapshotContentScope = &checkpoint.SnapshotContentScope
		tasks, err := replayTaskEvents(events, sourceContextID.String())
		if err != nil {
			return fmt.Errorf("replay checkpoint events: %w", err)
		}
		if !slices.ContainsFunc(tasks, func(task sessionTaskRow) bool { return task.ID == checkpoint.HeadTaskID }) {
			return fmt.Errorf("checkpoint has no head task in its events")
		}
		events, err = forkTaskEvents(events, sessionID)
		if err != nil {
			return fmt.Errorf("assign fork identities: %w", err)
		}
		tasks, err = replayTaskEvents(events, sessionID)
		if err != nil {
			return fmt.Errorf("replay fork events: %w", err)
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
		existing, err = readSessionRequest(ctx, c.db, userID, requestID)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent fork request: %w", err)
		}
		if existing.SourceCheckpointID == nil || *existing.SourceCheckpointID != checkpointUUID {
			return nil, false, ErrIdempotencyConflict
		}
		session, err := toSession(existing)
		if err == nil && session.State == apiv1alpha1.SessionState_SESSION_STATE_DELETED {
			return nil, false, ErrFailedPrecondition
		}
		return session, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("fork Session: %w", err)
	}
	session, err := toSession(row)
	return session, err == nil, err
}

// ReserveSessionCheckpoint captures the terminal task named by HeadTaskId,
// provided it is still the latest boundary. ErrSnapshotPending permits retry;
// ErrCheckpointAdvanced requires a new selection. The session must be READY.
// A repeated owner/requestID returns the same reservation for the same source
// and task, or ErrIdempotencyConflict otherwise. A
// CREATING reservation blocks new task writes and lifecycle operations while the caller
// retains the external snapshot.
func (c *Client) ReserveSessionCheckpoint(ctx context.Context, checkpoint *apiv1alpha1.Checkpoint, userID, requestID string) (*apiv1alpha1.Checkpoint, *SessionTaskSnapshot, error) {
	if checkpoint == nil {
		return nil, nil, fmt.Errorf("missing checkpoint")
	}
	sourceID, err := uuid.Parse(checkpoint.GetSessionId())
	if err != nil {
		return nil, nil, fmt.Errorf("invalid source session ID: %w", err)
	}
	var result *apiv1alpha1.Checkpoint
	var snapshot *SessionTaskSnapshot
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		existing, err := readCheckpointRequest(ctx, tx, userID, requestID)
		if err == nil {
			if existing.SourceSessionID != sourceID || existing.HeadTaskID != checkpoint.HeadTaskId {
				return ErrIdempotencyConflict
			}
			snapshot = checkpointSnapshot(existing)
			result, err = toSessionCheckpoint(existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get Session checkpoint by request: %w", err)
		}

		session, err := lockSession(ctx, tx, checkpoint.GetSessionId())
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (session.UserID != userID || session.State == "SESSION_STATE_DELETED")) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock Session %s: %w", checkpoint.GetSessionId(), err)
		}
		// Decoding rejects a corrupt stored session before it becomes a checkpoint source.
		if _, err := toSession(session); err != nil {
			return err
		}
		if session.State != "SESSION_STATE_READY" || session.Operation != "SESSION_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("session %s cannot checkpoint in state %s with operation %s: %w", checkpoint.GetSessionId(), session.State, session.Operation, ErrConflict)
		}
		type head struct {
			TaskID   string
			State    string
			Sequence int64
		}
		current, err := queryOne(ctx, tx, `
			SELECT e.task_id, t.state, e.sequence FROM session_task_event e
			JOIN session_task t ON t.history_id = e.history_id AND t.id = e.task_id
			WHERE e.history_id = $1 ORDER BY e.sequence DESC LIMIT 1
		`, pgx.RowToStructByName[head], session.HistoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrFailedPrecondition
		}
		if err != nil {
			return err
		}
		if current.TaskID != checkpoint.HeadTaskId {
			return ErrCheckpointAdvanced
		}
		if !a2a.TaskState(current.State).Terminal() {
			return fmt.Errorf("checkpoint requires a terminal task: %w", ErrFailedPrecondition)
		}
		pending, err := queryOne(ctx, tx, `
            SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1
                AND state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED', 'TASK_STATE_CANCELED', 'TASK_STATE_REJECTED'))
        `, pgx.RowTo[bool], session.HistoryID)
		if err != nil {
			return err
		}
		if pending {
			return fmt.Errorf("checkpoint requires all tasks to be terminal: %w", ErrFailedPrecondition)
		}
		if err := requireSettledRuntime(ctx, tx, session.HistoryID, ""); err != nil {
			if errors.Is(err, ErrFailedPrecondition) {
				return ErrSnapshotPending
			}
			return err
		}
		if session.PreparedRevision != nil {
			if _, err := getAvailableRuntimeRevisionForUpdate(ctx, tx, *session.PreparedRevision); err != nil {
				return err
			}
		}
		boundary, err := readSessionTask(ctx, tx, session.HistoryID, current.TaskID)
		if err != nil {
			return fmt.Errorf("get latest Session task boundary: %w", err)
		}
		if boundary.SnapshotAtespace == nil || boundary.SnapshotURI == nil ||
			boundary.SnapshotContentScope == nil || boundary.HistorySequence == nil || *boundary.HistorySequence != current.Sequence {
			return ErrSnapshotPending
		}

		value := proto.Clone(checkpoint).(*apiv1alpha1.Checkpoint)
		value.HeadTaskId = boundary.ID
		value.HistorySequence = uint64(*boundary.HistorySequence)
		value.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_CREATING
		value.CreatedAt = timestamppb.Now()
		value.Failure = nil
		value.Name = defaultCheckpointName(sourceID, boundary.ID)
		data, err := proto.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode checkpoint: %w", err)
		}
		row, err := queryOne(ctx, tx, `
			INSERT INTO session_checkpoint (id, source_session_id, user_id, request_id, head_task_id,
			    history_sequence, snapshot_atespace, snapshot_uri, snapshot_content_scope, source_history_id,
			    prepared_revision, data, state) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
			    $10, $11, $12, 'CREATING')
			ON CONFLICT DO NOTHING
			RETURNING id, source_session_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
			    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision
		`,
			pgx.RowToStructByName[sessionCheckpointRow], checkpoint.GetId(),
			checkpoint.GetSessionId(), userID, requestID, boundary.ID, *boundary.HistorySequence,
			*boundary.SnapshotAtespace, *boundary.SnapshotURI, *boundary.SnapshotContentScope, session.HistoryID,
			session.PreparedRevision, data,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			existing, existingErr := readCheckpointRequest(ctx, tx, userID, requestID)
			if existingErr == nil {
				if existing.SourceSessionID != sourceID {
					return ErrIdempotencyConflict
				}
				snapshot = checkpointSnapshot(existing)
				result, existingErr = toSessionCheckpoint(existing)
				return existingErr
			}
			if errors.Is(existingErr, pgx.ErrNoRows) {
				return fmt.Errorf("session %s has a checkpoint being created: %w", checkpoint.GetSessionId(), ErrConflict)
			}
			return fmt.Errorf("get conflicting Session checkpoint request: %w", existingErr)
		}
		if err != nil {
			return fmt.Errorf("insert Session checkpoint: %w", err)
		}
		snapshot = checkpointSnapshot(row)
		result, err = toSessionCheckpoint(row)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("reserve Session checkpoint: %w", err)
	}
	return result, snapshot, nil
}

// FinalizeSessionCheckpoint records either a retained tag UID and snapshot URI as
// READY, or a failure as FAILED. An identical terminal retry returns the existing
// checkpoint; a different terminal result returns ErrNotFound. This internal operation
// does not check ownership or create the external snapshot.
func (c *Client) FinalizeSessionCheckpoint(ctx context.Context, id, tagUID, snapshotURI, failure string) (*apiv1alpha1.Checkpoint, error) {
	if (tagUID == "") == (failure == "") || (tagUID == "") != (snapshotURI == "") {
		return nil, fmt.Errorf("finalize Session checkpoint requires tag UID and snapshot URI, or failure")
	}
	var result *apiv1alpha1.Checkpoint
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, id, true, "", nil)
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toSessionCheckpoint(row)
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
			UPDATE session_checkpoint
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
		return nil, fmt.Errorf("finalize Session checkpoint: %w", err)
	}
	return result, nil
}

// GetSessionCheckpoint returns an owned READY checkpoint. Missing checkpoints, other
// owners, and other lifecycle states return ErrNotFound.
func (c *Client) GetSessionCheckpoint(ctx context.Context, id, userID string) (*apiv1alpha1.Checkpoint, error) {
	row, err := readCheckpoint(ctx, c.db, id, userID, new("READY"))
	if err != nil {
		return nil, fmt.Errorf("get Session checkpoint: %w", notFoundOr(err))
	}
	return toSessionCheckpoint(row)
}

// GetSessionCheckpointSnapshot returns an owned checkpoint's snapshot reference and
// tag UID in any lifecycle state. Before finalization the URI refers to the source
// snapshot; afterward it refers to the retained copy. Missing checkpoints and other owners
// return ErrNotFound.
func (c *Client) GetSessionCheckpointSnapshot(ctx context.Context, id, userID string) (*SessionTaskSnapshot, string, error) {
	row, err := readCheckpoint(ctx, c.db, id, userID, nil)
	if err != nil {
		return nil, "", fmt.Errorf("get checkpoint snapshot: %w", notFoundOr(err))
	}
	if _, err := toSessionCheckpoint(row); err != nil {
		return nil, "", err
	}
	return checkpointSnapshot(row), row.TagUID, nil
}

// checkpointSnapshot extracts the external snapshot reference without reading or
// validating the external snapshot.
func checkpointSnapshot(row sessionCheckpointRow) *SessionTaskSnapshot {
	return &SessionTaskSnapshot{
		Atespace: row.SnapshotAtespace, URI: row.SnapshotURI, ContentScope: row.SnapshotContentScope,
	}
}

// ListSessionCheckpoints returns an owner's READY checkpoints for the source
// session in ascending ID order after afterID, up to limit. Retained checkpoints remain
// listable after the source session is deleted.
func (c *Client) ListSessionCheckpoints(ctx context.Context, sessionID, userID, afterID string, limit int) ([]*apiv1alpha1.Checkpoint, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, source_session_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision
		FROM session_checkpoint
		WHERE source_session_id = $1
		  AND user_id = $2
		  AND state = 'READY'
		  AND (NULLIF($3::text, '') IS NULL OR id > NULLIF($3::text, '')::uuid)
		ORDER BY id
		LIMIT $4
	`,
		pgx.RowToStructByName[sessionCheckpointRow], sessionID, userID, afterID, int32(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list Session checkpoints: %w", err)
	}
	result := make([]*apiv1alpha1.Checkpoint, len(rows))
	for i := range rows {
		checkpoint, err := toSessionCheckpoint(rows[i])
		if err != nil {
			return nil, err
		}
		result[i] = checkpoint
	}
	return result, nil
}

// BeginDeleteSessionCheckpoint marks an owned READY checkpoint DELETING and returns
// the snapshot and tag identity for external cleanup. Retrying a DELETING checkpoint is
// allowed. A fork reference, another lifecycle state, or a missing/unowned checkpoint
// returns ErrNotFound.
func (c *Client) BeginDeleteSessionCheckpoint(ctx context.Context, id, userID string) (*SessionTaskSnapshot, string, error) {
	var snapshot *SessionTaskSnapshot
	var tagUID string
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, id, false, userID, nil)
		if err != nil {
			return notFoundOr(err)
		}
		checkpoint, err := toSessionCheckpoint(row)
		if err != nil {
			return err
		}
		checkpoint.State = apiv1alpha1.CheckpointState_CHECKPOINT_STATE_DELETING
		data, err := proto.Marshal(checkpoint)
		if err != nil {
			return fmt.Errorf("encode checkpoint: %w", err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session_checkpoint
			SET state = 'DELETING', data = $3
			WHERE session_checkpoint.id = $1 AND session_checkpoint.user_id = $2
			  AND session_checkpoint.state IN ('READY', 'DELETING')
			  AND NOT EXISTS (
			      SELECT 1 FROM session i WHERE i.pinned_checkpoint_id = session_checkpoint.id
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
		return nil, "", fmt.Errorf("begin delete Session checkpoint: %w", err)
	}
	return snapshot, tagUID, nil
}

// DeleteSessionCheckpoint removes an owned DELETING checkpoint after the caller
// cleans up its external tag. No matching row is a successful no-op.
func (c *Client) DeleteSessionCheckpoint(ctx context.Context, id, userID string) error {
	_, err := c.db.Exec(ctx, `
		DELETE FROM session_checkpoint
		WHERE id = $1 AND user_id = $2 AND state = 'DELETING'
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete Session checkpoint: %w", err)
	}
	return nil
}

// defaultCheckpointName names a boundary by the conversation it was taken from and the
// turn it sits at, the two things that tell two boundaries apart.
func defaultCheckpointName(sessionID uuid.UUID, headTaskID string) string {
	return fmt.Sprintf("%s-%s", sessionID, headTaskID)
}

// UpdateCheckpointName renames an owned, ready checkpoint, and with it the name its forks
// are given. An empty name restores the generated default rather than leaving the
// checkpoint nameless. Missing, unowned and not-yet-ready checkpoints return ErrNotFound:
// a checkpoint Get and List will not show is not one a caller may rename, or read back
// through the rename's response.
func (c *Client) UpdateCheckpointName(ctx context.Context, id, userID, name string) (*apiv1alpha1.Checkpoint, error) {
	var result *apiv1alpha1.Checkpoint
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		ready := "READY"
		row, err := lockCheckpoint(ctx, tx, id, false, userID, &ready)
		if err != nil {
			return notFoundOr(err)
		}
		checkpoint, err := toSessionCheckpoint(row)
		if err != nil {
			return err
		}
		if name == "" {
			name = defaultCheckpointName(row.SourceSessionID, row.HeadTaskID)
		}
		checkpoint.Name = name
		data, err := proto.Marshal(checkpoint)
		if err != nil {
			return fmt.Errorf("encode checkpoint: %w", err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session_checkpoint
			SET data = $1
			WHERE id = $2 AND user_id = $3
		`, data, row.ID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		result = checkpoint
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rename checkpoint %s: %w", id, err)
	}
	return result, nil
}

// toSessionCheckpoint decodes a checkpoint and rejects disagreement between its
// payload and indexed identity, history boundary, or lifecycle state.
func toSessionCheckpoint(row sessionCheckpointRow) (*apiv1alpha1.Checkpoint, error) {
	checkpoint := &apiv1alpha1.Checkpoint{}
	if err := proto.Unmarshal(row.Data, checkpoint); err != nil {
		return nil, fmt.Errorf("decode checkpoint %s: %w", row.ID, err)
	}
	if checkpoint.GetId() != row.ID.String() || checkpoint.GetSessionId() != row.SourceSessionID.String() ||
		checkpoint.GetHeadTaskId() != row.HeadTaskID || checkpoint.GetHistorySequence() != uint64(row.HistorySequence) ||
		strings.TrimPrefix(checkpoint.GetState().String(), "CHECKPOINT_STATE_") != row.State {
		return nil, fmt.Errorf("checkpoint %s payload disagrees with indexed columns", row.ID)
	}
	return checkpoint, nil
}

type sessionCheckpointRow struct {
	ID                   uuid.UUID
	SourceSessionID      uuid.UUID
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
}

// lockCheckpoint locks a checkpoint until the caller's transaction ends, optionally
// filtering by state. Ownership is required unless internal finalization explicitly sets
// allUsers; no match returns pgx.ErrNoRows.
func lockCheckpoint(ctx context.Context, db pgx.Tx, id string, allUsers bool, userID string, state *string) (sessionCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_session_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision
		FROM session_checkpoint
		WHERE id = $1
		  -- Only internal finalization explicitly opts out of owner filtering.
		  AND ($2::boolean OR user_id = $3)
		  AND ($4::text IS NULL OR state = $4)
		FOR UPDATE
	`, pgx.RowToStructByName[sessionCheckpointRow], id, allUsers, userID, state)
}

// readCheckpointEvents returns the immutable events through a checkpoint's saved history
// boundary in sequence order. Callers authorize access and hold the checkpoint lock when
// forking.
func readCheckpointEvents(ctx context.Context, db dbExecutor, checkpointID uuid.UUID) ([]sessionTaskEventRow, error) {
	return queryMany(ctx, db, `
		SELECT e.sequence, e.history_id, e.task_id, e.data, e.created_at, e.message_id, e.task_position,
		    e.snapshot_atespace, e.snapshot_uri, e.snapshot_content_scope
		FROM session_checkpoint c
		JOIN session_task_event e
		  ON e.history_id = c.source_history_id
		 AND e.sequence <= c.history_sequence
		WHERE c.id = $1
		ORDER BY e.sequence
	`, pgx.RowToStructByName[sessionTaskEventRow], checkpointID)
}

// insertReplayedTask restores a task projection with its original ordering, timestamps,
// and snapshot boundary. Callers provide the destination history and
// transaction after validating the replay.
func insertReplayedTask(ctx context.Context, db dbExecutor, task sessionTaskRow) error {
	return execSQL(ctx, db, `
		INSERT INTO session_task (
		    history_id, id, state, status_timestamp, data, created_at,
		    snapshot_atespace, snapshot_uri,
		    snapshot_content_scope, history_sequence, position
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		task.HistoryID, task.ID, task.State, task.StatusTimestamp, task.Data, task.CreatedAt,
		task.SnapshotAtespace, task.SnapshotURI,
		task.SnapshotContentScope, task.HistorySequence, task.Position,
	)
}

// readCheckpoint reads an owned checkpoint, optionally restricted to one lifecycle state.
// Missing or unowned checkpoints return pgx.ErrNoRows; the read does not lock the row.
func readCheckpoint(ctx context.Context, db dbExecutor, id, userID string, state *string) (sessionCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_session_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision
		FROM session_checkpoint
		WHERE id = $1 AND user_id = $2
		  -- Lifecycle work also reads creating and deleting checkpoints.
		  AND ($3::text IS NULL OR state = $3)
	`, pgx.RowToStructByName[sessionCheckpointRow], id, userID, state)
}

// readCheckpointRequest returns a checkpoint reserved by the owner/requestID pair, in
// any lifecycle state, or pgx.ErrNoRows. Callers check its source before accepting a retry.
func readCheckpointRequest(ctx context.Context, db dbExecutor, userID, requestID string) (sessionCheckpointRow, error) {
	return queryOne(ctx, db, `
		SELECT id, source_session_id, user_id, request_id, head_task_id, history_sequence, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope, tag_uid, state, data, source_history_id, prepared_revision
		FROM session_checkpoint
		WHERE user_id = $1 AND request_id = $2
	`, pgx.RowToStructByName[sessionCheckpointRow], userID, requestID)
}

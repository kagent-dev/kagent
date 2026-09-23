package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

type taskTurnPhase string

const (
	taskTurnPhaseAdmitted  taskTurnPhase = "ADMITTED"
	taskTurnPhaseIssued    taskTurnPhase = "ISSUED"
	taskTurnPhaseCanceling taskTurnPhase = "CANCELING"
	taskTurnPhaseQuiescing taskTurnPhase = "QUIESCING"
	taskTurnPhaseSettled   taskTurnPhase = "SETTLED"
)

// TaskTurnClaim is the complete durable input granted to one turn executor.
type TaskTurnClaim struct {
	Task     *a2a.Task
	TurnID   uuid.UUID
	OwnerID  uuid.UUID
	Request  *a2a.SendMessageRequest
	Previous *a2a.Task
}

// TaskCancellation reports the exact current turn affected by a cancellation request.
type TaskCancellation struct {
	Task    *a2a.Task
	TurnID  uuid.UUID
	OwnerID *uuid.UUID
	Settled bool
}

// ClaimAgentInstanceTaskTurn grants execution only for the exact admitted turn. Expired
// ownership may transfer while work is still proven unissued.
func (c *Client) ClaimAgentInstanceTaskTurn(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID, lease time.Duration) (*TaskTurnClaim, bool, error) {
	if turnID == uuid.Nil || ownerID == uuid.Nil || lease <= 0 {
		return nil, false, fmt.Errorf("turn, owner, and positive lease are required")
	}
	var claim *TaskTurnClaim
	claimed := false
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		row, err := lockAgentInstanceTask(ctx, tx, instance.HistoryID, taskID)
		if err != nil {
			return notFoundOr(err)
		}
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		if err := loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{task}, nil); err != nil {
			return err
		}
		claim = &TaskTurnClaim{Task: task, TurnID: turnID, OwnerID: ownerID}
		if row.TurnID == nil || *row.TurnID != turnID || row.TurnPhase == nil || *row.TurnPhase != taskTurnPhaseAdmitted {
			return nil
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return nil
		}
		updated, err := queryOne(ctx, tx, `
			UPDATE agent_instance_task SET turn_owner_id = $4,
			    turn_owner_expires_at = NOW() + ($5 * interval '1 millisecond')
			WHERE history_id = $1 AND id = $2 AND turn_id = $3 AND turn_phase = 'ADMITTED'
			  AND (turn_owner_id IS NULL OR turn_owner_id = $4 OR turn_owner_expires_at <= NOW())
			  AND NOT EXISTS (
			      SELECT 1 FROM agent_instance_checkpoint
			      WHERE source_instance_id = $6 AND state = 'CREATING'
			  )
			RETURNING history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
			    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
			    turn_cancel_requested, position
		`, pgx.RowToStructByName[agentInstanceTaskRow], instance.HistoryID, taskID, turnID, ownerID,
			lease.Milliseconds(), instance.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		claim.Request, err = unmarshalSendMessageRequest(updated.TurnRequest)
		if err != nil {
			return err
		}
		if len(updated.TurnPreviousTask) != 0 {
			claim.Previous, err = unmarshalAgentInstanceTask(updated.TurnPreviousTask)
			if err != nil {
				return err
			}
		}
		claimed = true
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("claim AgentInstance task turn: %w", err)
	}
	return claim, claimed, nil
}

// ReleaseAgentInstanceTaskTurn releases only work still proven unissued.
func (c *Client) ReleaseAgentInstanceTaskTurn(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID) error {
	tag, err := c.db.Exec(ctx, `
		UPDATE agent_instance_task t SET turn_owner_id = NULL, turn_owner_expires_at = NULL
		FROM agent_instance i
		WHERE i.id = $1 AND i.history_id = t.history_id AND t.id = $2 AND t.turn_id = $3
		  AND t.turn_owner_id = $4 AND t.turn_phase = 'ADMITTED'
	`, instanceID, taskID, turnID, ownerID)
	if err != nil {
		return fmt.Errorf("release AgentInstance task turn: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("AgentInstance task turn is no longer releasable: %w", ErrConflict)
	}
	return nil
}

// MarkAgentInstanceTaskTurnIssued durably records possible runtime delivery before I/O.
func (c *Client) MarkAgentInstanceTaskTurnIssued(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID) error {
	tag, err := c.db.Exec(ctx, `
		UPDATE agent_instance_task t SET turn_phase = 'ISSUED'
		FROM agent_instance i
		WHERE i.id = $1 AND i.history_id = t.history_id AND t.id = $2 AND t.turn_id = $3
		  AND t.turn_owner_id = $4 AND t.turn_phase = 'ADMITTED' AND t.turn_owner_expires_at > NOW()
		  AND i.state = 'AGENT_INSTANCE_STATE_READY'
		  AND i.operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
		  AND NOT EXISTS (
		      SELECT 1 FROM agent_instance_checkpoint c
		      WHERE c.source_instance_id = i.id AND c.state = 'CREATING'
		  )
	`, instanceID, taskID, turnID, ownerID)
	if err != nil {
		return fmt.Errorf("mark AgentInstance task turn issued: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("AgentInstance task turn is no longer issuable: %w", ErrConflict)
	}
	return nil
}

// RenewAgentInstanceTaskTurn extends the exact owner's lease using database time and
// returns durable cancellation intent.
func (c *Client) RenewAgentInstanceTaskTurn(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID, lease time.Duration) (bool, error) {
	if lease <= 0 {
		return false, fmt.Errorf("positive turn lease is required")
	}
	cancelRequested, err := queryOne(ctx, c.db, `
		UPDATE agent_instance_task t
		SET turn_owner_expires_at = NOW() + ($5 * interval '1 millisecond')
		FROM agent_instance i
		WHERE i.id = $1 AND i.history_id = t.history_id AND t.id = $2 AND t.turn_id = $3
		  AND t.turn_owner_id = $4 AND t.turn_phase IN ('ISSUED', 'CANCELING', 'QUIESCING')
		  AND i.state = 'AGENT_INSTANCE_STATE_READY'
		  AND i.operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
		RETURNING t.turn_cancel_requested
	`, pgx.RowTo[bool], instanceID, taskID, turnID, ownerID, lease.Milliseconds())
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("AgentInstance task turn is no longer renewable: %w", ErrConflict)
	}
	if err != nil {
		return false, fmt.Errorf("renew AgentInstance task turn: %w", err)
	}
	return cancelRequested, nil
}

// RequestAgentInstanceTaskCancellation records intent for the current turn. An unissued
// turn is canceled and settled atomically without contacting the runtime.
func (c *Client) RequestAgentInstanceTaskCancellation(ctx context.Context, instanceID, taskID string) (*TaskCancellation, error) {
	var result *TaskCancellation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		row, err := lockAgentInstanceTask(ctx, tx, instance.HistoryID, taskID)
		if err != nil {
			return notFoundOr(err)
		}
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		if err := loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{task}, nil); err != nil {
			return err
		}
		result = &TaskCancellation{Task: task, OwnerID: row.TurnOwnerID}
		if task.Status.State.Terminal() {
			result.Settled = true
			return nil
		}
		if row.TurnID == nil || row.TurnPhase == nil || *row.TurnPhase == taskTurnPhaseSettled {
			return ErrFailedPrecondition
		}
		result.TurnID = *row.TurnID
		switch *row.TurnPhase {
		case taskTurnPhaseAdmitted:
			canceled := *task
			now := time.Now().UTC()
			canceled.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled, Timestamp: &now}
			if err := storeAgentInstanceTaskEvent(ctx, tx, instance, &canceled, &canceled, nil); err != nil {
				return err
			}
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_task SET turn_phase = 'SETTLED', turn_owner_id = NULL,
				    turn_owner_expires_at = NULL, turn_cancel_requested = TRUE
				WHERE history_id = $1 AND id = $2 AND turn_id = $3 AND turn_phase = 'ADMITTED'
			`, instance.HistoryID, taskID, *row.TurnID); err != nil {
				return err
			}
			result.Task, result.OwnerID, result.Settled = &canceled, nil, true
		case taskTurnPhaseIssued, taskTurnPhaseCanceling, taskTurnPhaseQuiescing:
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_task SET turn_cancel_requested = TRUE
				WHERE history_id = $1 AND id = $2 AND turn_id = $3
			`, instance.HistoryID, taskID, *row.TurnID); err != nil {
				return err
			}
		default:
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("request AgentInstance task cancellation: %w", err)
	}
	return result, nil
}

// BeginAgentInstanceTaskCancellation is the issue marker for the runtime cancel call.
func (c *Client) BeginAgentInstanceTaskCancellation(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID) (bool, error) {
	tag, err := c.db.Exec(ctx, `
		UPDATE agent_instance_task t SET turn_phase = 'CANCELING'
		FROM agent_instance i
		WHERE i.id = $1 AND i.history_id = t.history_id AND t.id = $2 AND t.turn_id = $3
		  AND t.turn_owner_id = $4 AND t.turn_owner_expires_at > NOW()
		  AND t.turn_phase = 'ISSUED' AND t.turn_cancel_requested
		  AND i.operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
	`, instanceID, taskID, turnID, ownerID)
	if err != nil {
		return false, fmt.Errorf("begin AgentInstance task cancellation: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// BeginAgentInstanceTaskQuiescence records possible Pause/Quiesce issuance.
func (c *Client) BeginAgentInstanceTaskQuiescence(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID, resultState a2a.TaskState) error {
	tag, err := c.db.Exec(ctx, `
		UPDATE agent_instance_task t SET turn_phase = 'QUIESCING'
		FROM agent_instance i
		WHERE i.id = $1 AND i.history_id = t.history_id AND t.id = $2 AND t.turn_id = $3
		  AND t.turn_owner_id = $4 AND t.turn_owner_expires_at > NOW()
		  AND t.turn_phase IN ('ISSUED', 'CANCELING')
		  AND (NOT t.turn_cancel_requested OR $5 = ANY($6::text[]))
		  AND i.operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
	`, instanceID, taskID, turnID, ownerID, string(resultState), []string{
		string(a2a.TaskStateCompleted), string(a2a.TaskStateCanceled), string(a2a.TaskStateFailed), string(a2a.TaskStateRejected),
	})
	if err != nil {
		return fmt.Errorf("begin AgentInstance task quiescence: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("AgentInstance task turn cannot begin quiescence: %w", ErrConflict)
	}
	return nil
}

// StoreOwnedAgentInstanceTaskEvent persists non-quiescent progress for the exact owner.
func (c *Client) StoreOwnedAgentInstanceTaskEvent(ctx context.Context, instanceID string, turnID, ownerID uuid.UUID, task *a2a.Task, event a2a.Event) error {
	if task == nil || taskTurnQuiescent(task.Status.State) {
		return fmt.Errorf("owned progress requires a non-quiescent task")
	}
	return c.withOwnedTaskTurn(ctx, instanceID, string(task.ID), turnID, ownerID,
		[]taskTurnPhase{taskTurnPhaseIssued, taskTurnPhaseCanceling}, func(tx pgx.Tx, instance agentInstanceRow) error {
			return storeAgentInstanceTaskEvent(ctx, tx, instance, task, event, nil)
		})
}

// SettleAgentInstanceTaskTurn atomically publishes the quiescent task boundary and
// releases durable execution ownership after the external quiescence effect succeeded.
func (c *Client) SettleAgentInstanceTaskTurn(ctx context.Context, instanceID string, turnID, ownerID uuid.UUID, task *a2a.Task, event a2a.Event, snapshot *AgentInstanceTaskSnapshot) error {
	if task == nil || !taskTurnQuiescent(task.Status.State) {
		return fmt.Errorf("turn settlement requires a quiescent task")
	}
	return c.withOwnedTaskTurn(ctx, instanceID, string(task.ID), turnID, ownerID,
		[]taskTurnPhase{taskTurnPhaseQuiescing}, func(tx pgx.Tx, instance agentInstanceRow) error {
			if err := storeAgentInstanceTaskEvent(ctx, tx, instance, task, event, snapshot); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `
				UPDATE agent_instance_task SET turn_phase = 'SETTLED', turn_owner_id = NULL,
				    turn_owner_expires_at = NULL
				WHERE history_id = $1 AND id = $2 AND turn_id = $3 AND turn_owner_id = $4
				  AND turn_phase = 'QUIESCING'
			`, instance.HistoryID, string(task.ID), turnID, ownerID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrConflict
			}
			return nil
		})
}

func (c *Client) withOwnedTaskTurn(ctx context.Context, instanceID, taskID string, turnID, ownerID uuid.UUID, phases []taskTurnPhase, fn func(pgx.Tx, agentInstanceRow) error) error {
	phaseValues := make([]string, len(phases))
	for i, phase := range phases {
		phaseValues[i] = string(phase)
	}
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return ErrConflict
		}
		owned, err := queryOne(ctx, tx, `
			SELECT EXISTS (
			    SELECT 1 FROM agent_instance_task
			    WHERE history_id = $1 AND id = $2 AND turn_id = $3 AND turn_owner_id = $4
			      AND turn_owner_expires_at > NOW() AND turn_phase = ANY($5::text[])
			    FOR UPDATE
			)
		`, pgx.RowTo[bool], instance.HistoryID, taskID, turnID, ownerID, phaseValues)
		if err != nil {
			return err
		}
		if !owned {
			return ErrConflict
		}
		return fn(tx, instance)
	})
	if err != nil {
		return fmt.Errorf("persist owned AgentInstance task turn: %w", err)
	}
	return nil
}

func lockAgentInstanceTask(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, taskID string) (agentInstanceTaskRow, error) {
	return queryOne(ctx, tx, `
		SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
		    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
		    turn_cancel_requested, position
		FROM agent_instance_task WHERE history_id = $1 AND id = $2 FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, taskID)
}

func marshalSendMessageRequest(request *a2a.SendMessageRequest) ([]byte, error) {
	if request == nil || request.Message == nil {
		return nil, fmt.Errorf("message is required")
	}
	pb, err := pbconv.ToProtoSendMessageRequest(request)
	if err != nil {
		return nil, err
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(pb)
}

func unmarshalSendMessageRequest(data []byte) (*a2a.SendMessageRequest, error) {
	pb := &a2apb.SendMessageRequest{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, fmt.Errorf("decode admitted send request: %w", err)
	}
	request, err := pbconv.FromProtoSendMessageRequest(pb)
	if err != nil {
		return nil, fmt.Errorf("convert admitted send request: %w", err)
	}
	return request, nil
}

func marshalTask(task *a2a.Task) ([]byte, error) {
	pb, err := pbconv.ToProtoTask(task)
	if err != nil {
		return nil, err
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(pb)
}

func taskTurnQuiescent(state a2a.TaskState) bool {
	return state.Terminal() || state == a2a.TaskStateInputRequired || state == a2a.TaskStateAuthRequired
}

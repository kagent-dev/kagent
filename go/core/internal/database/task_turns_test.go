package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestAgentInstanceTaskTurnConstraints(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()
	historyID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO a2a_context (id, user_id, context_id) VALUES ($1, 'alice', $1)
	`, historyID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO agent_instance_task (history_id, id, state, data)
		VALUES ($1, 'task', 'TASK_STATE_SUBMITTED', $2)
	`, historyID, []byte("task"))
	require.NoError(t, err)

	turnID, ownerID := uuid.New(), uuid.New()
	lease := time.Now().Add(time.Minute)
	for _, test := range []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "unknown phase",
			query: `UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'UNKNOWN', turn_request = $3
				WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID, turnID, []byte("request")},
		},
		{
			name: "missing request",
			query: `UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'ADMITTED'
				WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID, turnID},
		},
		{
			name: "owner without lease",
			query: `UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'ADMITTED', turn_request = $3,
				turn_owner_id = $4 WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID, turnID, []byte("request"), ownerID},
		},
		{
			name: "issued without owner",
			query: `UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'ISSUED', turn_request = $3
				WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID, turnID, []byte("request")},
		},
		{
			name: "settled with owner",
			query: `UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'SETTLED', turn_request = $3,
				turn_owner_id = $4, turn_owner_expires_at = $5 WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID, turnID, []byte("request"), ownerID, lease},
		},
		{
			name: "canceling without cancellation intent",
			query: `UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'CANCELING', turn_request = $3,
				turn_owner_id = $4, turn_owner_expires_at = $5 WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID, turnID, []byte("request"), ownerID, lease},
		},
		{
			name: "cancellation without turn",
			query: `UPDATE agent_instance_task SET turn_cancel_requested = TRUE
				WHERE history_id = $1 AND id = 'task'`,
			args: []any{historyID},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := db.Exec(ctx, test.query, test.args...)
			require.Error(t, err)
		})
	}

	_, err = db.Exec(ctx, `
		UPDATE agent_instance_task SET turn_id = $2, turn_phase = 'ADMITTED', turn_request = $3
		WHERE history_id = $1 AND id = 'task'
	`, historyID, turnID, []byte("request"))
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		UPDATE agent_instance_task SET turn_owner_id = $2, turn_owner_expires_at = $3
		WHERE history_id = $1 AND id = 'task'
	`, historyID, ownerID, lease)
	require.NoError(t, err)

	for _, phase := range []string{"ISSUED", "CANCELING", "QUIESCING"} {
		_, err := db.Exec(ctx, `
			UPDATE agent_instance_task SET turn_id = $2, turn_phase = $3, turn_request = $4,
			    turn_owner_id = $5, turn_owner_expires_at = $6,
			    turn_cancel_requested = ($3 = 'CANCELING')
			WHERE history_id = $1 AND id = 'task'
		`, historyID, turnID, phase, []byte("request"), ownerID, lease)
		require.NoError(t, err, phase)
	}
	_, err = db.Exec(ctx, `
		UPDATE agent_instance_task SET turn_phase = 'SETTLED', turn_owner_id = NULL,
		    turn_owner_expires_at = NULL
		WHERE history_id = $1 AND id = 'task'
	`, historyID)
	require.NoError(t, err)
}

func TestAgentInstanceTaskTurnOwnership(t *testing.T) {
	db := setupTestDB(t)
	client, ctx := NewClient(db), t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(ctx, client, instance.Id, "agent.example")
	require.NoError(t, err)

	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	task.History[0].TaskID, task.History[0].ContextID = task.ID, task.ContextID
	request := &a2a.SendMessageRequest{Message: task.History[0], Config: &a2a.SendMessageConfig{ReturnImmediately: true}}
	admission, err := client.AdmitAgentInstanceTask(ctx, instance.Id, []byte("request hash"), request, task)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, admission.TurnID)

	firstOwner, secondOwner, staleOwner := uuid.New(), uuid.New(), uuid.New()
	claim, claimed, err := client.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, firstOwner, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, request.Message.ID, claim.Request.Message.ID)
	require.Nil(t, claim.Previous)
	_, claimed, err = client.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner, time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "an unexpired admitted owner must be exclusive")

	_, err = db.Exec(ctx, `
		UPDATE agent_instance_task SET turn_owner_expires_at = NOW() - interval '1 second'
		WHERE history_id = (SELECT history_id FROM agent_instance WHERE id = $1) AND id = $2
	`, instance.Id, task.ID)
	require.NoError(t, err)
	claim, claimed, err = client.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed, "expired work proven unissued may transfer")
	require.Equal(t, secondOwner, claim.OwnerID)
	require.ErrorIs(t, client.MarkAgentInstanceTaskTurnIssued(ctx, instance.Id, string(task.ID), admission.TurnID, firstOwner), ErrConflict)
	require.NoError(t, client.MarkAgentInstanceTaskTurnIssued(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner))

	_, err = db.Exec(ctx, `
		UPDATE agent_instance_task SET turn_owner_expires_at = NOW() - interval '1 second'
		WHERE history_id = (SELECT history_id FROM agent_instance WHERE id = $1) AND id = $2
	`, instance.Id, task.ID)
	require.NoError(t, err)
	_, claimed, err = client.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, staleOwner, time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "possibly issued work must never transfer")
	cancelRequested, err := client.RenewAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner, time.Minute)
	require.NoError(t, err, "the same issued owner may renew after expiry")
	require.False(t, cancelRequested)

	working := *task
	working.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
	require.ErrorIs(t, client.StoreOwnedAgentInstanceTaskEvent(ctx, instance.Id, admission.TurnID, firstOwner, &working, &working), ErrConflict)
	require.NoError(t, client.StoreOwnedAgentInstanceTaskEvent(ctx, instance.Id, admission.TurnID, secondOwner, &working, &working))
	_, _, err = client.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{
		Id: uuid.NewString(), AgentInstanceId: instance.Id,
	}, "alice", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	_, err = client.BeginAgentInstanceOperation(ctx, instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.BeginAgentInstanceOperation(ctx, instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.ErrorIs(t, err, ErrConflict)

	cancellation, err := client.RequestAgentInstanceTaskCancellation(ctx, instance.Id, string(task.ID))
	require.NoError(t, err)
	require.False(t, cancellation.Settled)
	require.NotNil(t, cancellation.OwnerID)
	require.Equal(t, secondOwner, *cancellation.OwnerID)
	cancelRequested, err = client.RenewAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner, time.Minute)
	require.NoError(t, err)
	require.True(t, cancelRequested)
	began, err := client.BeginAgentInstanceTaskCancellation(ctx, instance.Id, string(task.ID), admission.TurnID, firstOwner)
	require.NoError(t, err)
	require.False(t, began)
	began, err = client.BeginAgentInstanceTaskCancellation(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner)
	require.NoError(t, err)
	require.True(t, began)
	began, err = client.BeginAgentInstanceTaskCancellation(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner)
	require.NoError(t, err)
	require.False(t, began, "the runtime cancellation marker is one-shot")

	require.ErrorIs(t, client.BeginAgentInstanceTaskQuiescence(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner, a2a.TaskStateInputRequired), ErrConflict)
	require.NoError(t, client.BeginAgentInstanceTaskQuiescence(ctx, instance.Id, string(task.ID), admission.TurnID, secondOwner, a2a.TaskStateCanceled))
	canceled := working
	canceled.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled}
	snapshot := &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}
	require.ErrorIs(t, client.SettleAgentInstanceTaskTurn(ctx, instance.Id, admission.TurnID, firstOwner, &canceled, &canceled, snapshot), ErrConflict)
	require.NoError(t, client.SettleAgentInstanceTaskTurn(ctx, instance.Id, admission.TurnID, secondOwner, &canceled, &canceled, snapshot))

	instanceRow, err := readAgentInstance(ctx, db, instance.Id)
	require.NoError(t, err)
	row, err := readAgentInstanceTask(ctx, db, instanceRow.HistoryID, string(task.ID))
	require.NoError(t, err)
	require.NotNil(t, row.TurnID)
	require.Equal(t, admission.TurnID, *row.TurnID)
	require.NotNil(t, row.TurnPhase)
	require.Equal(t, taskTurnPhaseSettled, *row.TurnPhase)
	require.Nil(t, row.TurnOwnerID)
	require.Nil(t, row.TurnOwnerExpiresAt)
	require.True(t, row.TurnCancelRequested)
}

func TestCancelAdmittedAgentInstanceTaskTurnWithoutRuntime(t *testing.T) {
	db := setupTestDB(t)
	client, ctx := NewClient(db), t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(ctx, client, instance.Id, "agent.example")
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	task.History[0].TaskID, task.History[0].ContextID = task.ID, task.ContextID
	request := &a2a.SendMessageRequest{Message: task.History[0]}
	admission, err := client.AdmitAgentInstanceTask(ctx, instance.Id, []byte("request hash"), request, task)
	require.NoError(t, err)
	result, err := client.RequestAgentInstanceTaskCancellation(ctx, instance.Id, string(task.ID))
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.Equal(t, a2a.TaskStateCanceled, result.Task.Status.State)
	_, claimed, err := client.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, uuid.New(), time.Minute)
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestAgentInstanceTaskIssueRacesUnissuedCancellation(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(ctx, client, instance.Id, "agent.example")
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	task.History[0].TaskID, task.History[0].ContextID = task.ID, task.ContextID
	request := &a2a.SendMessageRequest{Message: task.History[0]}
	admission, err := client.AdmitAgentInstanceTask(ctx, instance.Id, []byte("request hash"), request, task)
	require.NoError(t, err)
	owner := uuid.New()
	_, claimed, err := client.ClaimAgentInstanceTaskTurn(ctx, instance.Id, string(task.ID), admission.TurnID, owner, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	start := make(chan struct{})
	issueResult := make(chan error, 1)
	cancelResult := make(chan struct {
		cancellation *TaskCancellation
		err          error
	}, 1)
	go func() {
		<-start
		issueResult <- client.MarkAgentInstanceTaskTurnIssued(ctx, instance.Id, string(task.ID), admission.TurnID, owner)
	}()
	go func() {
		<-start
		cancellation, cancelErr := client.RequestAgentInstanceTaskCancellation(ctx, instance.Id, string(task.ID))
		cancelResult <- struct {
			cancellation *TaskCancellation
			err          error
		}{cancellation: cancellation, err: cancelErr}
	}()
	close(start)
	issueErr, canceled := <-issueResult, <-cancelResult
	require.NoError(t, canceled.err)
	if issueErr == nil {
		require.False(t, canceled.cancellation.Settled)
		require.NotNil(t, canceled.cancellation.OwnerID)
		return
	}
	require.ErrorIs(t, issueErr, ErrConflict)
	require.True(t, canceled.cancellation.Settled)
	require.Equal(t, a2a.TaskStateCanceled, canceled.cancellation.Task.Status.State)
}

func TestContinuationTurnStoresPreviousTaskAndHistoricalReceipt(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.ID, reply.TaskID, reply.ContextID = "reply-one", waiting.ID, waiting.ContextID
	request := &a2a.SendMessageRequest{Message: reply, Config: &a2a.SendMessageConfig{ReturnImmediately: true}}
	first, err := client.AdmitAgentInstanceTaskContinuation(t.Context(), instance.Id, []byte("reply one"), request)
	require.NoError(t, err)
	owner := uuid.New()
	claim, claimed, err := client.ClaimAgentInstanceTaskTurn(t.Context(), instance.Id, string(waiting.ID), first.TurnID, owner, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, claim.Previous)
	require.Equal(t, a2a.TaskStateInputRequired, claim.Previous.Status.State)
	require.Equal(t, waiting.Status.Message.ID, claim.Previous.Status.Message.ID)
	require.Equal(t, reply.ID, claim.Request.Message.ID)
	require.NoError(t, client.MarkAgentInstanceTaskTurnIssued(t.Context(), instance.Id, string(waiting.ID), first.TurnID, owner))
	require.NoError(t, client.BeginAgentInstanceTaskQuiescence(t.Context(), instance.Id, string(waiting.ID), first.TurnID, owner, a2a.TaskStateInputRequired))

	nextWaiting := *claim.Task
	nextQuestion := a2a.NewMessageForTask(a2a.MessageRoleAgent, &nextWaiting, a2a.NewTextPart("Which table?"))
	nextQuestion.ID = "question-two"
	nextWaiting.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: nextQuestion}
	require.NoError(t, client.SettleAgentInstanceTaskTurn(t.Context(), instance.Id, first.TurnID, owner, &nextWaiting, &nextWaiting, nil))

	secondReply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("customers"))
	secondReply.ID, secondReply.TaskID, secondReply.ContextID = "reply-two", waiting.ID, waiting.ContextID
	secondRequest := &a2a.SendMessageRequest{Message: secondReply}
	second, err := client.AdmitAgentInstanceTaskContinuation(t.Context(), instance.Id, []byte("reply two"), secondRequest)
	require.NoError(t, err)
	require.NotEqual(t, first.TurnID, second.TurnID)
	require.ErrorIs(t, client.BeginAgentInstanceTaskQuiescence(t.Context(), instance.Id, string(waiting.ID), first.TurnID, owner, a2a.TaskStateCompleted), ErrConflict)

	retry, err := client.AdmitAgentInstanceTaskContinuation(t.Context(), instance.Id, []byte("reply one"), request)
	require.NoError(t, err)
	require.Equal(t, first.TurnID, retry.TurnID)
	_, claimed, err = client.ClaimAgentInstanceTaskTurn(t.Context(), instance.Id, string(waiting.ID), retry.TurnID, uuid.New(), time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "a historical receipt cannot claim the current turn")

	secondClaim, claimed, err := client.ClaimAgentInstanceTaskTurn(t.Context(), instance.Id, string(waiting.ID), second.TurnID, uuid.New(), time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, secondClaim.Previous)
	require.Equal(t, "question-two", secondClaim.Previous.Status.Message.ID)
}

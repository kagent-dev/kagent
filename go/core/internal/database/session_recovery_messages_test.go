package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestCrashedRecoveryRejectsMalformedMessageHistory(t *testing.T) {
	for _, test := range []struct {
		name         string
		data         []byte
		emptyPayload bool
		mutate       func(*a2apb.Message)
	}{
		{name: "invalid protobuf", data: []byte{0xff}},
		{name: "not a message", emptyPayload: true},
		{name: "message identity", mutate: func(message *a2apb.Message) {
			message.MessageId = "different-message"
		}},
		{name: "task identity", mutate: func(message *a2apb.Message) {
			message.TaskId = "different-task"
		}},
		{name: "context identity", mutate: func(message *a2apb.Message) {
			message.ContextId = "different-context"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
			session, task := waitingTaskFixture(t, client)
			message := &a2apb.Message{
				MessageId: task.History[0].ID, TaskId: string(task.ID), ContextId: session.ContextId,
				Role: a2apb.Role_ROLE_USER,
			}
			if test.mutate != nil {
				test.mutate(message)
			}
			data := test.data
			if data == nil {
				event := &a2apb.StreamResponse{}
				if !test.emptyPayload {
					event.Payload = &a2apb.StreamResponse_Message{Message: message}
				}
				var err error
				data, err = proto.Marshal(event)
				require.NoError(t, err)
			}
			// Ordinary task writes reject these malformed identities before persistence.
			_, err := client.db.Exec(t.Context(), `
				UPDATE session_task_event SET data = $2
				WHERE task_id = $1 AND message_id = $3
			`, string(task.ID), data, task.History[0].ID)
			require.NoError(t, err)

			_, err = client.BeginCrashedSessionRecovery(t.Context(), session.Id, lifecycleGeneration(t, client, session.Id), "actor-"+session.Id, SessionTaskSnapshot{})
			require.Error(t, err)
			current, err := client.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation)
			require.Empty(t, current.GetRecovery().GetFailedMessages())
			require.Nil(t, current.Failure)
		})
	}
}

func TestCrashedRecoveryRollsBackAnnotationsWhenSettlementFails(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	_, version, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	for _, state := range []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateInputRequired} {
		task.Status.State = state
		version, err = client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash(string(state)), task, task, "")
		require.NoError(t, err)
	}
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	stored, err := readSession(t.Context(), client.db, session.Id)
	require.NoError(t, err)
	before, err := readSessionTask(t.Context(), client.db, stored.HistoryID, string(task.ID))
	require.NoError(t, err)
	// Message history remains valid, so recovery first writes its annotations.
	// Corrupting the task projection makes settlement fail later in that transaction.
	_, err = client.db.Exec(t.Context(), `UPDATE session_task SET data = $2 WHERE history_id = $1 AND id = $3`, stored.HistoryID, []byte{0xff}, string(task.ID))
	require.NoError(t, err)
	_, err = client.BeginCrashedSessionRecovery(t.Context(), session.Id, lifecycleGeneration(t, client, session.Id), "actor-"+session.Id, SessionTaskSnapshot{})
	require.Error(t, err)
	current, err := client.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation)
	require.Empty(t, current.GetRecovery().GetFailedMessages())

	_, err = client.db.Exec(t.Context(), `UPDATE session_task SET data = $2 WHERE history_id = $1 AND id = $3`, stored.HistoryID, before.Data, string(task.ID))
	require.NoError(t, err)
	work, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err, "failed recovery retains the original pending runtime boundary")
	require.Equal(t, string(task.ID), work.TaskID)
}

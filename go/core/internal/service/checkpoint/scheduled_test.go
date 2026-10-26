package checkpoint

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestScheduledCheckpointAccessSurvivesSourceDeletion(t *testing.T) {
	for _, deletion := range []string{"live", "explicit", "idle"} {
		t.Run(deletion, func(t *testing.T) {
			store, source := scheduledCheckpointFixture(t)
			tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1"}
			policy := &restrictiveAuthorizer{}
			svc := NewService(store, policy, tags, &testWorkflow{})
			human := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			checkpoint, err := svc.Create(human, source.Id, "checkpoint", "task-1")
			require.NoError(t, err)
			_, err = store.GetSessionCheckpoint(t.Context(), checkpoint.Id, "scheduler")
			require.NoError(t, err, "checkpoint remains under the source owner")
			_, err = store.GetSessionCheckpoint(t.Context(), checkpoint.Id, "alice")
			require.ErrorIs(t, err, database.ErrNotFound)

			if deletion != "live" {
				if deletion == "idle" {
					_, err := store.BeginIdleSessionDeletion(t.Context(), source.Id, time.Now().Add(time.Second))
					require.NoError(t, err)
				}
				finishCheckpointSessionOperation(t, store, source.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
				_, err = store.GetSessionByID(t.Context(), source.Id)
				require.ErrorIs(t, err, database.ErrNotFound)
			}
			for _, operation := range []string{"create", "get", "list", "fork", "rename", "delete"} {
				policy.resource, policy.verb = "ScheduledRunHistory", auth.VerbGet
				switch operation {
				case "rename":
					policy.resource, policy.verb = "ScheduledRunInvocation", auth.VerbUpdate
				case "delete":
					policy.resource, policy.verb = "ScheduledRunInvocation", auth.VerbDelete
				}
				switch operation {
				case "create":
					_, err = svc.Create(human, source.Id, "checkpoint", "task-1")
				case "get":
					_, err = svc.Get(human, checkpoint.Id)
				case "list":
					_, err = svc.List(human, ListRequest{SessionID: source.Id})
				case "fork":
					_, err = svc.Fork(human, checkpoint.Id, "denied-fork")
				case "rename":
					_, err = svc.Rename(human, checkpoint.Id, "denied-name")
				case "delete":
					err = svc.Delete(human, checkpoint.Id)
				}
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), "%s: %v", operation, err)
				last := policy.calls[len(policy.calls)-1]
				require.Equal(t, auth.Resource{Type: policy.resource, Namespace: "team-a", Name: "daily"}, last.resource)
			}
			policy.resource = ""
			replayed, err := svc.Create(human, source.Id, "checkpoint", "task-1")
			require.NoError(t, err)
			require.Equal(t, checkpoint.Id, replayed.Id)
			if deletion != "live" {
				_, err = svc.Create(human, source.Id, "new-checkpoint", "task-1")
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), "%v", err)
			}
			got, err := svc.Get(human, checkpoint.Id)
			require.NoError(t, err)
			require.Equal(t, checkpoint.Id, got.Id)
			listed, err := svc.List(human, ListRequest{SessionID: source.Id})
			require.NoError(t, err)
			require.Len(t, listed.Checkpoints, 1)
			renamed, err := svc.Rename(human, checkpoint.Id, "Report")
			require.NoError(t, err)
			require.Equal(t, "Report", renamed.Name)
			fork, err := svc.Fork(human, checkpoint.Id, "fork")
			require.NoError(t, err)
			require.Equal(t, "alice", fork.Creator)
			require.Nil(t, fork.GetScheduledRun(), "a user's fork is not a scheduled execution")
			require.True(t, proto.Equal(source.GetAgent(), fork.GetAgent()))
			retried, err := svc.Fork(human, checkpoint.Id, "fork")
			require.NoError(t, err)
			require.Equal(t, fork.Id, retried.Id)
			other, err := svc.Fork(auth.AuthSessionTo(t.Context(), testSession{userID: "bob"}), checkpoint.Id, "fork")
			require.NoError(t, err)
			require.Equal(t, "bob", other.Creator)
			require.NotEqual(t, fork.Id, other.Id, "fork idempotency belongs to each caller")
			finishCheckpointSessionOperation(t, store, fork.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
			finishCheckpointSessionOperation(t, store, other.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
			require.NoError(t, svc.Delete(human, checkpoint.Id))
			_, err = svc.Get(human, checkpoint.Id)
			require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound), "%v", err)
		})
	}
}

func TestOrdinaryCheckpointsRemainPrivate(t *testing.T) {
	store := &testStore{checkpointOwner: "bob", prepared: &apiv1.Checkpoint{
		Id: uuid.NewString(), SessionId: uuid.NewString(), State: apiv1.CheckpointState_CHECKPOINT_STATE_READY,
	}}
	svc := NewService(store, &auth.NoopAuthorizer{}, nil, nil)
	ctx := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	_, err := svc.Create(ctx, store.prepared.SessionId, "create", "task")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = svc.Get(ctx, store.prepared.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = svc.List(ctx, ListRequest{SessionID: store.prepared.SessionId})
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = svc.Rename(ctx, store.prepared.Id, "name")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = svc.Fork(ctx, store.prepared.Id, "fork")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	err = svc.Delete(ctx, store.prepared.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	require.Zero(t, store.reservations)
	require.False(t, store.deleted)
}

func scheduledCheckpointFixture(t *testing.T) (*database.Client, *apiv1.Session) {
	t.Helper()
	ctx := t.Context()
	dsn := dbtest.StartT(context.Background(), t)
	dbtest.MigrateT(t, dsn, false)
	pool, err := database.Connect(ctx, &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool, "public")
	def := database.AgentDefinition{Namespace: "team-a", AgentName: "daily", AgentUID: uuid.NewString(), DesiredRevision: "revision"}
	require.NoError(t, store.UpsertAgentDefinition(ctx, def))
	require.NoError(t, store.RecordRuntimeRevision(ctx, database.RuntimeRevision{Namespace: def.Namespace, AgentName: def.AgentName, AgentUID: def.AgentUID, Revision: def.DesiredRevision,
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", ActorTemplateUID: "template-uid",
	}, database.RecordRevisionOptions{Ready: true}))
	schedule, err := store.SyncScheduledRun(ctx, &database.ScheduledRun{
		ID: def.AgentUID, Creator: "scheduler", Ref: types.NamespacedName{Namespace: "team-a", Name: "daily"}, Generation: 1, ResourceVersion: "1",
		Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: def.AgentName}, Schedule: "0 9 * * *", Prompt: "report", Paused: new(true)}, AgentUID: def.AgentUID,
	})
	require.NoError(t, err)
	execution, err := store.TriggerScheduledRun(ctx, uuid.MustParse(schedule.ID), "alice", "manual", 1)
	require.NoError(t, err)
	finishCheckpointSessionOperation(t, store, execution.SessionId, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	source, err := store.GetSessionByID(ctx, execution.SessionId)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("report"))
	message.ContextID = source.ContextId
	task := a2a.NewSubmittedTask(message, message)
	task.ID = "task-1"
	initialHash := sha256.Sum256([]byte("create"))
	version, err := store.CreateRuntimeTask(ctx, source.Id, initialHash[:], task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	completedHash := sha256.Sum256([]byte("complete"))
	version, err = store.UpdateSessionTask(ctx, source.Id, version, completedHash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(ctx, source.Id, string(task.ID), version))
	boundary, err := store.ClaimSessionQuiescence(ctx)
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(ctx, boundary, &database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	return store, source
}

func finishCheckpointSessionOperation(t *testing.T, store *database.Client, id string, kind apiv1.RuntimeOperation) {
	t.Helper()
	operation, err := store.BeginSessionOperation(t.Context(), id, kind)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := store.ClaimSessionOperation(t.Context(), id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	actorUID := ""
	if kind == apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		actorUID = "actor-uid"
	}
	_, err = store.FinishSessionOperation(t.Context(), id, operation.ID, executor, "runtime.team-a.ate", actorUID, "")
	require.NoError(t, err)
}

func TestScheduledCheckpointRequestIDsBelongToCaller(t *testing.T) {
	store, source := scheduledCheckpointFixture(t)
	svc := NewService(store, &restrictiveAuthorizer{}, &testTags{snapshotURI: "s3://snapshots/snapshot-1"}, &testWorkflow{})
	alice := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	bob := auth.AuthSessionTo(t.Context(), testSession{userID: "bob"})
	first, err := svc.Create(alice, source.Id, "same", "task-1")
	require.NoError(t, err)
	bobService := NewService(store, &restrictiveAuthorizer{}, &testTags{snapshotURI: "s3://snapshots/snapshot-1"}, &testWorkflow{})
	second, err := bobService.Create(bob, source.Id, "same", "task-1")
	require.NoError(t, err)
	require.NotEqual(t, first.Id, second.Id)
	retry, err := svc.Create(alice, source.Id, "same", "task-1")
	require.NoError(t, err)
	require.Equal(t, first.Id, retry.Id)
}

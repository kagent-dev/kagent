package session

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestActorWorkflowLifecycle(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)

	created, err := workflow.Create(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY || created.GetA2AAuthority() == "" {
		t.Fatalf("created session = %+v", created)
	}
	if len(actors.actors) != 1 {
		t.Fatalf("actors = %v", actors.actors)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("created Actor status = %s", actor.GetStatus().GetState())
	}
	actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))].Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	boundary, err := workflow.Quiesce(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY || boundary.URI != "s3://snapshots/snapshot-1" {
		t.Fatalf("quiesced session = %+v, boundary = %+v", created, boundary)
	}

	suspended, err := workflow.Suspend(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.GetState() != apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED || suspended.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		t.Fatalf("suspended session = %+v", suspended)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("suspended Actor status = %s", actor.GetStatus().GetState())
	}

	resumed, err := workflow.Resume(context.Background(), suspended)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY || resumed.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		t.Fatalf("resumed session = %+v", resumed)
	}

	deleted, err := workflow.Delete(context.Background(), resumed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	if deleted.GetState() != apiv1.RuntimeState_RUNTIME_STATE_DELETED || len(actors.actors) != 0 {
		t.Fatalf("deleted session = %+v, actors = %v", deleted, actors.actors)
	}
}

func TestActorWorkflowRejectsReplacedRuntime(t *testing.T) {
	for _, operation := range []string{"quiesce", "suspend", "delete"} {
		t.Run(operation, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			session, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			actor := actors.actors[actorKey("team-a", substrate.ActorName(session.Id))]
			actor.Metadata.Uid = "replacement-uid"
			actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
			switch operation {
			case "quiesce":
				_, err = workflow.Quiesce(t.Context(), session)
			case "suspend":
				_, err = workflow.Suspend(t.Context(), session)
			case "delete":
				_, err = workflow.Delete(t.Context(), session)
			}
			require.ErrorContains(t, err, "verify runtime actor UID")
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, actor.Status.State)
			require.Len(t, actors.actors, 1)
		})
	}
}

func TestActorWorkflowForkCreatesSuspendedActorFromCheckpoint(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, checkpointID := lifecycleForkFixture(t, store, actors, session)
	fork, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	actor := actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))]
	if fork.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY ||
		actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED ||
		actor.GetSourceTag().GetName() != "checkpoint-"+checkpointID {
		t.Fatalf("fork = %+v, actor = %+v", fork, actor)
	}
	actor.Status.Snapshots[0].DurableSnapshot.Object.SnapshotUri = "s3://snapshots/later-turn"
	replayed, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	require.True(t, proto.Equal(fork, replayed), "a retry returns the current session without revalidating later Actor state")
}

// lifecycleFixture uses the same persistence boundary as production; only Actor
// calls are faked, so concurrency assertions exercise PostgreSQL admission.
func lifecycleFixture(t *testing.T) (*lifecycleTestStore, *apiv1.Session) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(cancel)
	conn, cleanup, err := dbtest.Start(ctx)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.NoError(t, dbtest.Migrate(conn, false))
	pool, err := pgxpool.New(t.Context(), conn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	client := database.NewClient(pool, "public")
	revision := &database.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		SourceSnapshot: []byte("{}"),
		AgentCard:      &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision", ActorTemplateUID: "actor-template-uid",
	}
	upsertErr := client.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid", DesiredRevision: revision.Revision})
	require.NoError(t, upsertErr)
	require.NoError(t, client.RecordRuntimeRevision(t.Context(), *revision, database.RecordRevisionOptions{Ready: true}))
	session, _, err := client.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}}, uuid.NewString())
	require.NoError(t, err)
	return &lifecycleTestStore{Client: client, revision: revision}, session
}

type lifecycleTestStore struct {
	*database.Client
	revision *database.RuntimeRevision
}

func (s *lifecycleTestStore) GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error) {
	return s.revision, nil
}

type lifecycleTestActors struct {
	mu             sync.Mutex
	actors         map[string]*ateapipb.Actor
	policyErr      error
	policyRevision string
	policyActor    string
	policyCalls    int
}

func actorKey(atespace, name string) string { return atespace + "/" + name }

func (a *lifecycleTestActors) GetActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	if actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) CreateActor(_ context.Context, atespace, name, templateNamespace, templateName string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateNamespace, Name: templateName},
		Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED},
	}
	a.actors[actorKey(atespace, name)] = actor
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) CreateActorFromTag(_ context.Context, atespace, name, templateNamespace, templateName, tagAtespace, tagName string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: atespace, Name: name, Uid: "actor-uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateNamespace, Name: templateName},
		SourceTag:     &ateapipb.ObjectRef{Atespace: tagAtespace, Name: tagName},
		Status: &ateapipb.ActorStatus{
			State:     ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			Snapshots: []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/snapshot-1"}}}},
		},
	}
	a.actors[actorKey(atespace, name)] = actor
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) ResumeActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) SuspendActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.Snapshots = []*ateapipb.Snapshot{{Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, ActorTemplateUid: "template-uid", DurableSnapshot: &ateapipb.SnapshotStorage{Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED, Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/snapshot-1"}}}}
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) DeleteActor(_ context.Context, atespace, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.actors, actorKey(atespace, name))
	return nil
}

func TestQuiesceRejectsWrongActorIdentity(t *testing.T) {
	session := &apiv1.Session{Id: "session-1", PreparedRevision: "revision-1"}
	store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a"}}
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{
		actorKey("team-a", substrate.ActorName(session.Id)): {
			Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "different-actor", Uid: "actor-uid"},
			Status:   &ateapipb.ActorStatus{},
		},
	}}
	if _, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Quiesce(t.Context(), session); err == nil {
		t.Fatal("Quiesce() accepted the wrong Actor")
	}
}

type snapshotTestActors struct {
	*lifecycleTestActors
	snapshots []*ateapipb.Snapshot
}

func (a *snapshotTestActors) SuspendActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	actor, err := a.lifecycleTestActors.SuspendActor(ctx, atespace, name)
	if err == nil {
		actor.Status.Snapshots = a.snapshots
	}
	return actor, err
}

func TestQuiesceRequiresDurableVolumesSnapshot(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &snapshotTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	created, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)

	for _, fidelity := range []ateapipb.SnapshotFidelity{
		ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_UNSPECIFIED,
		ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
		ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS,
		ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY,
	} {
		t.Run(fidelity.String(), func(t *testing.T) {
			actors.snapshots = []*ateapipb.Snapshot{{Generation: 2, Fidelity: fidelity,
				DurableSnapshot: &ateapipb.SnapshotStorage{
					Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED,
					Object: &ateapipb.ObjectSnapshot{SnapshotUri: "s3://snapshots/new"},
				}}}
			boundary, err := workflow.Quiesce(t.Context(), created)
			if fidelity != ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES {
				require.ErrorContains(t, err, "invalid snapshot fidelity")
				require.Nil(t, boundary)
				return
			}
			require.NoError(t, err)
			require.Equal(t, fidelity.String(), boundary.Fidelity)
			require.Equal(t, "s3://snapshots/new", boundary.URI)

			older := proto.CloneOf(actors.snapshots[0])
			older.Generation = 1
			actors.snapshots[0].DurableSnapshot.Status = ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_PENDING
			actors.snapshots = append(actors.snapshots, older)
			_, err = workflow.Quiesce(t.Context(), created)
			require.ErrorContains(t, err, "returned no snapshot", "a previous durable snapshot cannot cover the current boundary")
		})
	}
}

// lifecycleForkFixture retains a real checkpoint and its independent fork history.
func lifecycleForkFixture(t *testing.T, store *lifecycleTestStore, actors *lifecycleTestActors, source *apiv1.Session) (*apiv1.Session, string) {
	t.Helper()
	source, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), source)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = source.ContextId
	task := a2a.NewSubmittedTask(message, message)
	createHash := sha256.Sum256([]byte("fixture-create"))
	initialVersion, err := store.CreateRuntimeTask(t.Context(), source.Id, createHash[:], task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte("fixture-complete"))
	version, err := store.UpdateSessionTask(t.Context(), source.Id, initialVersion, hash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), source.Id, string(task.ID), version))
	boundary, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), boundary,
		&database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	checkpoint, _, err := store.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: string(task.ID)}, source.Creator, uuid.NewString())
	require.NoError(t, err)
	_, err = store.FinalizeSessionCheckpoint(t.Context(), checkpoint.Id, "tag-uid", "s3://snapshots/snapshot-1", "")
	require.NoError(t, err)
	requestID := uuid.NewString()
	fork, _, err := store.ForkSession(t.Context(), checkpoint.Id, source.Creator, requestID, uuid.NewString())
	require.NoError(t, err)
	// An ordinary Create must not reuse a fork request ID, even for the same pair.
	_, _, err = store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: source.Creator, Agent: source.Agent, Name: fork.Name}, requestID)
	require.ErrorIs(t, err, database.ErrIdempotencyConflict)
	return fork, checkpoint.Id
}

func (a *lifecycleTestActors) EnsureActorEgressPolicy(_ context.Context, atespace, name string, revision string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.policyActor = actorKey(atespace, name)
	a.policyRevision = revision
	a.policyCalls++
	return a.policyErr
}

func TestActorCreationRetainsEgressPolicyFailure(t *testing.T) {
	for _, name := range []string{"create", "fork"} {
		t.Run(name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			if name == "fork" {
				session, _ = lifecycleForkFixture(t, store, base, session)
			}
			actors := &retryTestActors{lifecycleTestActors: base}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			callsBefore := base.policyCalls
			base.policyErr = context.DeadlineExceeded
			_, err := workflow.Create(t.Context(), session)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			current, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_CREATING, current.State)
			require.Empty(t, current.A2AAuthority)
			require.Equal(t, actorKey("team-a", substrate.ActorName(session.Id)), base.policyActor)
			require.Equal(t, session.PreparedRevision, base.policyRevision)

			// Retry completes policy setup for the existing Actor before readiness.
			base.policyErr = nil
			ready, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, ready.State)
			require.EqualValues(t, 1, actors.mutations.Load())
			require.Equal(t, callsBefore+2, base.policyCalls)
		})
	}
}

func TestActorEgressPolicy(t *testing.T) {
	policy, err := substrate.ActorEgressPolicy("team-a", []string{"https://API.Example.com.", "https://api.example.com:443", "https://api.example.com:0443", "http://api.example.com:8080"}, nil)
	require.NoError(t, err)
	require.Equal(t, &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, policy.Metadata)
	require.Len(t, policy.Rules, 2)
	require.Equal(t, []string{"api.example.com"}, policy.Rules[0].GetHttp().GetHostnames())
	require.Equal(t, []int32{8080}, policy.Rules[0].GetHttp().GetPorts().GetNumbers())
	require.Equal(t, []string{"api.example.com"}, policy.Rules[1].GetHttps().GetHostnames())
	require.Equal(t, []int32{443}, policy.Rules[1].GetHttps().GetPorts().GetNumbers())
	policy, err = substrate.ActorEgressPolicy("team-a", nil, nil)
	require.NoError(t, err)
	require.Empty(t, policy.Rules, "no destinations must deny all egress")
	for _, destination := range []string{"", "*", "api.example.com", "https://api.example.com/path", "http://api.example.com:0", "http://api.example.com:65536", "http://user@api.example.com", "http://api.example.com?key=value"} {
		t.Run(destination, func(t *testing.T) {
			_, err := substrate.ActorEgressPolicy("team-a", []string{destination}, nil)
			require.Error(t, err)
		})
	}
	for _, destination := range []string{"http://192.0.2.1", "http://[2001:db8::1]", "http://[::ffff:192.0.2.1]"} {
		_, err := substrate.ActorEgressPolicy("team-a", []string{destination}, nil)
		require.ErrorContains(t, err, "expected a DNS name")
	}
	_, err = substrate.ActorEgressPolicy("team-a", []string{"http://api.example.com:8443", "https://api.example.com:8443"}, nil)
	require.ErrorContains(t, err, "both HTTP and HTTPS on the same port")
}

func TestActorEgressCredentialsRequireAllowedDestination(t *testing.T) {
	bindings := []egress.Credential{
		{Origin: "https://api.example.com:8443", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/auth/token"},
		{Origin: "https://api.example.com:8443", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team/auth/key"},
	}
	_, err := substrate.ActorEgressPolicy("team", []string{"https://other.example.com"}, bindings)
	require.ErrorContains(t, err, "is not explicitly allowed")
	policy, err := substrate.ActorEgressPolicy("team", []string{"http://api.example.com:8080", "https://api.example.com:8443", "https://other.example.com"}, bindings)
	require.NoError(t, err)
	require.Len(t, policy.Rules, 3)
	require.Empty(t, policy.Rules[0].GetHttp().GetEffects().GetReplaceHeaders(), "credentials must not cross schemes or ports")
	require.Equal(t, []string{"api.example.com"}, policy.Rules[1].GetHttps().GetHostnames())
	require.Equal(t, []int32{8443}, policy.Rules[1].GetHttps().GetPorts().GetNumbers())
	require.Len(t, policy.Rules[1].GetHttps().GetEffects().GetReplaceHeaders(), 2)
	require.Equal(t, []string{"other.example.com"}, policy.Rules[2].GetHttps().GetHostnames(), "no other HTTPS rule may bypass credential replacement")
}

func TestServiceLifecycleRetriesUseCurrentStateAndRespectDeletion(t *testing.T) {
	store, fixture := lifecycleFixture(t)
	actors := &retryTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	service := NewService(store, serviceTestAuthorizer{}, NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second))
	ctx := serviceTestContext("alice")
	session, err := service.Create(ctx, fixture.Agent, "retry-request", "conversation")
	require.NoError(t, err)
	suspended, err := service.Suspend(ctx, session.Id)
	require.NoError(t, err)
	mutations := actors.mutations.Load()
	retried, err := service.Create(ctx, fixture.Agent, "retry-request", "ignored retry name")
	require.NoError(t, err)
	require.Equal(t, suspended.Id, retried.Id)
	require.Equal(t, suspended.State, retried.State)
	require.Equal(t, mutations, actors.mutations.Load(), "creation retry cannot reissue runtime work")
	deleted, err := service.Delete(ctx, session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	require.Empty(t, deleted.A2AAuthority)
	mutations = actors.mutations.Load()
	_, err = service.Create(ctx, fixture.Agent, "retry-request", "")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
	_, err = service.Get(ctx, session.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = service.Delete(ctx, session.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = service.Resume(ctx, session.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	require.Equal(t, mutations, actors.mutations.Load(), "a tombstoned request must not create or touch compute")
}

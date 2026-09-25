package agentinstance

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestActorWorkflowLifecycle(t *testing.T) {
	store, instance := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors)

	created, err := workflow.Create(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || created.GetA2AAuthority() == "" {
		t.Fatalf("created instance = %+v", created)
	}
	if len(actors.actors) != 1 {
		t.Fatalf("actors = %v", actors.actors)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("created Actor status = %s", actor.GetStatus().GetState())
	}
	actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))].Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	if err := workflow.Pause(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		t.Fatalf("paused Actor status = %s", actor.GetStatus().GetState())
	}
	boundary, err := workflow.Quiesce(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || boundary.URI != "s3://snapshots/snapshot-1" {
		t.Fatalf("quiesced instance = %+v, boundary = %+v", created, boundary)
	}

	suspended, err := workflow.Suspend(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED || suspended.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		t.Fatalf("suspended instance = %+v", suspended)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("suspended Actor status = %s", actor.GetStatus().GetState())
	}

	resumed, err := workflow.Resume(context.Background(), suspended)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || resumed.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		t.Fatalf("resumed instance = %+v", resumed)
	}

	deleted, err := workflow.Delete(context.Background(), resumed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GetAgentInstanceByID(t.Context(), instance.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	if deleted.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED || len(actors.actors) != 0 {
		t.Fatalf("deleted instance = %+v, actors = %v", deleted, actors.actors)
	}
}

func TestActorWorkflowRejectsReplacedRuntime(t *testing.T) {
	for _, operation := range []string{"pause", "quiesce", "suspend", "delete"} {
		t.Run(operation, func(t *testing.T) {
			store, instance := lifecycleFixture(t)
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			workflow := NewActorWorkflow(store, actors)
			instance, err := workflow.Create(t.Context(), instance)
			require.NoError(t, err)
			actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.Id))]
			actor.Metadata.Uid = "replacement-uid"
			actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
			switch operation {
			case "pause":
				err = workflow.Pause(t.Context(), instance)
			case "quiesce":
				_, err = workflow.Quiesce(t.Context(), instance)
			case "suspend":
				_, err = workflow.Suspend(t.Context(), instance)
			case "delete":
				_, err = workflow.Delete(t.Context(), instance)
			}
			require.ErrorContains(t, err, "verify runtime actor UID")
			require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, actor.Status.State)
			require.Len(t, actors.actors, 1)
		})
	}
}

func TestActorWorkflowForkCreatesSuspendedActorFromCheckpoint(t *testing.T) {
	store, instance := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	instance, checkpointID := lifecycleForkFixture(t, store, actors, instance)
	fork, err := NewActorWorkflow(store, actors).Create(t.Context(), instance)
	if err != nil {
		t.Fatal(err)
	}
	actor := actors.actors[actorKey("team-a", substrate.ActorName(instance.GetId()))]
	if fork.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY ||
		actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED ||
		actor.GetSourceTag().GetName() != "checkpoint-"+checkpointID {
		t.Fatalf("fork = %+v, actor = %+v", fork, actor)
	}
	actor.Status.ExternalSnapshot.SnapshotUri = "s3://snapshots/later-turn"
	replayed, err := NewActorWorkflow(store, actors).Create(t.Context(), instance)
	require.NoError(t, err)
	require.True(t, proto.Equal(fork, replayed), "a retry returns the current instance without revalidating later Actor state")
}

// lifecycleFixture uses the same persistence boundary as production; only Actor
// calls are faked, so concurrency assertions exercise PostgreSQL admission.
func lifecycleFixture(t *testing.T) (*lifecycleTestStore, *apiv1alpha1.AgentInstance) {
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
	client := database.NewClient(pool)
	revision := &database.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		SourceSnapshot: []byte("{}"),
		AgentCard:      &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision", ActorTemplateUID: "actor-template-uid",
	}
	require.NoError(t, client.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid", DesiredRevision: revision.Revision}))
	require.NoError(t, client.RecordRuntimeRevision(t.Context(), *revision, true))
	instance, _, err := client.CreateAgentInstance(t.Context(), &apiv1alpha1.AgentInstance{Id: uuid.NewString(), Creator: "alice", Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}}, uuid.NewString())
	require.NoError(t, err)
	return &lifecycleTestStore{Client: client, revision: revision}, instance
}

type lifecycleTestStore struct {
	*database.Client
	revision *database.RuntimeRevision
}

func (s *lifecycleTestStore) GetRuntimeRevision(context.Context, string) (*database.RuntimeRevision, error) {
	return s.revision, nil
}

type lifecycleTestActors struct {
	mu          sync.Mutex
	actors      map[string]*ateapipb.Actor
	policyErr   error
	policy      *ateapipb.EgressPolicy
	policyActor string
	policyCalls int
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
			State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
			ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/snapshot-1", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA},
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

func (a *lifecycleTestActors) PauseActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_PAUSED
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) SuspendActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/snapshot-1", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) DeleteActor(_ context.Context, atespace, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.actors, actorKey(atespace, name))
	return nil
}

func TestQuiesceRejectsWrongActorIdentity(t *testing.T) {
	instance := &apiv1alpha1.AgentInstance{Id: "instance-1", PreparedRevision: "revision-1"}
	store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a"}}
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{
		actorKey("team-a", substrate.ActorName(instance.Id)): {
			Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "different-actor", Uid: "actor-uid"},
			Status:   &ateapipb.ActorStatus{},
		},
	}}
	if _, err := NewActorWorkflow(store, actors).Quiesce(t.Context(), instance); err == nil {
		t.Fatal("Quiesce() accepted the wrong Actor")
	}
}

// lifecycleForkFixture retains a real checkpoint and its independent fork history.
func lifecycleForkFixture(t *testing.T, store *lifecycleTestStore, actors *lifecycleTestActors, source *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, string) {
	t.Helper()
	source, err := NewActorWorkflow(store, actors).Create(t.Context(), source)
	require.NoError(t, err)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ContextID = source.ContextId
	task := a2a.NewSubmittedTask(message, message)
	createHash := sha256.Sum256([]byte("fixture-create"))
	initialVersion, err := store.CreateRuntimeTask(t.Context(), source.Id, createHash[:], task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte("fixture-complete"))
	version, err := store.UpdateAgentInstanceTask(t.Context(), source.Id, initialVersion, hash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleAgentInstanceTask(t.Context(), source.Id, string(task.ID), version))
	boundary, err := store.ClaimInstanceQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.FinishInstanceQuiescence(t.Context(), boundary,
		&database.AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source", ContentScope: "DATA"}))
	checkpoint, _, err := store.ReserveAgentInstanceCheckpoint(t.Context(), &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: source.Id, HeadTaskId: string(task.ID)}, source.Creator, uuid.NewString())
	require.NoError(t, err)
	_, err = store.FinalizeAgentInstanceCheckpoint(t.Context(), checkpoint.Id, "tag-uid", "s3://snapshots/snapshot-1", "")
	require.NoError(t, err)
	requestID := uuid.NewString()
	fork, _, err := store.ForkAgentInstance(t.Context(), checkpoint.Id, source.Creator, requestID, uuid.NewString())
	require.NoError(t, err)
	// An ordinary Create must not reuse a fork request ID, even for the same pair.
	_, _, err = store.CreateAgentInstance(t.Context(), &apiv1alpha1.AgentInstance{Id: uuid.NewString(), Creator: source.Creator, Agent: source.Agent, Name: fork.Name}, requestID)
	require.ErrorIs(t, err, database.ErrIdempotencyConflict)
	return fork, checkpoint.Id
}

func (a *lifecycleTestActors) EnsureActorEgressPolicy(_ context.Context, atespace, name string, policy *ateapipb.EgressPolicy) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.policyActor = actorKey(atespace, name)
	a.policy = proto.CloneOf(policy)
	a.policyCalls++
	return a.policyErr
}

func TestActorCreationRetainsEgressPolicyFailure(t *testing.T) {
	for _, name := range []string{"create", "fork"} {
		t.Run(name, func(t *testing.T) {
			store, instance := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			if name == "fork" {
				instance, _ = lifecycleForkFixture(t, store, base, instance)
			}
			actors := &retryTestActors{lifecycleTestActors: base}
			workflow := NewActorWorkflow(store, actors)
			callsBefore := base.policyCalls
			store.revision.EgressDestinations = []string{"*"}
			_, err := workflow.Create(t.Context(), instance)
			require.ErrorContains(t, err, "invalid egress destination")
			require.Zero(t, actors.mutations.Load(), "validate the allowlist before issuing Actor creation")
			require.Equal(t, callsBefore, base.policyCalls)

			store.revision.EgressDestinations = []string{"api.example.com", "192.0.2.1"}
			store.revision.Credentials = []egress.Credential{{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://kubernetes.io/team-a/auth/token"}}
			base.policyErr = context.DeadlineExceeded
			_, err = workflow.Create(t.Context(), instance)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			current, err := store.GetAgentInstanceByID(t.Context(), instance.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING, current.State)
			require.Empty(t, current.A2AAuthority)
			require.Equal(t, actorKey("team-a", substrate.ActorName(instance.Id)), base.policyActor)
			require.Equal(t, &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, base.policy.Metadata)
			require.Len(t, base.policy.Rules, 3)
			require.Equal(t, &ateapipb.CredentialHeaderInjection{Header: "authorization", Prefix: "Bearer ", CredentialUri: "ate-secret://kubernetes.io/team-a/auth/token"}, base.policy.Rules[0].GetHostnames().GetEffects().GetInjectStaticHeaders()[0])
			require.Equal(t, []string{"api.example.com"}, base.policy.Rules[0].GetHostnames().GetPatterns())
			require.Equal(t, []string{"192.0.2.1/32"}, base.policy.Rules[2].GetCidrs().GetCidrs())

			// The Actor was created, but policy setup may still be running.
			// Another caller must neither repeat effects nor publish readiness.
			base.policyErr = nil
			_, err = workflow.Create(t.Context(), instance)
			require.ErrorIs(t, err, database.ErrConflict)
			_, err = workflow.Delete(t.Context(), instance)
			require.ErrorIs(t, err, database.ErrConflict)
			require.EqualValues(t, 1, actors.mutations.Load())
			require.Equal(t, callsBefore+1, base.policyCalls)
		})
	}
}

func TestActorEgressPolicy(t *testing.T) {
	policy, err := actorEgressPolicy("team-a", []string{"API.Example.com.", "api.example.com", "192.0.2.1", "2001:db8::1", "::ffff:192.0.2.1"}, nil)
	require.NoError(t, err)
	require.Equal(t, &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, policy.Metadata)
	require.Len(t, policy.Rules, 2)
	require.Equal(t, []string{"api.example.com"}, policy.Rules[0].GetHostnames().GetPatterns())
	require.Equal(t, []string{"192.0.2.1/32", "2001:db8::1/128"}, policy.Rules[1].GetCidrs().GetCidrs())
	policy, err = actorEgressPolicy("team-a", nil, nil)
	require.NoError(t, err)
	require.Empty(t, policy.Rules, "no destinations must deny all egress")
	for _, destination := range []string{"", "*", "https://api.example.com", "api.example.com:443", "192.0.2.0/24", "fe80::1%eth0"} {
		t.Run(destination, func(t *testing.T) {
			_, err := actorEgressPolicy("team-a", []string{destination}, nil)
			require.Error(t, err)
		})
	}
}

func TestActorEgressCredentialsRequireAllowedDestination(t *testing.T) {
	bindings := []egress.Credential{
		{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://kubernetes.io/team/auth/token"},
		{Hostname: "api.example.com", Header: "x-api-key", URI: "ate-secret://kubernetes.io/team/auth/key"},
	}
	_, err := actorEgressPolicy("team", []string{"other.example.com"}, bindings)
	require.ErrorContains(t, err, "is not allowed")
	policy, err := actorEgressPolicy("team", []string{"api.example.com", "other.example.com"}, bindings)
	require.NoError(t, err)
	require.Len(t, policy.Rules, 2)
	require.Equal(t, []string{"api.example.com"}, policy.Rules[0].GetHostnames().GetPatterns())
	require.Len(t, policy.Rules[0].GetHostnames().GetEffects().GetInjectStaticHeaders(), 2)
	require.Nil(t, policy.Rules[1].GetHostnames().GetEffects(), "the broad allow rule must not bypass injection")
}

func TestServiceLifecycleRetriesUseCurrentStateAndRespectDeletion(t *testing.T) {
	store, fixture := lifecycleFixture(t)
	actors := &retryTestActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}}
	service := NewService(store, serviceTestAuthorizer{}, NewActorWorkflow(store, actors))
	ctx := serviceTestContext("alice")
	instance, err := service.Create(ctx, fixture.Agent, "retry-request", "conversation")
	require.NoError(t, err)
	suspended, err := service.Suspend(ctx, instance.Id)
	require.NoError(t, err)
	mutations := actors.mutations.Load()
	retried, err := service.Create(ctx, fixture.Agent, "retry-request", "ignored retry name")
	require.NoError(t, err)
	require.Equal(t, suspended.Id, retried.Id)
	require.Equal(t, suspended.State, retried.State)
	require.Equal(t, mutations, actors.mutations.Load(), "creation retry cannot reissue runtime work")
	deleted, err := service.Delete(ctx, instance.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED, deleted.State)
	require.Empty(t, deleted.A2AAuthority)
	mutations = actors.mutations.Load()
	_, err = service.Create(ctx, fixture.Agent, "retry-request", "")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
	_, err = service.Get(ctx, instance.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = service.Delete(ctx, instance.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = service.Resume(ctx, instance.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	require.Equal(t, mutations, actors.mutations.Load(), "a tombstoned request must not create or touch compute")
}

package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

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
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	directories := &recordingDirectoryReleaser{}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, WithDirectoryReleaser(directories))

	created, err := workflow.Create(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	require.Nil(t, actors.existing[actorKey("team-a", substrate.ActorName(session.GetId()))], "a session without a volume source supplies no existing volume")
	if created.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY || created.GetA2AAuthority() == "" {
		t.Fatalf("created session = %+v", created)
	}
	if len(actors.actors) != 1 {
		t.Fatalf("actors = %v", actors.actors)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("created Actor status = %s", actor.GetStatus().GetState())
	}
	actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))].Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	if err := workflow.Pause(context.Background(), created); err != nil {
		t.Fatal(err)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_PAUSED {
		t.Fatalf("paused Actor status = %s", actor.GetStatus().GetState())
	}
	boundary, err := workflow.Quiesce(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if created.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY || boundary.URI != "s3://snapshots/snapshot-1" {
		t.Fatalf("quiesced session = %+v, boundary = %+v", created, boundary)
	}

	suspended, err := workflow.Suspend(context.Background(), created)
	if err != nil {
		t.Fatal(err)
	}
	if suspended.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED || suspended.GetOperation() != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		t.Fatalf("suspended session = %+v", suspended)
	}
	if actor := actors.actors[actorKey("team-a", substrate.ActorName(session.GetId()))]; actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Fatalf("suspended Actor status = %s", actor.GetStatus().GetState())
	}

	resumed, err := workflow.Resume(context.Background(), suspended)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY || resumed.GetOperation() != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		t.Fatalf("resumed session = %+v", resumed)
	}

	deleted, err := workflow.Delete(context.Background(), resumed)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, database.ErrNotFound)
	if deleted.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED || len(actors.actors) != 0 {
		t.Fatalf("deleted session = %+v, actors = %v", deleted, actors.actors)
	}
	require.Empty(t, directories.released, "a session without a volume source releases no directory")
}

// recordingDirectoryReleaser records the sessions whose directory it was told
// may go.
type recordingDirectoryReleaser struct {
	mu       sync.Mutex
	released []*apiv1alpha1.Session
}

func (r *recordingDirectoryReleaser) ReleaseSessionDirectory(_ context.Context, session *apiv1alpha1.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, session)
	return nil
}

// A session on a workspace volume gets an Actor that supplies its own
// directory and the mirrors for the template's workspace volumes; suspend and
// resume keep them, and deleting the session tells the volume's owner that its
// directory may go, while the volume stays.
func TestActorWorkflowMountsTheSessionsWorkspaceDirectory(t *testing.T) {
	source := &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "nfs.csi.k8s.io", VolumeHandle: "nfs-server#share#workspace##"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: substrate.WorkspaceMountPath},
			{SubPath: "mirrors", MountPath: substrate.MirrorsMountPath, ReadOnly: true},
		},
	}
	store, session := lifecycleFixtureWith(t, source)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	directories := &recordingDirectoryReleaser{}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, WithDirectoryReleaser(directories))
	key := actorKey("team-a", substrate.ActorName(session.GetId()))

	created, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	want, err := substrate.SessionExistingVolumes(session.GetId(), source)
	require.NoError(t, err)
	got := actors.existing[key]
	require.Len(t, got, len(want))
	for i := range want {
		require.True(t, proto.Equal(want[i], got[i]), "existing volume %d = %v", i, got[i])
	}
	require.Equal(t, "sessions/"+session.GetId(), got[0].GetSubPath())

	suspended, err := workflow.Suspend(t.Context(), created)
	require.NoError(t, err)
	resumed, err := workflow.Resume(t.Context(), suspended)
	require.NoError(t, err)
	require.Len(t, actors.existing, 1, "suspend and resume create no other Actor")
	require.Empty(t, directories.released)

	_, err = workflow.Delete(t.Context(), resumed)
	require.NoError(t, err)
	require.Len(t, directories.released, 1)
	require.Equal(t, session.GetId(), directories.released[0].GetId())
	require.True(t, proto.Equal(source, directories.released[0].GetVolumeSource()))
}

func TestActorWorkflowRejectsReplacedRuntime(t *testing.T) {
	for _, operation := range []string{"pause", "quiesce", "suspend", "delete"} {
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
			case "pause":
				err = workflow.Pause(t.Context(), session)
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
	if fork.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY ||
		actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED ||
		actor.GetSourceTag().GetName() != "checkpoint-"+checkpointID {
		t.Fatalf("fork = %+v, actor = %+v", fork, actor)
	}
	actor.Status.ExternalSnapshot.SnapshotUri = "s3://snapshots/later-turn"
	replayed, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	require.True(t, proto.Equal(fork, replayed), "a retry returns the current session without revalidating later Actor state")
}

// lifecycleFixture uses the same persistence boundary as production; only Actor
// calls are faked, so concurrency assertions exercise PostgreSQL admission.
func lifecycleFixture(t *testing.T) (*lifecycleTestStore, *apiv1alpha1.Session) {
	t.Helper()
	return lifecycleFixtureWith(t, nil)
}

// lifecycleFixtureWith creates the session on source, a workspace volume, or
// without one when source is nil.
func lifecycleFixtureWith(t *testing.T, source *apiv1alpha1.SessionVolumeSource) (*lifecycleTestStore, *apiv1alpha1.Session) {
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
	require.NoError(t, client.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid", DesiredRevision: revision.Revision}))
	require.NoError(t, client.RecordRuntimeRevision(t.Context(), *revision, true))
	session, _, err := client.CreateSession(t.Context(), &apiv1alpha1.Session{Id: uuid.NewString(), Creator: "alice", Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, VolumeSource: source}, uuid.NewString())
	require.NoError(t, err)
	return &lifecycleTestStore{Client: client, pool: pool, revision: revision}, session
}

type lifecycleTestStore struct {
	*database.Client
	pool     *pgxpool.Pool
	revision *database.RuntimeRevision
	// untouched answers HasSessionTasks for a store without a database: the
	// session holds no task. Without it the session holds one.
	untouched bool
}

// HasSessionTasks reads the fixture's database, or answers untouched when
// the store has none.
func (s *lifecycleTestStore) HasSessionTasks(ctx context.Context, sessionID string) (bool, error) {
	if s.Client == nil {
		return !s.untouched, nil
	}
	return s.Client.HasSessionTasks(ctx, sessionID)
}

// GetRuntimeRevision answers the fixture's revision; another revision the
// fixture's database knows, one a session moved to, is read from there.
func (s *lifecycleTestStore) GetRuntimeRevision(ctx context.Context, revision string) (*database.RuntimeRevision, error) {
	if s.Client != nil && s.revision.Revision != "" && revision != s.revision.Revision {
		return s.Client.GetRuntimeRevision(ctx, revision)
	}
	return s.revision, nil
}

type lifecycleTestActors struct {
	mu               sync.Mutex
	actors           map[string]*ateapipb.Actor
	workers          []*ateapipb.Worker
	workersErr       error
	getErr           error
	policyErr        error
	policy           *ateapipb.EgressPolicy
	inherited        *ateapipb.EgressPolicy
	policyActor      string
	policyCalls      int
	repointErr       error
	repointCalls     int
	revertErr        error
	reverts          int
	replacedPolicies int
	// existing records the existing volumes each Actor was created with.
	existing map[string][]*ateapipb.ExistingVolume
}

// RepointActor moves a suspended Actor onto another template as Substrate
// does: an Actor already there is returned as it is, a live one is refused.
func (a *lifecycleTestActors) RepointActor(_ context.Context, atespace, name, templateAtespace, templateName string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.repointErr != nil {
		return nil, a.repointErr
	}
	actor := a.actors[actorKey(atespace, name)]
	if actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	if template := actor.GetActorTemplate(); template.GetAtespace() == templateAtespace && template.GetName() == templateName {
		return proto.CloneOf(actor), nil
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		return nil, status.Error(codes.FailedPrecondition, "actor is not suspended")
	}
	actor.ActorTemplate = &ateapipb.ObjectRef{Atespace: templateAtespace, Name: templateName}
	a.repointCalls++
	return proto.CloneOf(actor), nil
}

// RevertActor returns a crashed, running or paused Actor to SUSPENDED at the
// snapshot it holds, as Substrate does.
func (a *lifecycleTestActors) RevertActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.revertErr != nil {
		return nil, a.revertErr
	}
	actor := a.actors[actorKey(atespace, name)]
	if actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	switch actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_CRASHED, ateapipb.ActorState_ACTOR_STATE_RUNNING, ateapipb.ActorState_ACTOR_STATE_PAUSED:
	default:
		return nil, status.Error(codes.FailedPrecondition, "actor is not in a revertable state")
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.Crash = nil
	a.reverts++
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) ReplaceActorEgressPolicy(_ context.Context, atespace, name string, policy *ateapipb.EgressPolicy) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.policyActor = actorKey(atespace, name)
	a.policy = proto.CloneOf(policy)
	a.replacedPolicies++
	return a.policyErr
}

func actorKey(atespace, name string) string { return atespace + "/" + name }

func (a *lifecycleTestActors) GetActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.getErr != nil {
		return nil, a.getErr
	}
	actor := a.actors[actorKey(atespace, name)]
	if actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) CreateActor(_ context.Context, atespace, name, templateNamespace, templateName string, existing []*ateapipb.ExistingVolume) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.existing == nil {
		a.existing = map[string][]*ateapipb.ExistingVolume{}
	}
	a.existing[actorKey(atespace, name)] = existing
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

func (a *lifecycleTestActors) ResumeActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	if err := fenceActor(ctx, actor); err != nil {
		return nil, err
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) PauseActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	if err := fenceActor(ctx, actor); err != nil {
		return nil, err
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_PAUSED
	return proto.CloneOf(actor), nil
}

func (a *lifecycleTestActors) SuspendActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	actor := a.actors[actorKey(atespace, name)]
	if err := fenceActor(ctx, actor); err != nil {
		return nil, err
	}
	actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	actor.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/snapshot-1", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA}
	return proto.CloneOf(actor), nil
}

// fenceActor checks a request's fencing token as Substrate does: an older
// token, or the same generation from another holder, is refused; a newer one
// is recorded; a request without one is admitted.
func fenceActor(ctx context.Context, actor *ateapipb.Actor) error {
	token := substrate.FencingTokenFrom(ctx)
	if token == nil {
		return nil
	}
	if recorded := actor.GetStatus().GetFencingToken(); recorded != nil {
		if token.GetGeneration() < recorded.GetGeneration() || (token.GetGeneration() == recorded.GetGeneration() && token.GetHolder() != recorded.GetHolder()) {
			return status.Errorf(codes.FailedPrecondition, "fencing token %s/%d is older than %s/%d", token.GetHolder(), token.GetGeneration(), recorded.GetHolder(), recorded.GetGeneration())
		}
	}
	actor.Status.FencingToken = proto.CloneOf(token)
	return nil
}

func (a *lifecycleTestActors) ListAllWorkers(context.Context) ([]*ateapipb.Worker, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.workers, a.workersErr
}

func (a *lifecycleTestActors) DeleteActor(_ context.Context, atespace, name string, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.actors, actorKey(atespace, name))
	return nil
}

// supersedingRevision re-renders the fixture's agent: revision-2, on another
// ActorTemplate with another allowlist, becomes its current revision.
func supersedingRevision(t *testing.T, store *lifecycleTestStore) *database.RuntimeRevision {
	t.Helper()
	current := database.RuntimeRevision{
		Revision: "revision-2", Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{"https://api.example"},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision-2", ActorTemplateUID: "actor-template-uid-2",
	}
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid", DesiredRevision: current.Revision}))
	require.NoError(t, store.RecordRuntimeRevision(t.Context(), current, true))
	return &current
}

func TestRepointQuiescedMovesASuspendedActorOntoTheCurrentRevision(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	name := substrate.ActorName(session.Id)

	unchanged, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, "revision-1", unchanged.GetPreparedRevision(), "a session on its agent's current revision stays")
	require.Equal(t, 0, actors.repointCalls)

	current := supersedingRevision(t, store)
	moved, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, current.Revision, moved.GetPreparedRevision())
	actor, err := actors.GetActor(t.Context(), "team-a", name)
	require.NoError(t, err)
	require.Equal(t, current.ActorTemplateName, actor.GetActorTemplate().GetName(), "the Actor runs on the current revision's template")
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, actor.GetStatus().GetState(), "and stays suspended until a turn wakes it")
	require.Equal(t, actorKey("team-a", name), actors.policyActor)
	require.Equal(t, 1, actors.replacedPolicies, "the Actor takes the current revision's allowlist")
	require.NotEmpty(t, actors.policy.GetRules())
	stored, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, current.Revision, stored.GetPreparedRevision(), "the session is recorded on the current revision")

	again, err := workflow.RepointQuiesced(t.Context(), moved)
	require.NoError(t, err)
	require.Equal(t, current.Revision, again.GetPreparedRevision())
	require.Equal(t, 1, actors.repointCalls, "a moved Actor is left as it is")

	// The gateway, holding a session read before the sweep moved it, finds the
	// Actor moved and records nothing new.
	stale, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, current.Revision, stale.GetPreparedRevision())
	require.Equal(t, 1, actors.repointCalls)
}

func TestRepointQuiescedLeavesARefusedOrLiveActor(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	supersedingRevision(t, store)

	actors.repointErr = status.Error(codes.FailedPrecondition, "volume layout differs")
	refused, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err, "a template Substrate refuses is not an error")
	require.Equal(t, "revision-1", refused.GetPreparedRevision(), "the session stays on its prepared revision")
	require.Equal(t, 0, actors.replacedPolicies)

	actors.repointErr = nil
	running, err := actors.ResumeActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, running.GetStatus().GetState())
	live, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, "revision-1", live.GetPreparedRevision(), "a live Actor is never moved")
	require.Equal(t, 0, actors.repointCalls)
	stored, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, "revision-1", stored.GetPreparedRevision())
}

// claimedStore answers every repoint record as a lifecycle operation that
// claimed the session first, with the session still on its revision.
type claimedStore struct {
	*lifecycleTestStore
}

func (s *claimedStore) RepointSession(ctx context.Context, sessionID, _, _ string) (*apiv1alpha1.Session, error) {
	session, err := s.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return session, fmt.Errorf("claimed: %w", database.ErrConflict)
}

func TestRepointQuiescedGoesBackWhenALifecycleOperationClaimedTheSession(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	session, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	supersedingRevision(t, store)

	_, err = NewActorWorkflow(&claimedStore{store}, actors, make(chan struct{}, 1), time.Second).RepointQuiesced(t.Context(), session)
	require.ErrorIs(t, err, database.ErrConflict)
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, store.revision.ActorTemplateName, actor.GetActorTemplate().GetName(), "the Actor goes back to the template of the revision the session records")
	require.Equal(t, 2, actors.repointCalls)
	require.Equal(t, 2, actors.replacedPolicies, "with that revision's allowlist")
	require.Empty(t, actors.policy.GetRules())
}

func TestResumeMovesASuspendedSessionOntoTheCurrentRevision(t *testing.T) {
	store, session := lifecycleFixture(t)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	suspended, err := workflow.Suspend(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, suspended.GetState())
	current := supersedingRevision(t, store)

	resumed, err := workflow.Resume(t.Context(), suspended)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, resumed.GetState())
	require.Equal(t, current.Revision, resumed.GetPreparedRevision(), "the session resumed on its agent's current revision")
	actor, err := actors.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
	require.NoError(t, err)
	require.Equal(t, current.ActorTemplateName, actor.GetActorTemplate().GetName())
	require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, actor.GetStatus().GetState())
	require.Equal(t, 1, actors.replacedPolicies)

	// A later resume of the same session finds nothing to move.
	suspended, err = workflow.Suspend(t.Context(), resumed)
	require.NoError(t, err)
	resumed, err = workflow.Resume(t.Context(), suspended)
	require.NoError(t, err)
	require.Equal(t, current.Revision, resumed.GetPreparedRevision())
	require.Equal(t, 1, actors.repointCalls)
}

func TestQuiesceRejectsWrongActorIdentity(t *testing.T) {
	session := &apiv1alpha1.Session{Id: "session-1", PreparedRevision: "revision-1"}
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

// lifecycleForkFixture retains a real checkpoint and its independent fork history.
func lifecycleForkFixture(t *testing.T, store *lifecycleTestStore, actors *lifecycleTestActors, source *apiv1alpha1.Session) (*apiv1alpha1.Session, string) {
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
	boundary, err := store.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.NoError(t, err)
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), boundary,
		&database.SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source", ContentScope: "DATA"}))
	checkpoint, _, err := store.ReserveSessionCheckpoint(t.Context(), &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: string(task.ID)}, source.Creator, uuid.NewString())
	require.NoError(t, err)
	_, err = store.FinalizeSessionCheckpoint(t.Context(), checkpoint.Id, "tag-uid", "s3://snapshots/snapshot-1", "")
	require.NoError(t, err)
	requestID := uuid.NewString()
	fork, _, err := store.ForkSession(t.Context(), checkpoint.Id, source.Creator, requestID, uuid.NewString())
	require.NoError(t, err)
	// An ordinary Create must not reuse a fork request ID, even for the same pair.
	_, _, err = store.CreateSession(t.Context(), &apiv1alpha1.Session{Id: uuid.NewString(), Creator: source.Creator, Agent: source.Agent, Name: fork.Name}, requestID)
	require.ErrorIs(t, err, database.ErrIdempotencyConflict)
	return fork, checkpoint.Id
}

func (a *lifecycleTestActors) EnsureActorEgressPolicy(_ context.Context, atespace, name string, policy, inherited *ateapipb.EgressPolicy) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.policyActor = actorKey(atespace, name)
	a.policy, a.inherited = proto.CloneOf(policy), proto.CloneOf(inherited)
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
			store.revision.EgressDestinations = []string{"*"}
			_, err := workflow.Create(t.Context(), session)
			require.ErrorContains(t, err, "invalid egress destination")
			require.Zero(t, actors.mutations.Load(), "validate the allowlist before issuing Actor creation")
			require.Equal(t, callsBefore, base.policyCalls)

			store.revision.EgressDestinations = []string{"https://api.example.com:443", "https://other.example.com:8443"}
			store.revision.Credentials = []egress.Credential{{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/auth/token"}}
			base.policyErr = context.DeadlineExceeded
			_, err = workflow.Create(t.Context(), session)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			current, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING, current.State)
			require.Empty(t, current.A2AAuthority)
			require.Equal(t, actorKey("team-a", substrate.ActorName(session.Id)), base.policyActor)
			require.Equal(t, &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "default"}, base.policy.Metadata)
			require.Len(t, base.policy.Rules, 2)
			require.Equal(t, &ateapipb.CredentialHeader{Header: "authorization", Prefix: "Bearer ", CredentialUri: "ate-secret://k8s.io/default/team-a/auth/token"}, base.policy.Rules[0].GetHttps().GetEffects().GetReplaceHeaders()[0])
			require.Equal(t, []string{"api.example.com"}, base.policy.Rules[0].GetHttps().GetHostnames())
			require.Equal(t, []string{"other.example.com"}, base.policy.Rules[1].GetHttps().GetHostnames())

			// Retry completes policy setup for the existing Actor before readiness.
			base.policyErr = nil
			ready, err := workflow.Create(t.Context(), session)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, ready.State)
			require.EqualValues(t, 1, actors.mutations.Load())
			require.Equal(t, callsBefore+2, base.policyCalls)
		})
	}
}

// A binding scoped to the golden boot, a private skill source's git
// credential, stays off every policy a session's Actor is given: on create and
// on a repoint. Its host stays reachable, and the model and MCP bindings stay.
func TestSessionEgressPolicyLeavesOutTheGoldenBootsCredentials(t *testing.T) {
	destinations := []string{"https://api.example.com", "https://mcp.example.com", "https://git.example.com"}
	credentials := []egress.Credential{
		{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/model/key"},
		{Hostname: "git.example.com", Header: "authorization", Prefix: "Basic ", URI: "ate-secret://k8s.io/default/team-a/git/token", Scope: egress.ScopeGolden},
		{Hostname: "mcp.example.com", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team-a/mcp/key"},
	}
	requireSessionPolicy := func(t *testing.T, policy *ateapipb.EgressPolicy) {
		t.Helper()
		headers := map[string][]string{}
		for _, rule := range policy.GetRules() {
			host := rule.GetHttps().GetHostnames()[0]
			headers[host] = []string{}
			for _, header := range rule.GetHttps().GetEffects().GetReplaceHeaders() {
				headers[host] = append(headers[host], header.GetHeader()+" "+header.GetPrefix()+header.GetCredentialUri())
			}
		}
		require.Equal(t, map[string][]string{
			"api.example.com": {"authorization Bearer ate-secret://k8s.io/default/team-a/model/key"},
			"git.example.com": {},
			"mcp.example.com": {"x-api-key ate-secret://k8s.io/default/team-a/mcp/key"},
		}, headers, "the source's host stays allowed without the golden boot's credential; the model and MCP bindings stay")
	}

	store, session := lifecycleFixture(t)
	store.revision.EgressDestinations, store.revision.Credentials = destinations, credentials
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
	session, err := workflow.Create(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, 1, actors.policyCalls)
	requireSessionPolicy(t, actors.policy)
	template, err := substrate.ActorEgressPolicy("team-a", destinations, credentials)
	require.NoError(t, err)
	require.True(t, proto.Equal(template, actors.inherited), "the template's default, which the new Actor starts with, is the policy replaced")

	current := database.RuntimeRevision{
		Revision: "revision-2", Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: destinations, Credentials: credentials,
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision-2", ActorTemplateUID: "actor-template-uid-2",
	}
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid", DesiredRevision: current.Revision}))
	require.NoError(t, store.RecordRuntimeRevision(t.Context(), current, true))
	moved, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, current.Revision, moved.GetPreparedRevision())
	require.Equal(t, 1, actors.replacedPolicies)
	requireSessionPolicy(t, actors.policy)
}

// The hosts a session was admitted with for its workspace volume are allowed
// on that session's Actor alone: on create and on a repoint, once each even
// when the Agent lists one, and never on a session without them or on the
// template's default the Actor starts with.
func TestSessionEgressPolicyAllowsTheSessionsProviders(t *testing.T) {
	httpsHosts := func(policy *ateapipb.EgressPolicy) []string {
		var hosts []string
		for _, rule := range policy.GetRules() {
			require.Equal(t, []int32{443}, rule.GetHttps().GetPorts().GetNumbers())
			hosts = append(hosts, rule.GetHttps().GetHostnames()...)
		}
		return hosts
	}
	store, plain := lifecycleFixtureWith(t, testVolumeSource())
	store.revision.EgressDestinations = []string{"https://api.example.com", "https://github.com"}
	session, _, err := store.CreateSession(t.Context(), &apiv1alpha1.Session{
		Id: uuid.NewString(), Creator: "alice", Agent: plain.GetAgent(), VolumeSource: testVolumeSource(),
		Providers: []*apiv1alpha1.SessionProvider{
			{Hostname: "github.com", Scheme: "Basic", Provider: "github"},
			{Hostname: "gitlab.example.com", Scheme: "Bearer", Provider: "gitlab"},
		},
	}, uuid.NewString())
	require.NoError(t, err)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)

	session, err = workflow.Create(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, actorKey("team-a", substrate.ActorName(session.GetId())), actors.policyActor)
	require.Equal(t, []string{"api.example.com", "github.com", "gitlab.example.com"}, httpsHosts(actors.policy), "the providers join the Agent's origins, github.com once")
	require.Equal(t, []string{"api.example.com", "github.com"}, httpsHosts(actors.inherited), "the template's default stays the revision's")

	_, err = workflow.Create(t.Context(), plain)
	require.NoError(t, err)
	require.Equal(t, []string{"api.example.com", "github.com"}, httpsHosts(actors.policy), "a session admitted without providers gets the revision's allowlist")

	current := supersedingRevision(t, store)
	moved, err := workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, current.Revision, moved.GetPreparedRevision())
	require.Equal(t, actorKey("team-a", substrate.ActorName(session.GetId())), actors.policyActor)
	require.Equal(t, []string{"api.example", "github.com", "gitlab.example.com"}, httpsHosts(actors.policy), "the repointed Actor keeps its providers on the current revision's allowlist")
}

// A session admitted with providers gets its caller credential on exactly
// its providers' hosts: the credential the gateway fetches for the provider
// instance's URI at the installation's authority, git for Basic and api for
// Bearer, never a value the controller or the sandbox holds. A session of the
// same Agent without providers and the template's default get none, and the
// Harness's own credentials stay as the revision binds them.
func TestSessionEgressPolicyBindsTheProvidersCallerCredentials(t *testing.T) {
	headers := func(policy *ateapipb.EgressPolicy) map[string][]string {
		result := map[string][]string{}
		for _, rule := range policy.GetRules() {
			host := rule.GetHttps().GetHostnames()[0]
			result[host] = []string{}
			for _, header := range rule.GetHttps().GetEffects().GetReplaceHeaders() {
				source := header.GetCredentialUri()
				if jwt := header.GetActorJwt(); jwt != nil {
					source = fmt.Sprintf("actor-jwt %v %ds", jwt.GetAudiences(), jwt.GetExpirationSeconds())
				}
				result[host] = append(result[host], header.GetHeader()+" "+header.GetPrefix()+source)
			}
		}
		return result
	}
	harness := map[string][]string{
		"api.example.com": {"authorization Bearer ate-secret://k8s.io/default/team-a/model/key"},
		"mcp.example.com": {"x-api-key ate-secret://k8s.io/default/team-a/mcp/key"},
	}
	store, plain := lifecycleFixtureWith(t, testVolumeSource())
	store.revision.EgressDestinations = []string{"https://api.example.com", "https://mcp.example.com"}
	store.revision.Credentials = []egress.Credential{
		{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/model/key"},
		{Hostname: "mcp.example.com", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team-a/mcp/key"},
	}
	session, _, err := store.CreateSession(t.Context(), &apiv1alpha1.Session{
		Id: uuid.NewString(), Creator: "alice", Agent: plain.GetAgent(), VolumeSource: testVolumeSource(),
		Providers: []*apiv1alpha1.SessionProvider{
			{Hostname: "github.com", Scheme: "Basic", Provider: "github"},
			{Hostname: "api.github.com", Scheme: "Bearer", Provider: "github"},
			{Hostname: "gitlab.example.com", Scheme: "Bearer", Provider: "gitlab"},
		},
	}, uuid.NewString())
	require.NoError(t, err)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second, WithCredentialAuthority("muster.giantswarm.io"))

	session, err = workflow.Create(t.Context(), session)
	require.NoError(t, err)
	withProviders := map[string][]string{
		"api.example.com":    harness["api.example.com"],
		"mcp.example.com":    harness["mcp.example.com"],
		"api.github.com":     {"authorization Bearer ate-secret://muster.giantswarm.io/github/api"},
		"gitlab.example.com": {"authorization Bearer ate-secret://muster.giantswarm.io/gitlab/api"},
		"github.com":         {"authorization Basic ate-secret://muster.giantswarm.io/github/git"},
	}
	require.Equal(t, withProviders, headers(actors.policy), "each provider gets its instance's credential URI, the Harness's bindings stay")
	require.Equal(t, harness, headers(actors.inherited), "the template's default carries no caller credential")

	_, err = workflow.Create(t.Context(), plain)
	require.NoError(t, err)
	require.Equal(t, harness, headers(actors.policy), "a session of the same Agent without providers gets no caller credential")

	supersedingRevision(t, store)
	_, err = workflow.RepointQuiesced(t.Context(), session)
	require.NoError(t, err)
	repointed := headers(actors.policy)
	for _, host := range []string{"api.github.com", "gitlab.example.com", "github.com"} {
		require.Equal(t, withProviders[host], repointed[host], "the repointed Actor keeps its caller credential on %s", host)
	}
}

// A provider on a host whose Authorization the Agent already binds cannot
// take it over: the session's policy is refused, naming the conflict.
func TestSessionEgressPolicyRefusesAProviderOnACredentialedHeader(t *testing.T) {
	revision := &database.RuntimeRevision{
		EgressDestinations: []string{"https://api.github.com"},
		Credentials:        []egress.Credential{{Hostname: "api.github.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/mcp/key"}},
	}
	_, err := sessionEgressPolicy("team-a", revision, providerAuthority{authority: "muster.giantswarm.io"}, []*apiv1alpha1.SessionProvider{{Hostname: "api.github.com", Scheme: "Bearer", Provider: "github"}})
	require.ErrorContains(t, err, "conflicting credentials for api.github.com header authorization")
	_, err = sessionEgressPolicy("team-a", revision, providerAuthority{authority: "muster.giantswarm.io"}, []*apiv1alpha1.SessionProvider{{Hostname: "api.github.com", PathPrefix: "/repos/", Scheme: "Bearer", Provider: "github"}})
	require.ErrorContains(t, err, "conflicting credentials for api.github.com header authorization", "a path prefix does not let a provider share the Agent's header")
}

// GitLab takes Basic for git and Bearer for its API under /api/ on one host:
// the git paths get the instance's git credential, the API's paths its API
// token. Two Bearer providers on one host and prefix stay refused.
func TestSessionEgressPolicyScopesAProviderToItsPathPrefix(t *testing.T) {
	const authority = "muster.giantswarm.io"
	git := &apiv1alpha1.SessionProvider{Hostname: "gitlab.example.com", Scheme: "Basic", Provider: "gitlab"}
	api := &apiv1alpha1.SessionProvider{Hostname: "gitlab.example.com", PathPrefix: "/api/", Scheme: "Bearer", Provider: "gitlab"}
	policy, err := sessionEgressPolicy("team-a", &database.RuntimeRevision{}, providerAuthority{authority: authority}, []*apiv1alpha1.SessionProvider{git, api})
	require.NoError(t, err)
	require.Len(t, policy.GetRules(), 1, "one rule for the host")
	rule := policy.GetRules()[0].GetHttps()
	require.Equal(t, []string{"gitlab.example.com"}, rule.GetHostnames())
	headers := rule.GetEffects().GetReplaceHeaders()
	require.Len(t, headers, 2)
	require.Equal(t, "authorization", headers[0].GetHeader())
	require.Equal(t, "Basic ", headers[0].GetPrefix())
	require.Empty(t, headers[0].GetPathPrefix())
	require.Equal(t, "ate-secret://muster.giantswarm.io/gitlab/git", headers[0].GetCredentialUri())
	require.Equal(t, "Bearer ", headers[1].GetPrefix())
	require.Equal(t, "/api/", headers[1].GetPathPrefix())
	require.Equal(t, "ate-secret://muster.giantswarm.io/gitlab/api", headers[1].GetCredentialUri())
	require.Nil(t, headers[1].GetActorJwt())

	wide := &apiv1alpha1.SessionProvider{Hostname: "gitlab.example.com", Scheme: "Bearer", Provider: "gitlab"}
	other := &apiv1alpha1.SessionProvider{Hostname: "gitlab.example.com", Scheme: "Bearer", Provider: "elsewhere"}
	_, err = sessionEgressPolicy("team-a", &database.RuntimeRevision{}, providerAuthority{authority: authority}, []*apiv1alpha1.SessionProvider{wide, other})
	require.ErrorContains(t, err, "conflicting credentials for gitlab.example.com header authorization")
}

// A session stored before its providers had an instance field of their own
// names the instance as the audience and gets the same credential URI; with
// no authority configured, a provider's host is allowed without a credential.
func TestSessionEgressPolicyReadsAStoredProvidersAudienceAsItsInstance(t *testing.T) {
	//nolint:staticcheck // audience is how a session stored before provider named the instance.
	stored := &apiv1alpha1.SessionProvider{Hostname: "api.github.com", Audience: "github", Scheme: "Bearer"}
	policy, err := sessionEgressPolicy("team-a", &database.RuntimeRevision{}, providerAuthority{authority: "muster.giantswarm.io"}, []*apiv1alpha1.SessionProvider{stored})
	require.NoError(t, err)
	headers := policy.GetRules()[0].GetHttps().GetEffects().GetReplaceHeaders()
	require.Len(t, headers, 1)
	require.Equal(t, "ate-secret://muster.giantswarm.io/github/api", headers[0].GetCredentialUri())

	policy, err = sessionEgressPolicy("team-a", &database.RuntimeRevision{}, providerAuthority{}, []*apiv1alpha1.SessionProvider{stored})
	require.NoError(t, err)
	require.Equal(t, []string{"api.github.com"}, policy.GetRules()[0].GetHttps().GetHostnames())
	require.Empty(t, policy.GetRules()[0].GetHttps().GetEffects().GetReplaceHeaders())
}

// A Bearer provider whose instance verifies the Actor's JWT itself (muster
// serving the session's person to its own sandbox) gets the JWT the gateway
// mints for the Actor, bound to the instance's configured audience, on its
// host alone, with or without a credential authority; the session's other
// providers keep their credential URIs, and the template's default and a
// session of the same Agent without providers get no actor JWT.
func TestSessionEgressPolicyBindsTheActorJWTOfAProviderThatVerifiesIt(t *testing.T) {
	const muster = "muster.agent-platform.svc"
	store, plain := lifecycleFixtureWith(t, testVolumeSource())
	store.revision.EgressDestinations = []string{"http://" + muster + ":8090"}
	session, _, err := store.CreateSession(t.Context(), &apiv1alpha1.Session{
		Id: uuid.NewString(), Creator: "alice", Agent: plain.GetAgent(), VolumeSource: testVolumeSource(),
		Providers: []*apiv1alpha1.SessionProvider{
			{Hostname: muster, Scheme: "Bearer", Provider: "muster"},
			{Hostname: "github.com", Scheme: "Basic", Provider: "github"},
		},
	}, uuid.NewString())
	require.NoError(t, err)
	actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second,
		WithCredentialAuthority("muster.giantswarm.io"), WithActorJWTProviders(map[string]egress.ActorJWTProvider{"muster": {Hostname: muster, Audience: "https://muster.example.com"}}))

	musterHeaders := func(policy *ateapipb.EgressPolicy) []*ateapipb.CredentialHeader {
		var headers []*ateapipb.CredentialHeader
		for _, rule := range policy.GetRules() {
			if slices.Contains(rule.GetHttp().GetHostnames(), muster) {
				require.Equal(t, []int32{8090}, rule.GetHttp().GetPorts().GetNumbers())
				headers = append(headers, rule.GetHttp().GetEffects().GetReplaceHeaders()...)
			}
			if slices.Contains(rule.GetHttps().GetHostnames(), muster) {
				headers = append(headers, rule.GetHttps().GetEffects().GetReplaceHeaders()...)
			}
		}
		return headers
	}
	_, err = workflow.Create(t.Context(), session)
	require.NoError(t, err)
	headers := musterHeaders(actors.policy)
	require.Len(t, headers, 2, "the Agent's in-cluster origin and the provider's HTTPS origin of the host")
	for _, header := range headers {
		require.Equal(t, "authorization", header.GetHeader())
		require.Equal(t, "Bearer ", header.GetPrefix())
		require.Empty(t, header.GetCredentialUri(), "no credential URI: the provider verifies the Actor's JWT")
		require.Equal(t, []string{"https://muster.example.com"}, header.GetActorJwt().GetAudiences())
		require.Equal(t, int64(900), header.GetActorJwt().GetExpirationSeconds())
	}
	for _, rule := range actors.policy.GetRules() {
		if slices.Contains(rule.GetHttps().GetHostnames(), "github.com") {
			require.Equal(t, "ate-secret://muster.giantswarm.io/github/git", rule.GetHttps().GetEffects().GetReplaceHeaders()[0].GetCredentialUri())
		}
	}
	require.Empty(t, musterHeaders(actors.inherited), "the template's default carries no actor JWT")

	_, err = workflow.Create(t.Context(), plain)
	require.NoError(t, err)
	require.Empty(t, musterHeaders(actors.policy), "a session of the same Agent without providers gets no actor JWT")

	policy, err := sessionEgressPolicy("team-a", &database.RuntimeRevision{}, providerAuthority{actorJWT: map[string]egress.ActorJWTProvider{"muster": {Hostname: muster, Audience: "https://muster.example.com"}}},
		[]*apiv1alpha1.SessionProvider{{Hostname: muster, Scheme: "Bearer", Provider: "muster"}, {Hostname: "github.com", Scheme: "Basic", Provider: "github"}})
	require.NoError(t, err)
	require.Len(t, policy.GetRules(), 2)
	for _, rule := range policy.GetRules() {
		switch rule.GetHttps().GetHostnames()[0] {
		case muster:
			require.Equal(t, []string{"https://muster.example.com"}, rule.GetHttps().GetEffects().GetReplaceHeaders()[0].GetActorJwt().GetAudiences(), "no credential authority is needed for an actor JWT")
		default:
			require.Empty(t, rule.GetHttps().GetEffects().GetReplaceHeaders(), "without an authority the other providers get no credential")
		}
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
	policy, err = substrate.ActorEgressPolicy("team-a", []string{"https://*.example.com:443"}, nil)
	require.NoError(t, err, "a leftmost-label wildcard is a Substrate hostname pattern")
	require.Equal(t, []string{"*.example.com"}, policy.Rules[0].GetHttps().GetHostnames())
	for _, destination := range []string{"", "*", "api.example.com", "https://api.example.com/path", "http://api.example.com:0", "http://api.example.com:65536", "http://user@api.example.com", "http://api.example.com?key=value",
		"https://*", "https://*.com", "https://a.*.example.com", "https://*.*.example.com", "https://*example.com"} {
		t.Run(destination, func(t *testing.T) {
			_, err := substrate.ActorEgressPolicy("team-a", []string{destination}, nil)
			require.Error(t, err)
		})
	}
	for _, destination := range []string{"http://192.0.2.1", "http://[2001:db8::1]", "http://[::ffff:192.0.2.1]"} {
		_, err := substrate.ActorEgressPolicy("team-a", []string{destination}, nil)
		require.ErrorContains(t, err, "requires a DNS name")
	}
	_, err = substrate.ActorEgressPolicy("team-a", []string{"http://api.example.com:8443", "https://api.example.com:8443"}, nil)
	require.ErrorContains(t, err, "both HTTP and HTTPS on the same port")
}

func TestActorEgressCredentialsRequireAllowedDestination(t *testing.T) {
	bindings := []egress.Credential{
		{Hostname: "api.example.com", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/auth/token"},
		{Hostname: "api.example.com", Header: "x-api-key", URI: "ate-secret://k8s.io/default/team/auth/key"},
	}
	_, err := substrate.ActorEgressPolicy("team", []string{"https://other.example.com"}, bindings)
	require.ErrorContains(t, err, "is not allowed")
	policy, err := substrate.ActorEgressPolicy("team", []string{"http://api.example.com:8080", "https://api.example.com:8443", "https://other.example.com"}, bindings)
	require.NoError(t, err)
	require.Len(t, policy.Rules, 3)
	require.Len(t, policy.Rules[0].GetHttp().GetEffects().GetReplaceHeaders(), 2, "configured HTTP endpoints also require credential replacement")
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
	session, err := service.Create(ctx, CreateRequest{Agent: fixture.Agent, RequestID: "retry-request", Name: "conversation"})
	require.NoError(t, err)
	suspended, err := service.Suspend(ctx, session.Id)
	require.NoError(t, err)
	mutations := actors.mutations.Load()
	retried, err := service.Create(ctx, CreateRequest{Agent: fixture.Agent, RequestID: "retry-request", Name: "ignored retry name"})
	require.NoError(t, err)
	require.Equal(t, suspended.Id, retried.Id)
	require.Equal(t, suspended.State, retried.State)
	require.Equal(t, mutations, actors.mutations.Load(), "creation retry cannot reissue runtime work")
	deleted, err := service.Delete(ctx, session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	require.Empty(t, deleted.A2AAuthority)
	mutations = actors.mutations.Load()
	_, err = service.Create(ctx, CreateRequest{Agent: fixture.Agent, RequestID: "retry-request"})
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
	_, err = service.Get(ctx, session.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = service.Delete(ctx, session.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	_, err = service.Resume(ctx, session.Id)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
	require.Equal(t, mutations, actors.mutations.Load(), "a tombstoned request must not create or touch compute")
}

func TestRuntimeLost(t *testing.T) {
	session := &apiv1alpha1.Session{Id: "session-1", PreparedRevision: "revision-1"}
	name := substrate.ActorName(session.Id)
	for _, test := range []struct {
		name      string
		state     ateapipb.ActorState
		snapshot  bool
		crash     string
		missing   bool
		untouched bool
		getErr    error
		wantCause string
		wantLost  bool
		wantErr   string
	}{
		{name: "missing", missing: true, wantCause: "Actor team-a/" + name + " not found", wantLost: true},
		{name: "crashed without a snapshot", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, wantCause: "Actor team-a/" + name + " crashed with no snapshot to return to", wantLost: true},
		// A session without a task gave its runtime nothing to keep: the
		// resume reverts the Actor to where it started.
		{name: "crashed without a snapshot, no task", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, untouched: true, crash: restoreCrash, wantCause: "Actor team-a/" + name + " crashed: " + restoreCrash},
		// Substrate returns a crashed Actor to its durable snapshot; the resume
		// tells whether it can. The crash is reported, the runtime is not lost.
		{name: "crashed with a snapshot", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, snapshot: true, wantCause: "Actor team-a/" + name + " crashed"},
		{name: "crashed with a snapshot and a message", state: ateapipb.ActorState_ACTOR_STATE_CRASHED, snapshot: true, crash: "worker drained", wantCause: "Actor team-a/" + name + " crashed: worker drained"},
		{name: "paused", state: ateapipb.ActorState_ACTOR_STATE_PAUSED},
		{name: "running", state: ateapipb.ActorState_ACTOR_STATE_RUNNING},
		{name: "suspended", state: ateapipb.ActorState_ACTOR_STATE_SUSPENDED, snapshot: true},
		{name: "unknown", getErr: status.Error(codes.Unavailable, "ate-api is rolling"), wantErr: "ate-api is rolling"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a"}, untouched: test.untouched}
			actors := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}, getErr: test.getErr}
			if !test.missing {
				actor := &ateapipb.Actor{
					Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: name, Uid: "actor-uid"},
					Status:   &ateapipb.ActorStatus{State: test.state},
				}
				if test.snapshot {
					actor.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/" + name + "/last", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL}
				}
				if test.crash != "" {
					actor.Status.Crash = &ateapipb.ActorCrash{Message: test.crash}
				}
				actors.actors[actorKey("team-a", name)] = actor
			}
			cause, lost, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second).RuntimeLost(t.Context(), session)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.False(t, lost, "not knowing is not lost")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantLost, lost)
			require.Equal(t, test.wantCause, cause)
		})
	}
}

// crashingActors reports the session's Actor RUNNING for the first running
// reads, then as Substrate records it once it notices the worker went away
// under the turn: crashed, back at its snapshot.
type crashingActors struct {
	*lifecycleTestActors
	running int
	reads   int
}

func (a *crashingActors) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	actor, err := a.lifecycleTestActors.GetActor(ctx, atespace, name)
	if err != nil {
		return nil, err
	}
	if a.reads++; a.reads > a.running {
		actor.Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
		actor.Status.Crash = &ateapipb.ActorCrash{Message: "worker drained"}
	}
	return actor, nil
}

func TestTurnCrash(t *testing.T) {
	session := &apiv1alpha1.Session{Id: "session-1", PreparedRevision: "revision-1"}
	store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a"}}
	name := substrate.ActorName(session.Id)
	newActors := func(running int) *crashingActors {
		return &crashingActors{running: running, lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{actorKey("team-a", name): {
			Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: name, Uid: "actor-uid"},
			Status: &ateapipb.ActorStatus{
				State:            ateapipb.ActorState_ACTOR_STATE_RUNNING,
				ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/" + name + "/last", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL},
			},
		}}}}
	}
	t.Run("crashes while asked again", func(t *testing.T) {
		actors := newActors(2)
		workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
		workflow.crashWait, workflow.crashRetry = time.Second, time.Millisecond
		cause, lost, err := workflow.TurnCrash(t.Context(), session)
		require.NoError(t, err)
		require.False(t, lost, "an Actor back at its snapshot is not lost")
		require.Equal(t, "Actor team-a/"+name+" crashed: worker drained", cause)
		require.Equal(t, 3, actors.reads)
	})
	t.Run("still running at the bound", func(t *testing.T) {
		actors := newActors(math.MaxInt)
		workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
		workflow.crashWait, workflow.crashRetry = 20*time.Millisecond, time.Millisecond
		cause, lost, err := workflow.TurnCrash(t.Context(), session)
		require.NoError(t, err)
		require.False(t, lost)
		require.Empty(t, cause, "an Actor that has not crashed keeps its turn")
		require.Greater(t, actors.reads, 1)
	})
	t.Run("not running", func(t *testing.T) {
		actors := newActors(0)
		workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
		workflow.crashWait, workflow.crashRetry = time.Second, time.Millisecond
		cause, _, err := workflow.TurnCrash(t.Context(), session)
		require.NoError(t, err)
		require.NotEmpty(t, cause)
		require.Equal(t, 1, actors.reads, "a crashed Actor is answered at once")
	})
}

// heldActors answers the resumes in answers in order, the way ate-api answers
// while it cannot resume the Actor yet, and resumes it after them. A last
// answer that asks for the resume later repeats: the condition outlasts the
// bound.
type heldActors struct {
	*lifecycleTestActors
	answers []error
	resumes int
}

func (a *heldActors) ResumeActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.resumes++
	var err error
	if len(a.answers) > 0 {
		err = a.answers[0]
		if len(a.answers) > 1 || !resumeLater(err) {
			a.answers = a.answers[1:]
		}
	}
	if errors.Is(err, errRestoreCrashed) {
		a.mu.Lock()
		actor := a.actors[actorKey(atespace, name)]
		actor.Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
		actor.Status.Crash = &ateapipb.ActorCrash{Message: restoreCrash}
		a.mu.Unlock()
		err = errCrashedForGood
	}
	if err != nil {
		return nil, err
	}
	return a.lifecycleTestActors.ResumeActor(ctx, atespace, name)
}

// restoreCrash is the crash Substrate records for a restore whose runtime
// never came up.
const restoreCrash = `resume failed: atelet Restore: while calling ateom.RestoreWorkload: rpc error: code = Internal desc = while waiting for container wakeup probe: wakeup probe for "kagent" never returned 200 within 30s`

// errRestoreCrashed makes heldActors crash the Actor under the resume, as
// Substrate does when the restored runtime never comes up, and answer the
// resume the way ate-api then does.
var errRestoreCrashed = errors.New("the restore crashes the Actor")

var (
	// The answers ate-api gives a resume it cannot perform yet: the worker
	// pool has no free worker, a departing worker cut the resume short, and
	// another operation holds the Actor.
	errNoFreeWorker = status.Error(codes.ResourceExhausted, "no free workers available")
	errResumeCut    = status.Error(codes.Unavailable, "worker is draining")
	errActorHeld    = status.Error(codes.Aborted, "another operation is in progress for this actor")
	// The answers ate-api gives a resume of a crashed Actor: it is returning
	// the Actor its worker's departure crashed to its snapshot, or it refuses
	// to resume an Actor crashed on a live worker.
	errCrashRecovering = status.Error(codes.Unavailable, "resume of actor team-a/session-1 was interrupted by its worker (suspend failed: atelet Checkpoint: worker unreachable while tearing down the failed actor); the actor returns to its snapshot, retry the resume")
	errCrashedForGood  = status.Error(codes.FailedPrecondition, "AssignWorker prerequisite not met for Actor: team-a/session-1 (got: ACTOR_STATE_CRASHED, want ACTOR_STATE_SUSPENDED or ACTOR_STATE_PAUSED)")
)

func TestAwaitRuntime(t *testing.T) {
	session := &apiv1alpha1.Session{Id: "session-1", PreparedRevision: "revision-1"}
	name := substrate.ActorName(session.GetId())
	for _, test := range []struct {
		name        string
		crashed     bool
		noSnapshot  bool
		untouched   bool
		answers     []error
		revertErr   error
		wantResumes int
		wantReverts int
		wantErr     string
		wantLost    bool
		wantCrash   string
	}{
		{name: "free", wantResumes: 1},
		{name: "held, then free", answers: []error{errActorHeld, errActorHeld, errActorHeld, nil}, wantResumes: 4},
		{name: "no free worker, then one", answers: []error{errNoFreeWorker, errResumeCut, errActorHeld, nil}, wantResumes: 4},
		// The last answer repeats: the condition outlasts the bound.
		{name: "held past the bound", answers: []error{errActorHeld}, wantErr: "still deferred by Substrate after"},
		{name: "no free worker past the bound", answers: []error{errNoFreeWorker}, wantErr: "no free workers available"},
		{name: "not transient", answers: []error{errActorHeld, status.Error(codes.PermissionDenied, "denied")}, wantResumes: 2, wantErr: "denied"},
		// A crashed Actor with a snapshot is resumed from it once Substrate
		// has returned it there. One Substrate refuses to resume, its sandbox
		// dead under the runtime, is reverted to that snapshot and resumed
		// again, once: the conversation goes on from the snapshot.
		{name: "crashed, returned to its snapshot", crashed: true, answers: []error{errCrashRecovering, errNoFreeWorker, nil}, wantResumes: 3},
		{name: "crashed, reverted to its snapshot", crashed: true, answers: []error{errCrashedForGood}, wantResumes: 2, wantReverts: 1},
		{name: "crashed again after the revert", answers: []error{errRestoreCrashed, errRestoreCrashed}, wantResumes: 2, wantReverts: 1, wantErr: "Actor team-a/" + name + " crashed and cannot be resumed", wantLost: true, wantCrash: "crash: " + restoreCrash},
		{name: "refused, not crashed", answers: []error{errCrashedForGood}, wantResumes: 1, wantErr: "AssignWorker prerequisite not met"},
		// The first resume of a session restores its template's golden
		// snapshot. A restore whose runtime never comes up crashes the Actor
		// with nothing of the session's in it: it is reverted and resumed
		// again, once.
		{name: "first restore crashed, session without a task", noSnapshot: true, untouched: true, answers: []error{errRestoreCrashed, nil}, wantResumes: 2, wantReverts: 1},
		{name: "first restore crashed twice", noSnapshot: true, untouched: true, answers: []error{errRestoreCrashed, errRestoreCrashed}, wantResumes: 2, wantReverts: 1, wantErr: "Actor team-a/" + name + " crashed and cannot be resumed", wantLost: true, wantCrash: "crash: " + restoreCrash},
		{name: "restore crashed, session with a task", noSnapshot: true, answers: []error{errRestoreCrashed}, wantResumes: 1, wantErr: "Actor team-a/" + name + " crashed and cannot be resumed", wantLost: true, wantCrash: "crash: " + restoreCrash},
		{name: "revert refused", noSnapshot: true, untouched: true, answers: []error{errRestoreCrashed}, revertErr: status.Error(codes.Aborted, "concurrent update conflict"), wantResumes: 1, wantErr: "revert Actor team-a/" + name + ": rpc error: code = Aborted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &lifecycleTestStore{revision: &database.RuntimeRevision{ActorTemplateAtespace: "team-a"}, untouched: test.untouched}
			key := actorKey("team-a", name)
			actor := &ateapipb.Actor{Status: &ateapipb.ActorStatus{
				State:            ateapipb.ActorState_ACTOR_STATE_SUSPENDED,
				ExternalSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/" + name + "/last"},
			}}
			if test.noSnapshot {
				actor.Status.ExternalSnapshot = nil
			}
			if test.crashed {
				actor.Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
				actor.Status.Crash = &ateapipb.ActorCrash{Message: "suspend failed: atelet Checkpoint: worker unreachable while tearing down the failed actor"}
			}
			actors := &heldActors{lifecycleTestActors: &lifecycleTestActors{actors: map[string]*ateapipb.Actor{key: actor}, revertErr: test.revertErr}, answers: test.answers}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			workflow.busyWait, workflow.busyRetry = 200*time.Millisecond, time.Millisecond
			err := workflow.AwaitRuntime(t.Context(), session)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, ateapipb.ActorState_ACTOR_STATE_RUNNING, actors.actors[key].GetStatus().GetState())
			}
			if test.wantLost {
				require.ErrorIs(t, err, ErrRuntimeLost)
				var loss *RuntimeLossError
				require.ErrorAs(t, err, &loss)
				require.Equal(t, "Actor team-a/"+name+" crashed and cannot be resumed", loss.Cause)
				wantCrash := test.wantCrash
				if wantCrash == "" {
					wantCrash = "crash: suspend failed: atelet Checkpoint"
				}
				require.ErrorContains(t, err, wantCrash, "the crash is kept for the log")
				require.ErrorIs(t, err, errCrashedForGood)
			} else {
				require.NotErrorIs(t, err, ErrRuntimeLost)
			}
			if test.wantResumes != 0 {
				require.Equal(t, test.wantResumes, actors.resumes)
			} else {
				require.Greater(t, actors.resumes, 1, "an Actor Substrate cannot resume yet is resumed again within the bound")
			}
			require.Equal(t, test.wantReverts, actors.reverts)
		})
	}
}

// A resume Substrate answers later within a lifecycle attempt, with no free
// worker while the pool rolls, a resume a departing worker cut short or
// another operation holding the Actor, is issued again until it goes through,
// and the session is ready without a second request. Any other answer fails
// the attempt at once; an answer that outlasts the attempt is returned as it
// is. The Actor is still suspended, so the operation ends: the session takes
// a delete, and the next request resumes it.
func TestResumeWaitsWhileSubstrateAnswersLater(t *testing.T) {
	for _, test := range []struct {
		name        string
		answers     []error
		attempt     time.Duration
		wantResumes int
		wantCode    codes.Code
	}{
		{name: "no free worker, then one", answers: []error{errNoFreeWorker, errNoFreeWorker, errResumeCut, errActorHeld, nil}, wantResumes: 5},
		{name: "not transient", answers: []error{errNoFreeWorker, status.Error(codes.PermissionDenied, "denied")}, wantResumes: 2, wantCode: codes.PermissionDenied},
		{name: "no free worker past the attempt", answers: []error{errNoFreeWorker}, attempt: 200 * time.Millisecond, wantCode: codes.ResourceExhausted},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			setup := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			session, err := setup.Create(t.Context(), session)
			require.NoError(t, err)
			session, err = setup.Suspend(t.Context(), session)
			require.NoError(t, err)

			actors := &heldActors{lifecycleTestActors: base, answers: test.answers}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			workflow.busyRetry = time.Millisecond
			ctx := t.Context()
			if test.attempt != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.attempt)
				defer cancel()
			}
			resumed, err := workflow.Resume(ctx, session)
			if test.wantCode == codes.OK {
				require.NoError(t, err)
				require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, resumed.GetState())
				require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, resumed.GetOperation())
				require.Equal(t, test.wantResumes, actors.resumes)
				return
			}
			require.Equal(t, test.wantCode, status.Code(err))
			if test.wantResumes != 0 {
				require.Equal(t, test.wantResumes, actors.resumes)
			} else {
				require.Greater(t, actors.resumes, 1, "a resume Substrate answers later is issued again within the attempt")
			}
			ended, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, ended.GetOperation(), "a resume that left the Actor suspended holds no operation")
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, ended.GetState())
			ready, err := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second).Resume(t.Context(), ended)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, ready.GetState())
		})
	}
}

// rollingActors answers its first unavailable ate-api calls, reads and
// repoints alike, Unavailable: the controller's connection to an ate-api whose
// pods are replaced fails every call until it reaches a new pod.
type rollingActors struct {
	*lifecycleTestActors
	unavailable int
	calls       int
}

func (a *rollingActors) rolling() error {
	a.calls++
	if a.calls <= a.unavailable {
		return status.Error(codes.Unavailable, "connection error: desc = \"transport: Error while dialing: dial tcp 10.96.0.20:443: connect: connection refused\"")
	}
	return nil
}

func (a *rollingActors) GetActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if err := a.rolling(); err != nil {
		return nil, err
	}
	return a.lifecycleTestActors.GetActor(ctx, atespace, name)
}

func (a *rollingActors) RepointActor(ctx context.Context, atespace, name, templateAtespace, templateName string) (*ateapipb.Actor, error) {
	if err := a.rolling(); err != nil {
		return nil, err
	}
	return a.lifecycleTestActors.RepointActor(ctx, atespace, name, templateAtespace, templateName)
}

func (a *rollingActors) ResumeActor(ctx context.Context, atespace, name string) (*ateapipb.Actor, error) {
	if err := a.rolling(); err != nil {
		return nil, err
	}
	return a.lifecycleTestActors.ResumeActor(ctx, atespace, name)
}

// A resume issued while ate-api rolls waits until the controller reaches the
// new control plane, the reads and the repoint onto the agent's current
// revision before the resume as much as the resume, and the session is ready
// without a second request.
func TestResumeWaitsOutAnAteAPIRoll(t *testing.T) {
	for _, unavailable := range []int{1, 2, 3, 5} {
		t.Run(fmt.Sprintf("%d calls unavailable", unavailable), func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			setup := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			session, err := setup.Create(t.Context(), session)
			require.NoError(t, err)
			session, err = setup.Suspend(t.Context(), session)
			require.NoError(t, err)
			current := supersedingRevision(t, store)

			actors := &rollingActors{lifecycleTestActors: base, unavailable: unavailable}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			workflow.busyRetry = time.Millisecond
			resumed, err := workflow.Resume(t.Context(), session)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, resumed.GetState())
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, resumed.GetOperation())
			require.Equal(t, current.Revision, resumed.GetPreparedRevision())
			require.Greater(t, actors.calls, unavailable)
		})
	}
}

// A resume whose repoint ate-api answers Unavailable past the attempt fails,
// and the Actor is still suspended: the operation ends instead of holding the
// session, so its delete is not refused, and a later resume goes through.
func TestResumeFailedBeforeTheResumeHoldsNoOperation(t *testing.T) {
	for _, next := range []string{"delete", "resume"} {
		t.Run(next, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			setup := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			session, err := setup.Create(t.Context(), session)
			require.NoError(t, err)
			session, err = setup.Suspend(t.Context(), session)
			require.NoError(t, err)
			supersedingRevision(t, store)

			base.repointErr = status.Error(codes.Unavailable, "connection error")
			workflow := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			workflow.busyRetry = time.Millisecond
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err = workflow.Resume(ctx, session)
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.ErrorContains(t, err, "the Actor is still suspended")

			ended, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, ended.GetState())
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, ended.GetOperation())

			base.repointErr = nil
			switch next {
			case "delete":
				deleted, err := setup.Delete(t.Context(), ended)
				require.NoError(t, err, "a failed resume must not refuse the session's delete")
				require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, deleted.GetState())
			case "resume":
				resumed, err := setup.Resume(t.Context(), ended)
				require.NoError(t, err)
				require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, resumed.GetState())
			}
		})
	}
}

// transientCrashActors answers a resume the way Substrate does when a drained
// worker cuts its restore short: Internal, the Actor crashed until Substrate
// returns it to rest.
type transientCrashActors struct {
	*lifecycleTestActors
}

func (a *transientCrashActors) ResumeActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actors[actorKey(atespace, name)].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	return nil, status.Error(codes.Internal, "while calling ateom.RestoreWorkload: wakeup probe cancelled: context canceled")
}

func (a *transientCrashActors) rest(atespace, name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actors[actorKey(atespace, name)].Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
}

// A resume whose Actor crashed for a transient cause keeps its operation while
// the Actor is crashed; once Substrate returns the Actor to rest, or the Actor
// is gone, a delete ends the held resume and deletes the session instead of
// being refused.
func TestDeleteEndsAHeldResumeOnceTheActorRests(t *testing.T) {
	for _, then := range []string{"rests", "gone"} {
		t.Run(then, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			setup := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			session, err := setup.Create(t.Context(), session)
			require.NoError(t, err)
			session, err = setup.Suspend(t.Context(), session)
			require.NoError(t, err)

			actors := &transientCrashActors{lifecycleTestActors: base}
			workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Second)
			_, err = workflow.Resume(t.Context(), session)
			require.Equal(t, codes.Internal, status.Code(err))
			held, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, held.GetOperation(), "a crashed Actor leaves the outcome open")

			_, err = workflow.Delete(t.Context(), held)
			require.ErrorIs(t, err, database.ErrConflict, "a delete waits while the Actor is crashed")
			held, err = store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, held.GetOperation(), "the refused delete leaves the resume held")

			name := substrate.ActorName(session.Id)
			switch then {
			case "rests":
				actors.rest("team-a", name)
			case "gone":
				require.NoError(t, base.DeleteActor(t.Context(), "team-a", name, true))
			}
			deleted, err := workflow.Delete(t.Context(), held)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, deleted.GetState())
		})
	}
}

// snapshotLossActors answers every resume the way Substrate does when the
// Actor's snapshot is gone from the store: DataLoss, the Actor left as state.
type snapshotLossActors struct {
	*lifecycleTestActors
	state ateapipb.ActorState
}

func (a *snapshotLossActors) ResumeActor(_ context.Context, atespace, name string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actors[actorKey(atespace, name)].Status.State = a.state
	return nil, status.Error(codes.DataLoss, "external snapshot not found")
}

func TestResumeAfterSnapshotLossFailsTheSessionAndDeleteProceeds(t *testing.T) {
	for _, state := range []ateapipb.ActorState{ateapipb.ActorState_ACTOR_STATE_CRASHED, ateapipb.ActorState_ACTOR_STATE_SUSPENDED} {
		t.Run(state.String(), func(t *testing.T) {
			store, session := lifecycleFixture(t)
			base := &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}
			setup := NewActorWorkflow(store, base, make(chan struct{}, 1), time.Second)
			session, err := setup.Create(t.Context(), session)
			require.NoError(t, err)
			session, err = setup.Suspend(t.Context(), session)
			require.NoError(t, err)

			workflow := NewActorWorkflow(store, &snapshotLossActors{lifecycleTestActors: base, state: state}, make(chan struct{}, 1), time.Second)
			_, err = workflow.Resume(t.Context(), session)
			require.ErrorIs(t, err, ErrRuntimeLost)
			require.ErrorContains(t, err, "external snapshot not found")

			failed, err := store.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_FAILED, failed.State)
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, failed.Operation)
			require.Equal(t, "RuntimeLost", failed.GetFailure().GetReason())
			require.Contains(t, failed.GetFailure().GetMessage(), "external snapshot not found")

			_, err = workflow.Resume(t.Context(), failed)
			require.ErrorIs(t, err, database.ErrConflict, "a failed session cannot be resumed again")

			deleted, err := workflow.Delete(t.Context(), failed)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
			_, err = store.GetSession(t.Context(), session.Id, session.Creator)
			require.ErrorIs(t, err, database.ErrNotFound)
			_, err = base.GetActor(t.Context(), "team-a", substrate.ActorName(session.Id))
			require.Equal(t, codes.NotFound, status.Code(err), "Delete must remove the Actor")
		})
	}
}

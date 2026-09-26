package sandbox

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testSession string

func (s testSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: string(s)}}
}

type testActors struct {
	substrate.LifecycleClient
	mu             sync.Mutex
	actor          *ateapipb.Actor
	egressPolicy   *ateapipb.EgressPolicy
	calls          []string
	readErr        error
	mutationErr    error
	suspendStarted chan struct{}
	suspendRelease <-chan struct{}
}

func (a *testActors) GetActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.readErr != nil {
		return nil, a.readErr
	}
	if a.actor == nil {
		return nil, status.Error(codes.NotFound, "missing")
	}
	return proto.CloneOf(a.actor), nil
}
func (a *testActors) CreateActor(_ context.Context, space, name, templateSpace, templateName string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "create")
	a.actor = &ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: space, Name: name, Uid: "uid"},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: templateSpace, Name: templateName},
		Status:        &ateapipb.ActorStatus{State: ateapipb.ActorState_ACTOR_STATE_SUSPENDED}}
	return proto.CloneOf(a.actor), a.mutationErr
}
func (a *testActors) EnsureActorEgressPolicy(_ context.Context, _, _ string, policy *ateapipb.EgressPolicy) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "policy")
	a.egressPolicy = proto.CloneOf(policy)
	return a.mutationErr
}
func (a *testActors) ResumeActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "resume")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_RUNNING
	return proto.CloneOf(a.actor), a.mutationErr
}
func (a *testActors) SuspendActor(context.Context, string, string) (*ateapipb.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.suspendStarted != nil {
		close(a.suspendStarted)
		a.suspendStarted = nil
		<-a.suspendRelease
	}
	a.calls = append(a.calls, "suspend")
	a.actor.Status.State = ateapipb.ActorState_ACTOR_STATE_SUSPENDED
	return proto.CloneOf(a.actor), a.mutationErr
}
func (a *testActors) DeleteActor(context.Context, string, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "delete")
	a.actor = nil
	return a.mutationErr
}

func (a *testActors) observedCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.calls)
}

func startWorker(t *testing.T, service *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- service.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
}

func serviceFixture(t *testing.T) (*Service, context.Context, *testActors) {
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
	store := database.NewClient(pool)
	require.NoError(t, store.UpsertSandboxTemplateDefinition(ctx, database.SandboxTemplateDefinition{Namespace: "team-a", SandboxTemplateName: "scratch", SandboxTemplateUID: "template-uid", DesiredRevision: "revision"}))
	require.NoError(t, store.RecordSandboxRevision(ctx, database.SandboxRevision{
		RuntimeArtifact:     database.RuntimeArtifact{Revision: "revision", Kind: "sandbox", Namespace: "team-a", ActorTemplateAtespace: "team-a", ActorTemplateName: "revision", ActorTemplateUID: "revision-uid"},
		SandboxTemplateName: "scratch", SandboxTemplateUID: "template-uid", SourceSnapshot: []byte("{}"),
	}, true))
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha3.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "scratch", UID: "template-uid"},
	}).Build()
	actors := &testActors{}
	service, err := NewService(Config{Store: store, Kube: kube, Authorizer: auth.NoopAuthorizer{}, Actors: actors,
		DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour})
	require.NoError(t, err)
	return service, auth.AuthSessionTo(t.Context(), testSession("alice")), actors
}

func createRequest() *apiv1alpha1.CreateSandboxRequest {
	return &apiv1alpha1.CreateSandboxRequest{SandboxTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "scratch"}, RequestId: "create"}
}

func TestSandboxLifecycleClientRetriesAfterRestart(t *testing.T) {
	for _, name := range []string{"create", "suspend", "resume", "delete"} {
		t.Run(name, func(t *testing.T) {
			service, ctx, actors := serviceFixture(t)
			var instance *apiv1alpha1.Sandbox
			if name != "create" {
				var err error
				instance, err = service.Create(ctx, createRequest())
				require.NoError(t, err, "creation must work without a background worker")
				if name == "resume" || name == "delete" {
					_, err = service.Suspend(ctx, instance.Id)
					require.NoError(t, err)
				}
			}
			call := func(s *Service) (*apiv1alpha1.Sandbox, error) {
				switch name {
				case "suspend":
					return s.Suspend(ctx, instance.Id)
				case "resume":
					return s.Resume(ctx, instance.Id)
				case "delete":
					return s.Delete(ctx, instance.Id)
				default:
					return s.Create(ctx, createRequest())
				}
			}
			actors.mutationErr = status.Error(codes.Unavailable, "response lost after effect")
			_, err := call(service)
			require.Equal(t, codes.Unavailable, status.Code(err))
			listed, err := service.List(ctx, &apiv1alpha1.ListSandboxesRequest{})
			require.NoError(t, err)
			require.Len(t, listed.Sandboxes, 1)
			instance = listed.Sandboxes[0]
			pending, err := service.config.Store.GetSandboxOperation(ctx, instance.Id)
			require.NoError(t, err)
			require.NotEqual(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, pending.Instance.Operation)
			restarted, err := NewService(service.config)
			require.NoError(t, err)
			actors.mutationErr = nil
			result, err := call(restarted)
			require.NoError(t, err)
			require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, result.Operation)
			wanted := apiv1alpha1.RuntimeState_RUNTIME_STATE_READY
			if name == "suspend" {
				wanted = apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED
			}
			if name == "delete" {
				wanted = apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED
			}
			require.Equal(t, wanted, result.State)
			completed, err := service.config.Store.GetSandboxOperation(ctx, instance.Id)
			require.NoError(t, err)
			require.Equal(t, pending.ID, completed.ID, "retry continues the same operation")
			count := 0
			for _, call := range actors.observedCalls() {
				if call == "create" {
					count++
				}
			}
			require.Equal(t, 1, count, "retry must preserve the original Actor and files")
			require.NotNil(t, actors.egressPolicy)
			require.Empty(t, actors.egressPolicy.Rules)
		})
	}
}

func TestSandboxPreparationFailureRequiresClientRetry(t *testing.T) {
	service, ctx, actors := serviceFixture(t)
	actors.readErr = errors.New("temporary lookup failure")
	_, err := service.Create(ctx, createRequest())
	require.Error(t, err)
	require.Empty(t, actors.observedCalls())
	actors.readErr = nil
	startWorker(t, service)
	require.Never(t, func() bool { return len(actors.observedCalls()) != 0 }, 1200*time.Millisecond, 20*time.Millisecond,
		"the expiration worker must not retry ordinary lifecycle work")
	current, err := service.Create(ctx, createRequest())
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Equal(t, []string{"create", "policy", "resume"}, actors.observedCalls())
}

func TestExpirationCleansUpIncompleteCreation(t *testing.T) {
	service, ctx, actors := serviceFixture(t)
	service.config.DefaultTTL = time.Second
	actors.mutationErr = status.Error(codes.Unavailable, "creation response lost")
	_, err := service.Create(ctx, createRequest())
	require.Error(t, err)
	listed, err := service.List(ctx, &apiv1alpha1.ListSandboxesRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Sandboxes, 1)
	instance := listed.Sandboxes[0]
	original, err := service.config.Store.GetSandboxOperation(ctx, instance.Id)
	require.NoError(t, err)
	actors.mutationErr = nil
	startWorker(t, service)
	require.Eventually(t, func() bool {
		current, err := service.Get(ctx, instance.Id)
		return err == nil && current.State == apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED
	}, 5*time.Second, 20*time.Millisecond)
	_, err = service.config.Store.FinishSandboxOperation(ctx, instance.Id, original.ID, original.ExecutorID, "")
	require.ErrorIs(t, err, database.ErrConflict, "expired generation cannot publish success")
	require.Contains(t, actors.observedCalls(), "delete")
}

func TestSandboxConflictingRequestCanRetryAfterActiveAttempt(t *testing.T) {
	service, ctx, actors := serviceFixture(t)
	instance, err := service.Create(ctx, createRequest())
	require.NoError(t, err)
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	actors.mu.Lock()
	actors.suspendStarted, actors.suspendRelease = started, release
	actors.mu.Unlock()
	suspended := make(chan error, 1)
	go func() { _, err := service.Suspend(ctx, instance.Id); suspended <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("suspend did not reach Substrate")
	}
	_, err = service.Resume(ctx, instance.Id)
	require.ErrorIs(t, err, database.ErrConflict)
	unblock()
	require.NoError(t, <-suspended)
	current, err := service.Resume(ctx, instance.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation)
	require.Equal(t, []string{"create", "policy", "resume", "suspend", "resume"}, actors.observedCalls())
}

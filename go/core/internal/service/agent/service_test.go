package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	agentsvc "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

type testSession struct {
	principal auth.Principal
}

func (s testSession) Principal() auth.Principal { return s.principal }

func authedContext(userID string) context.Context {
	return auth.AuthSessionTo(context.Background(), testSession{
		principal: auth.Principal{User: auth.User{ID: userID}},
	})
}

type stubAuthorizer struct {
	err error
}

func (a *stubAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return a.err
}

type stubActorLifecycle struct {
	ensureResult  sandboxbackend.EnsureResult
	ensureErr     error
	suspendErr    error
	state         substrate.SessionActorState
	stateErr      error
	ensureCalls   int
	suspendCalls  int
	getStateCalls int
}

func (s *stubActorLifecycle) EnsureSessionActor(context.Context, *v1alpha3.AgentHarness, string) (sandboxbackend.EnsureResult, error) {
	s.ensureCalls++
	return s.ensureResult, s.ensureErr
}

func (s *stubActorLifecycle) SuspendSessionActor(context.Context, *v1alpha3.AgentHarness, string) error {
	s.suspendCalls++
	return s.suspendErr
}

func (s *stubActorLifecycle) GetSessionActorState(context.Context, *v1alpha3.AgentHarness, string) (substrate.SessionActorState, error) {
	s.getStateCalls++
	return s.state, s.stateErr
}

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	return scheme
}

func TestList_RequiresAuthentication(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	_, err := service.List(context.Background(), agentsvc.ListRequest{})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))
}

func TestList_RejectsWhitespaceNamespace(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	_, err := service.List(authedContext("user-a"), agentsvc.ListRequest{Namespace: " bad "})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
}

func TestList_ReturnsSandboxAgents(t *testing.T) {
	scheme := newScheme(t)
	sandboxAgent := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1", Namespace: "default"},
		Spec:       v1alpha3.SandboxAgentSpec{Type: v1alpha3.AgentType_Declarative},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sandboxAgent).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	views, err := service.List(authedContext("user-a"), agentsvc.ListRequest{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, agentsvc.KindSandboxAgent, views[0].Kind)
	assert.Equal(t, "agent-1", views[0].Ref.Name)
}

func TestList_SkipsUnknownAgentHarnessBackend(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "totally-unknown-backend"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	views, err := service.List(authedContext("user-a"), agentsvc.ListRequest{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, views, "unknown harness backend should be filtered out of listing")
}

func TestGetSandboxAgent_NotFound(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	_, err := service.GetSandboxAgent(authedContext("user-a"), agentsvc.GetRequest{
		Ref: types.NamespacedName{Namespace: "default", Name: "missing"},
	})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
}

func TestGetAgentHarness_UnknownBackendReportsNotFound(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "totally-unknown-backend"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	_, err := service.GetAgentHarness(authedContext("user-a"), agentsvc.GetRequest{
		Ref: types.NamespacedName{Namespace: "default", Name: "harness-1"},
	})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeNotFound))
}

func TestCreateSandboxAgent_RequiresAgent(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	_, err := service.CreateSandboxAgent(authedContext("user-a"), agentsvc.CreateSandboxAgentRequest{Agent: nil})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
}

func TestCreateSandboxAgent_AlreadyExists(t *testing.T) {
	scheme := newScheme(t)
	existing := &v1alpha3.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1", Namespace: "default"},
		Spec:       v1alpha3.SandboxAgentSpec{Type: v1alpha3.AgentType_Declarative},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default",
		agentsvc.WithValidator(func(context.Context, *v1alpha3.SandboxAgent) error { return nil }))

	_, err := service.CreateSandboxAgent(authedContext("user-a"), agentsvc.CreateSandboxAgentRequest{
		Agent: &v1alpha3.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-1", Namespace: "default"},
			Spec:       v1alpha3.SandboxAgentSpec{Type: v1alpha3.AgentType_Declarative},
		},
	})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeAlreadyExists))
}

func TestDeleteAgentHarness_RequiresNamespaceAndName(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	err := service.DeleteAgentHarness(authedContext("user-a"), agentsvc.DeleteRequest{})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
}

func TestEnsureAgentHarnessSessionActor_NoLifecycleConfigured(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "substrate"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	// No WithActorLifecycle option supplied.
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default")

	_, err := service.EnsureAgentHarnessSessionActor(authedContext("user-a"), agentsvc.ActorRequest{
		Ref:       types.NamespacedName{Namespace: "default", Name: "harness-1"},
		SessionID: "sess-1",
	})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
}

func TestEnsureAgentHarnessSessionActor_RequiresSessionID(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "substrate"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	lifecycle := &stubActorLifecycle{}
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default", agentsvc.WithActorLifecycle(lifecycle))

	_, err := service.EnsureAgentHarnessSessionActor(authedContext("user-a"), agentsvc.ActorRequest{
		Ref:       types.NamespacedName{Namespace: "default", Name: "harness-1"},
		SessionID: "   ",
	})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	assert.Equal(t, 0, lifecycle.ensureCalls, "lifecycle should not be invoked when validation fails")
}

func TestEnsureAgentHarnessSessionActor_Success(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "substrate"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	lifecycle := &stubActorLifecycle{
		ensureResult: sandboxbackend.EnsureResult{Handle: sandboxbackend.Handle{ID: "actor-123"}},
	}
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default", agentsvc.WithActorLifecycle(lifecycle))

	actor, err := service.EnsureAgentHarnessSessionActor(authedContext("user-a"), agentsvc.ActorRequest{
		Ref:       types.NamespacedName{Namespace: "default", Name: "harness-1"},
		SessionID: "sess-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "actor-123", actor.ActorID)
	assert.Equal(t, agentsvc.ActorStateRunning, actor.State)
	assert.Equal(t, 1, lifecycle.ensureCalls)
}

func TestSuspendAgentHarnessSessionActor_PropagatesLifecycleError(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "substrate"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	lifecycle := &stubActorLifecycle{suspendErr: newTestError()}
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default", agentsvc.WithActorLifecycle(lifecycle))

	_, err := service.SuspendAgentHarnessSessionActor(authedContext("user-a"), agentsvc.ActorRequest{
		Ref:       types.NamespacedName{Namespace: "default", Name: "harness-1"},
		SessionID: "sess-1",
	})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	assert.Equal(t, 1, lifecycle.suspendCalls)
}

func TestGetAgentHarnessSessionActor_MapsSuspendedState(t *testing.T) {
	scheme := newScheme(t)
	harness := &v1alpha3.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness-1", Namespace: "default"},
		Spec:       v1alpha3.AgentHarnessSpec{Backend: "substrate"},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(harness).Build()
	lifecycle := &stubActorLifecycle{state: substrate.SessionActorStateSuspended}
	service := agentsvc.NewService(kube, &stubAuthorizer{}, "default", agentsvc.WithActorLifecycle(lifecycle))

	actor, err := service.GetAgentHarnessSessionActor(authedContext("user-a"), agentsvc.ActorRequest{
		Ref:       types.NamespacedName{Namespace: "default", Name: "harness-1"},
		SessionID: "sess-1",
	})
	require.NoError(t, err)
	assert.Equal(t, agentsvc.ActorStateSuspended, actor.State)
}

func TestAuthorizationDenied_ReturnsPermissionDenied(t *testing.T) {
	kube := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	service := agentsvc.NewService(kube, &stubAuthorizer{err: newTestError()}, "default")

	_, err := service.List(authedContext("user-a"), agentsvc.ListRequest{})
	require.Error(t, err)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
}

func newTestError() error {
	return &testError{"boom"}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

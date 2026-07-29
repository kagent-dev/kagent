package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/controller/reconciler"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	agentservice "github.com/kagent-dev/kagent/go/core/internal/service/agent"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	pkgauth "github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type denyAuthorizer struct{}

func (denyAuthorizer) Check(context.Context, pkgauth.Principal, pkgauth.Verb, pkgauth.Resource) error {
	return errors.New("denied")
}

type fakeActorLifecycle struct {
	ensureCalls  int
	suspendCalls int
	state        substrate.SessionActorState
	sessionID    string
}

func (f *fakeActorLifecycle) EnsureSessionActor(_ context.Context, _ *v1alpha2.AgentHarness, sessionID string) (sandboxbackend.EnsureResult, error) {
	f.ensureCalls++
	f.sessionID = sessionID
	return sandboxbackend.EnsureResult{Handle: sandboxbackend.Handle{ID: "actor-1"}}, nil
}

func (f *fakeActorLifecycle) SuspendSessionActor(context.Context, *v1alpha2.AgentHarness, string) error {
	f.suspendCalls++
	return nil
}

func (f *fakeActorLifecycle) GetSessionActorState(context.Context, *v1alpha2.AgentHarness, string) (substrate.SessionActorState, error) {
	return f.state, nil
}

func TestServiceReads(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))

	newService := func(authorizer pkgauth.Authorizer, objects ...ctrlclient.Object) (*agentservice.Service, context.Context) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		service := agentservice.NewService(kubeClient, authorizer, "default")
		ctx := pkgauth.AuthSessionTo(context.Background(), &authimpl.SimpleSession{
			P: pkgauth.Principal{User: pkgauth.User{ID: "test-user"}},
		})
		return service, ctx
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "model", Namespace: "default"},
		Spec: v1alpha2.ModelConfigSpec{
			Provider: v1alpha2.ModelProviderOpenAI,
			Model:    "gpt-4.1",
		},
	}
	regular := &v1alpha2.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
		Spec: v1alpha2.AgentSpec{
			Type: v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{
				ModelConfig: "model",
				Tools:       []*v1alpha2.Tool{{Type: v1alpha2.ToolProviderType_Agent}},
			},
		},
		Status: v1alpha2.AgentStatus{Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue, Reason: reconciler.AgentReadyReasonDeploymentReady},
			{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "AnyReason"},
		}},
	}
	sandbox := &v1alpha2.SandboxAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "shared", Namespace: "default"},
		Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
			Type:        v1alpha2.AgentType_Declarative,
			Declarative: &v1alpha2.DeclarativeAgentSpec{ModelConfig: "model"},
		}},
	}
	harness := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "harness", Namespace: "default"},
		Spec: v1alpha2.AgentHarnessSpec{
			Backend:        v1alpha2.AgentHarnessBackendOpenClaw,
			Description:    "Harness",
			ModelConfigRef: "model",
		},
		Status: v1alpha2.AgentHarnessStatus{Conditions: []metav1.Condition{
			{Type: v1alpha2.AgentHarnessConditionTypeReady, Status: metav1.ConditionTrue},
			{Type: v1alpha2.AgentHarnessConditionTypeAccepted, Status: metav1.ConditionTrue},
		}},
	}
	unknownHarness := &v1alpha2.AgentHarness{
		ObjectMeta: metav1.ObjectMeta{Name: "unknown", Namespace: "default"},
		Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendType("unknown")},
	}

	t.Run("list merges supported kinds and enriches views", func(t *testing.T) {
		service, ctx := newService(&authimpl.NoopAuthorizer{}, modelConfig, regular, sandbox, harness, unknownHarness)

		views, err := service.List(ctx, agentservice.ListRequest{Namespace: "default"})
		require.NoError(t, err)
		require.Len(t, views, 3)

		byKey := map[string]agentservice.View{}
		for _, view := range views {
			byKey[string(view.Kind)+"/"+view.Ref.Name] = view
		}
		regularView := byKey[string(agentservice.KindAgent)+"/shared"]
		assert.Equal(t, "default__NS__shared", regularView.ID)
		assert.Equal(t, v1alpha2.ModelProviderOpenAI, regularView.ModelProvider)
		assert.Equal(t, "gpt-4.1", regularView.Model)
		assert.Equal(t, types.NamespacedName{Namespace: "default", Name: "model"}, regularView.ModelConfigRef)
		assert.Len(t, regularView.Tools, 1)
		assert.True(t, regularView.DeploymentReady)
		assert.True(t, regularView.Accepted)
		assert.Equal(t, v1alpha2.WorkloadModeDeployment, regularView.WorkloadMode)

		sandboxView := byKey[string(agentservice.KindSandboxAgent)+"/shared"]
		assert.Equal(t, v1alpha2.WorkloadModeSandbox, sandboxView.WorkloadMode)
		harnessView := byKey[string(agentservice.KindAgentHarness)+"/harness"]
		require.NotNil(t, harnessView.Harness)
		assert.Equal(t, v1alpha2.AgentHarnessBackendOpenClaw, harnessView.Harness.Backend)
		assert.Equal(t, "/api/agentharnesses/default/harness/acp", harnessView.Harness.ACPPath)
		assert.True(t, harnessView.DeploymentReady)
		assert.True(t, harnessView.Accepted)
	})

	t.Run("same name remains isolated by kind", func(t *testing.T) {
		service, ctx := newService(&authimpl.NoopAuthorizer{}, modelConfig, regular, sandbox)

		regularView, err := service.GetAgent(ctx, agentservice.GetRequest{
			Ref: types.NamespacedName{Namespace: "default", Name: "shared"},
		})
		require.NoError(t, err)
		assert.Equal(t, agentservice.KindAgent, regularView.Kind)

		sandboxView, err := service.GetSandboxAgent(ctx, agentservice.GetRequest{
			Ref: types.NamespacedName{Namespace: "default", Name: "shared"},
		})
		require.NoError(t, err)
		assert.Equal(t, agentservice.KindSandboxAgent, sandboxView.Kind)
	})

	t.Run("list keeps partial row when model config is missing", func(t *testing.T) {
		missingModel := regular.DeepCopy()
		missingModel.Name = "partial"
		service, ctx := newService(&authimpl.NoopAuthorizer{}, missingModel)

		views, err := service.List(ctx, agentservice.ListRequest{})
		require.NoError(t, err)
		require.Len(t, views, 1)
		assert.Empty(t, views[0].Model)

		_, err = service.GetAgent(ctx, agentservice.GetRequest{Ref: types.NamespacedName{Namespace: "default", Name: "partial"}})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	})

	t.Run("invalid namespace is rejected", func(t *testing.T) {
		service, ctx := newService(&authimpl.NoopAuthorizer{})
		_, err := service.List(ctx, agentservice.ListRequest{Namespace: " bad "})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	})

	t.Run("authorization is required", func(t *testing.T) {
		service, ctx := newService(denyAuthorizer{}, regular)
		_, err := service.List(ctx, agentservice.ListRequest{})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))

		service, _ = newService(&authimpl.NoopAuthorizer{}, regular)
		_, err = service.List(context.Background(), agentservice.ListRequest{})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))
	})
}

func TestServiceMutationsAndLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha2.AddToScheme(scheme))
	ctx := pkgauth.AuthSessionTo(context.Background(), &authimpl.SimpleSession{
		P: pkgauth.Principal{User: pkgauth.User{ID: "test-user"}},
	})

	t.Run("regular agent create update and delete", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		validated := 0
		service := agentservice.NewService(
			kubeClient,
			&authimpl.NoopAuthorizer{},
			"default",
			agentservice.WithValidator(func(context.Context, v1alpha2.AgentObject) error {
				validated++
				return nil
			}),
		)

		created, err := service.CreateAgent(ctx, agentservice.CreateAgentRequest{Agent: &v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "assistant"},
			Spec:       v1alpha2.AgentSpec{Type: v1alpha2.AgentType_BYO, BYO: &v1alpha2.BYOAgentSpec{}},
		}})
		require.NoError(t, err)
		assert.Equal(t, "default", created.Ref.Namespace)
		assert.Equal(t, 1, validated)

		stored := &v1alpha2.Agent{}
		require.NoError(t, kubeClient.Get(ctx, created.Ref, stored))
		stored.Labels = map[string]string{"preserved": "true"}
		require.NoError(t, kubeClient.Update(ctx, stored))

		updated, err := service.UpdateAgent(ctx, agentservice.UpdateAgentRequest{
			Ref: created.Ref,
			Agent: &v1alpha2.Agent{
				ObjectMeta: metav1.ObjectMeta{Name: "assistant", Namespace: "default"},
				Spec:       v1alpha2.AgentSpec{Type: v1alpha2.AgentType_Declarative, Declarative: &v1alpha2.DeclarativeAgentSpec{}},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, v1alpha2.AgentType_Declarative, updated.Resource.(*v1alpha2.Agent).Spec.Type)
		assert.Equal(t, "true", updated.Resource.(*v1alpha2.Agent).Labels["preserved"])
		assert.Equal(t, 2, validated)

		require.NoError(t, service.DeleteAgent(ctx, agentservice.DeleteRequest{Ref: created.Ref}))
		err = kubeClient.Get(ctx, created.Ref, &v1alpha2.Agent{})
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("sandbox update requires ref and body metadata to match", func(t *testing.T) {
		sandboxAgent := &v1alpha2.SandboxAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "sandbox", Namespace: "default"},
			Spec: v1alpha2.SandboxAgentSpec{AgentSpec: v1alpha2.AgentSpec{
				Type:        v1alpha2.AgentType_Declarative,
				Declarative: &v1alpha2.DeclarativeAgentSpec{},
			}},
		}
		regularAgent := &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "sandbox", Namespace: "default"}}
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sandboxAgent, regularAgent).Build()
		service := agentservice.NewService(
			kubeClient,
			&authimpl.NoopAuthorizer{},
			"default",
			agentservice.WithValidator(func(context.Context, v1alpha2.AgentObject) error { return nil }),
		)

		_, err := service.UpdateSandboxAgent(ctx, agentservice.UpdateSandboxAgentRequest{
			Ref: types.NamespacedName{Namespace: "default", Name: "sandbox"},
			Agent: &v1alpha2.SandboxAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"},
			},
		})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))

		require.NoError(t, service.DeleteSandboxAgent(ctx, agentservice.DeleteRequest{
			Ref: types.NamespacedName{Namespace: "default", Name: "sandbox"},
		}))
		require.NoError(t, kubeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "sandbox"}, &v1alpha2.Agent{}))
	})

	t.Run("harness create and lifecycle", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		lifecycle := &fakeActorLifecycle{state: substrate.SessionActorStateRunning}
		service := agentservice.NewService(
			kubeClient,
			&authimpl.NoopAuthorizer{},
			"default",
			agentservice.WithActorLifecycle(lifecycle),
		)

		created, err := service.CreateAgentHarness(ctx, agentservice.CreateAgentHarnessRequest{AgentHarness: &v1alpha2.AgentHarness{
			ObjectMeta: metav1.ObjectMeta{Name: "harness"},
			Spec:       v1alpha2.AgentHarnessSpec{Backend: v1alpha2.AgentHarnessBackendOpenClaw},
		}})
		require.NoError(t, err)

		actor, err := service.EnsureAgentHarnessSessionActor(ctx, agentservice.ActorRequest{Ref: created.Ref, SessionID: " session-1 "})
		require.NoError(t, err)
		assert.Equal(t, "actor-1", actor.ActorID)
		assert.Equal(t, "session-1", actor.SessionID)
		assert.Equal(t, "session-1", lifecycle.sessionID)
		assert.Equal(t, agentservice.ActorStateRunning, actor.State)
		assert.Equal(t, 1, lifecycle.ensureCalls)

		actor, err = service.GetAgentHarnessSessionActor(ctx, agentservice.ActorRequest{Ref: created.Ref, SessionID: "session-1"})
		require.NoError(t, err)
		assert.Equal(t, agentservice.ActorStateRunning, actor.State)

		actor, err = service.SuspendAgentHarnessSessionActor(ctx, agentservice.ActorRequest{Ref: created.Ref, SessionID: "session-1"})
		require.NoError(t, err)
		assert.Equal(t, agentservice.ActorStateSuspended, actor.State)
		assert.Equal(t, 1, lifecycle.suspendCalls)
	})

	t.Run("missing validator and lifecycle inputs are reported", func(t *testing.T) {
		kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		service := agentservice.NewService(kubeClient, &authimpl.NoopAuthorizer{}, "default")

		_, err := service.CreateAgent(ctx, agentservice.CreateAgentRequest{Agent: &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "agent"}}})
		require.NoError(t, err)

		_, err = service.EnsureAgentHarnessSessionActor(ctx, agentservice.ActorRequest{})
		require.Error(t, err)
		assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
	})
}

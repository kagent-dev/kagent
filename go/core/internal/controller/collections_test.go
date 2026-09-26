package controller

import (
	"context"
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconciliationCollectionsCompileAndObserveRevision(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	template := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid", Labels: map[string]string{"runtime": "python"}},
		Spec: kagentv1alpha3.AgentTemplateSpec{
			ModelConfig:  &corev1.LocalObjectReference{Name: "model"},
			SystemPrompt: "help",
		},
	}
	matchingHarness := harness("team-a", "kagent", map[string]string{"runtime": "python"})
	matchingHarness.UID = "harness-uid"
	matchingHarness.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
	matchingHarness.Spec.Workload.Image = "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	matchingHarness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{
		WorkerPoolRef:  corev1.LocalObjectReference{Name: "default"},
		SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
	}
	modelConfigs := krt.NewStaticCollection(nil, []*kagentv1alpha3.ModelConfig{{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5"}}}, opts.WithName("ModelConfigs")...)
	templates := krt.NewStaticCollection[*kagentv1alpha3.AgentTemplate](nil, nil, opts.WithName("AgentTemplates")...)
	mock := krttest.NewMock(t, []any{
		matchingHarness,
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}},
	})

	collections := Collections{
		AgentTemplates:           templates,
		Harnesses:                krttest.GetMockCollection[*kagentv1alpha3.Harness](mock),
		ModelConfigs:             modelConfigs,
		RemoteMCPServers:         krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
		ConfigMaps:               krttest.GetMockCollection[*corev1.ConfigMap](mock),
		Secrets:                  krttest.GetMockCollection[*corev1.Secret](mock),
		WorkerPools:              krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
		AgentRuntimeObservations: krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...),
	}
	collections.ModelConfigStatuses, collections.ResolvedModelConfigs = newModelConfigReconciliations(collections.ModelConfigs, collections.ConfigMaps, collections.Secrets, opts)
	collections.Agents = krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, matchingHarness)}, opts.WithName("Agents")...)
	collections.Reconciliations = newAgentReconciliations(
		collections.Agents, v2translator.Collections{
			Harnesses: collections.Harnesses, AgentTemplates: collections.AgentTemplates, ResolvedModelConfigs: collections.ResolvedModelConfigs,
			RemoteMCPServers: collections.RemoteMCPServers, ConfigMaps: collections.ConfigMaps,
			Secrets: collections.Secrets, WorkerPools: collections.WorkerPools,
		}, collections.AgentRuntimeObservations, opts,
	)
	collections.AgentStatuses = newAgentStatuses(collections.Agents, collections.Reconciliations, opts)

	// The Agent informer can observe a new Agent before the template informer
	// observes its reference. Adding the template must clear the failure without
	// changing or re-enqueuing the Agent explicitly.
	waitFor(t, func() bool {
		updates := collections.AgentStatuses.List()
		if len(updates) != 1 {
			return false
		}
		condition := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionResolvedRefs)
		return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "ReferenceResolutionFailed"
	})
	templates.UpdateObject(template)

	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		return len(states) == 1 && states[0].Failure == nil && states[0].DesiredActorTemplate != nil
	})
	state := collections.Reconciliations.List()[0]
	if state.ObservedActorTemplate != nil {
		t.Fatal("ActorTemplate was observed before it existed")
	}
	waitFor(t, func() bool {
		updates := collections.AgentStatuses.List()
		if len(updates) != 1 {
			return false
		}
		ready := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionReady)
		return ready != nil && ready.Status == metav1.ConditionFalse
	})

	observed := proto.CloneOf(state.DesiredActorTemplate)
	observed.Metadata.Uid = "actor-template-uid"
	observed.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}}}
	store := &fakeRuntimeRevisionStore{}
	reconciler := &Reconciler{
		collections: collections, templates: &fakeActorTemplates{template: observed}, store: store,
	}
	require.NoError(t, reconciler.reconcileAgent(t.Context(), state.ResourceName()))
	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		updates := collections.AgentStatuses.List()
		if len(states) != 1 || states[0].ObservedActorTemplate == nil || len(updates) != 1 {
			return false
		}
		ready := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionReady)
		return ready != nil && ready.Status == metav1.ConditionTrue && updates[0].Status.LatestSuccessfulRevision == state.RevisionID.String()
	})

	t.Run("deleting revision becomes pending and retries", func(t *testing.T) {
		store.pairErr = database.ErrObjectDeleting
		require.NoError(t, reconciler.reconcileAgent(t.Context(), state.ResourceName()))
		waitFor(t, func() bool {
			pending := collections.Reconciliations.GetKey(state.ResourceName())
			updates := collections.AgentStatuses.List()
			if pending == nil || pending.ObservedActorTemplate != nil || pending.Failure != nil || len(updates) != 1 {
				return false
			}
			return apimeta.IsStatusConditionFalse(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionReady)
		})

		pollCtx, cancelPoll := context.WithCancel(t.Context())
		queued := make(chan string, 1)
		reconciler.agents = controllers.NewQueue("test-deleting-revision", controllers.WithGenericReconciler(func(item any) error {
			select {
			case queued <- item.(string):
			case <-pollCtx.Done():
			}
			return nil
		}))
		go reconciler.agents.Run(pollCtx.Done())
		go reconciler.pollPendingTemplates(pollCtx.Done())
		t.Cleanup(func() {
			cancelPoll()
			require.NoError(t, reconciler.agents.WaitForClose(time.Second))
		})
		// Retry more than once: the poll must keep working after the KRT event.
		for range 2 {
			select {
			case key := <-queued:
				require.Equal(t, state.ResourceName(), key)
				require.NoError(t, reconciler.reconcileAgent(t.Context(), key))
			case <-time.After(5 * time.Second):
				t.Fatal("pending pair was not requeued while awaiting deletion")
			}
		}
		store.pairErr = nil
		require.NoError(t, reconciler.reconcileAgent(t.Context(), state.ResourceName()))
		waitFor(t, func() bool {
			updates := collections.AgentStatuses.List()
			return len(updates) == 1 && true &&
				apimeta.IsStatusConditionTrue(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionReady)
		})
	})

	modelConfigs.UpdateObject(&kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5.1"}})
	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		return len(states) == 1 && states[0].RevisionID != state.RevisionID && states[0].ObservedActorTemplate == nil
	})
}

func TestReconciliationWorkerPoolSandboxClass(t *testing.T) {
	for _, harnessType := range []v2translator.HarnessType{
		v2translator.HarnessTypeKagent, v2translator.HarnessTypeCodex, v2translator.HarnessTypeClaude, v2translator.HarnessTypeBYO,
	} {
		t.Run(string(harnessType), func(t *testing.T) {
			stop := make(chan struct{})
			t.Cleanup(func() { close(stop) })
			opts := krt.NewOptionsBuilder(stop, "test-sandbox", nil)
			template := &kagentv1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"},
				Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "help"},
			}
			runtimeHarness := harness("team-a", string(harnessType), nil)
			runtimeHarness.UID = "harness-uid"
			runtimeHarness.Spec.Workload.Image = "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			runtimeHarness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "selected"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
			}
			responses := kagentv1alpha3.OpenAIAPIFormatResponses
			model := &kagentv1alpha3.ModelConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model", UID: "model-uid"},
				Spec: kagentv1alpha3.ModelConfigSpec{
					Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5", APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
					OpenAI: &kagentv1alpha3.OpenAIConfig{APIFormat: &responses},
				},
			}
			switch harnessType {
			case v2translator.HarnessTypeKagent:
				runtimeHarness.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
			case v2translator.HarnessTypeCodex:
				runtimeHarness.Spec.Codex = &kagentv1alpha3.CodexHarness{}
			case v2translator.HarnessTypeClaude:
				runtimeHarness.Spec.Claude = &kagentv1alpha3.ClaudeHarness{}
				model.Spec.Provider, model.Spec.Model, model.Spec.OpenAI = kagentv1alpha3.ModelProviderAnthropic, "claude-sonnet-4-5", nil
			case v2translator.HarnessTypeBYO:
				runtimeHarness.Spec.BYO = &kagentv1alpha3.BYOHarness{}
				runtimeHarness.Spec.Workload.Command = []string{"/agent"}
				template.Spec.ModelConfig = nil
			}
			mock := krttest.NewMock(t, []any{
				template, runtimeHarness, model,
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model-auth"}, Data: map[string][]byte{"api-key": []byte("secret")}},
			})
			templates := krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock)
			agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, runtimeHarness)}, opts.WithName("Agents")...)
			configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
			secrets := krttest.GetMockCollection[*corev1.Secret](mock)
			_, resolvedModels := newModelConfigReconciliations(krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock), configMaps, secrets, opts)
			workerPools := krt.NewStaticCollection(nil, []*atev1alpha1.WorkerPool{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "selected"}, Spec: atev1alpha1.WorkerPoolSpec{SandboxClass: atev1alpha1.SandboxClassMicroVM}},
				{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "unselected"}, Spec: atev1alpha1.WorkerPoolSpec{SandboxClass: atev1alpha1.SandboxClassMicroVM}},
			}, opts.WithName("WorkerPools")...)
			observations := krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...)
			reconciliations := newAgentReconciliations(agents, v2translator.Collections{
				Harnesses: krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), AgentTemplates: templates, ResolvedModelConfigs: resolvedModels,
				RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
				ConfigMaps:       configMaps, Secrets: secrets, WorkerPools: workerPools,
			}, observations, opts)
			key := "team-a/assistant"
			waitFor(t, func() bool {
				state := reconciliations.GetKey(key)
				return state != nil && state.Failure != nil
			})
			missingPool := &ReconciliationFailure{
				Condition: kagentv1alpha3.AgentConditionResolvedRefs, Reason: "WorkerPoolNotFound", Message: `WorkerPool "team-a/selected" not found`,
			}
			require.Equal(t, missingPool, reconciliations.GetKey(key).Failure)
			require.Nil(t, reconciliations.GetKey(key).Revision, "missing capacity must fail compilation")

			workerPools.UpdateObject(&atev1alpha1.WorkerPool{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "selected"},
			})
			waitFor(t, func() bool {
				state := reconciliations.GetKey(key)
				return state != nil && state.Failure == nil && state.DesiredActorTemplate != nil && state.Revision.SandboxClass == ""
			})
			baseline := reconciliations.GetKey(key)
			gvisorRevision := baseline.RevisionID
			require.False(t, gvisorRevision.IsZero())
			gvisorTemplate := proto.CloneOf(baseline.DesiredActorTemplate)
			observed := proto.CloneOf(gvisorTemplate)
			observed.Metadata.Uid = "actor-template-uid"
			observations.UpdateObject(AgentRuntimeObservation{
				Namespace: template.Namespace, AgentName: template.Name, RevisionID: gvisorRevision, Template: observed,
			})
			waitFor(t, func() bool { return reconciliations.GetKey(key).ObservedActorTemplate != nil })

			for _, step := range []struct {
				name    string
				class   atev1alpha1.SandboxClass
				remove  bool
				failure *ReconciliationFailure
			}{
				{name: "default"},
				{name: "explicit gvisor", class: atev1alpha1.SandboxClassGvisor},
				{name: "microvm", class: atev1alpha1.SandboxClassMicroVM},
				{name: "back to gvisor", class: atev1alpha1.SandboxClassGvisor},
				{name: "unsupported", class: "unsupported", failure: &ReconciliationFailure{
					Condition: kagentv1alpha3.AgentConditionCompatible, Reason: "RevisionInvalid", Message: `unsupported sandbox class "unsupported"`,
				}},
				{name: "deleted", remove: true, failure: missingPool},
				{name: "recreated"},
			} {
				t.Run(step.name, func(t *testing.T) {
					if step.remove {
						workerPools.DeleteObject("team-a/selected")
					} else {
						workerPools.UpdateObject(&atev1alpha1.WorkerPool{
							ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "selected"},
							Spec:       atev1alpha1.WorkerPoolSpec{SandboxClass: step.class},
						})
					}
					waitFor(t, func() bool {
						state := reconciliations.GetKey(key)
						if step.failure != nil {
							return state != nil && state.Failure != nil && *state.Failure == *step.failure
						}
						return state != nil && state.Failure == nil && state.DesiredActorTemplate != nil && state.Revision.SandboxClass == step.class
					})
					state := reconciliations.GetKey(key)
					if step.failure != nil {
						require.True(t, state.RevisionID.IsZero())
						require.Nil(t, state.DesiredActorTemplate)
						require.Nil(t, state.ObservedActorTemplate)
						pairStatus := statusForAgent(*state, 1, gvisorRevision.String())
						require.Equal(t, gvisorRevision.String(), pairStatus.LatestSuccessfulRevision)
						condition := apimeta.FindStatusCondition(pairStatus.Conditions, step.failure.Condition)
						require.NotNil(t, condition)
						require.Equal(t, metav1.ConditionFalse, condition.Status)
						require.Equal(t, step.failure.Reason, condition.Reason)
						return
					}
					expected := proto.CloneOf(gvisorTemplate)
					expected.Metadata.Name = state.DesiredActorTemplate.GetMetadata().GetName()
					if step.class == atev1alpha1.SandboxClassMicroVM {
						require.NotEqual(t, gvisorRevision, state.RevisionID)
						require.NotEqual(t, gvisorTemplate.GetMetadata().GetName(), expected.Metadata.Name)
						require.Nil(t, state.ObservedActorTemplate, "a gVisor observation must not satisfy a MicroVM revision")
						expected.SandboxConfig = &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_MICROVM, ConfigName: "microvm"}
					} else {
						require.Equal(t, gvisorRevision, state.RevisionID)
						require.Equal(t, gvisorTemplate.GetMetadata().GetName(), expected.Metadata.Name)
						expected.SandboxConfig = &ateapipb.SandboxConfig{SandboxClass: ateapipb.SandboxClass_SANDBOX_CLASS_GVISOR, ConfigName: "gvisor-default"}
					}
					require.True(t, proto.Equal(expected, state.DesiredActorTemplate), "sandbox selection must preserve the rest of the ActorTemplate")
					require.Equal(t, map[string]string{"kagent.dev/worker-pool": "selected"}, state.DesiredActorTemplate.GetWorkerSelector().GetMatchLabels())
				})
			}
		})
	}
}

func TestClaudeReconciliationCompilesActorTemplate(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test-claude", nil)

	template := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid", Labels: map[string]string{"runtime": "claude"}},
		Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "help"},
	}
	claudeHarness := harness("team-a", "claude", map[string]string{"runtime": "claude"})
	claudeHarness.UID = "harness-uid"
	claudeHarness.Spec.Claude = &kagentv1alpha3.ClaudeHarness{}
	claudeHarness.Spec.Workload.Image = "example.com/claude@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	claudeHarness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{
		WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
	}
	model := &kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model", UID: "model-uid"}, Spec: kagentv1alpha3.ModelConfigSpec{
		Provider: kagentv1alpha3.ModelProviderAnthropic, Model: "claude-sonnet-4-5", APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
	}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model-auth", UID: "secret-uid"}, Data: map[string][]byte{"api-key": []byte("secret")}}
	mock := krttest.NewMock(t, []any{
		template,
		claudeHarness,
		model,
		secret,
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}},
	})
	templates := krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock)
	agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, claudeHarness)}, opts.WithName("Agents")...)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	secrets := krttest.GetMockCollection[*corev1.Secret](mock)
	_, resolvedModelConfigs := newModelConfigReconciliations(
		krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock), configMaps, secrets, opts,
	)
	reconciliations := newAgentReconciliations(
		agents, v2translator.Collections{
			Harnesses: krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), AgentTemplates: templates, ResolvedModelConfigs: resolvedModelConfigs,
			RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
			ConfigMaps:       configMaps, Secrets: secrets,
			WorkerPools: krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
		}, krttest.GetMockCollection[AgentRuntimeObservation](mock), opts,
	)
	waitFor(t, func() bool {
		states := reconciliations.List()
		return len(states) == 1 && states[0].Failure == nil && states[0].DesiredActorTemplate != nil
	})
	state := reconciliations.List()[0]
	if state.Revision == nil || state.Revision.Environment[0].Name != "ANTHROPIC_API_KEY" || state.Revision.Environment[0].Value != v2translator.CredentialPlaceholder {
		t.Fatalf("Claude revision environment = %#v", state.Revision)
	}
	if state.DesiredActorTemplate.GetContainers()[0].GetReadyz().GetHttpGet().GetPort() != 8081 {
		t.Fatalf("Claude ActorTemplate readiness = %#v", state.DesiredActorTemplate.GetContainers()[0].GetReadyz())
	}
}

func TestCodexReconciliationCompilesActorTemplate(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test-codex", nil)
	responses := kagentv1alpha3.OpenAIAPIFormatResponses
	template := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid", Labels: map[string]string{"runtime": "codex"}},
		Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "help"},
	}
	codexHarness := harness("team-a", "codex", map[string]string{"runtime": "codex"})
	codexHarness.UID = "harness-uid"
	codexHarness.Spec.Codex = &kagentv1alpha3.CodexHarness{}
	codexHarness.Spec.Workload.Image = "example.com/codex@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	codexHarness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{
		WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
	}
	model := &kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model", UID: "model-uid"}, Spec: kagentv1alpha3.ModelConfigSpec{
		Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5.2-codex", APIKeySecret: "model-auth", APIKeySecretKey: "api-key",
		OpenAI: &kagentv1alpha3.OpenAIConfig{APIFormat: &responses},
	}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model-auth", UID: "secret-uid"}, Data: map[string][]byte{"api-key": []byte("secret")}}
	mock := krttest.NewMock(t, []any{
		template,
		codexHarness,
		model,
		secret,
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}},
	})
	templates := krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock)
	agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, codexHarness)}, opts.WithName("Agents")...)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	secrets := krttest.GetMockCollection[*corev1.Secret](mock)
	_, resolvedModelConfigs := newModelConfigReconciliations(
		krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock), configMaps, secrets, opts,
	)
	reconciliations := newAgentReconciliations(
		agents, v2translator.Collections{
			Harnesses: krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), AgentTemplates: templates, ResolvedModelConfigs: resolvedModelConfigs,
			RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
			ConfigMaps:       configMaps, Secrets: secrets,
			WorkerPools: krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
		}, krttest.GetMockCollection[AgentRuntimeObservation](mock), opts,
	)
	waitFor(t, func() bool {
		states := reconciliations.List()
		return len(states) == 1 && states[0].Failure == nil && states[0].DesiredActorTemplate != nil
	})
	state := reconciliations.List()[0]
	if state.Revision == nil || state.Revision.Environment[0].Name != "OPENAI_API_KEY" || state.Revision.Environment[0].Value != v2translator.CredentialPlaceholder {
		t.Fatalf("Codex revision environment = %#v", state.Revision)
	}
	if state.DesiredActorTemplate.GetContainers()[0].GetReadyz().GetHttpGet().GetPort() != 8081 {
		t.Fatalf("Codex ActorTemplate readiness = %#v", state.DesiredActorTemplate.GetContainers()[0].GetReadyz())
	}
}

func TestReconciliationTracksSharedAgentTemplate(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	child := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "child", Labels: map[string]string{"runtime": "python"}},
		Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "before"},
	}
	root := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "root", Labels: map[string]string{"runtime": "python"}},
		Spec: kagentv1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: "model"},
			Tools: []kagentv1alpha3.ToolBinding{{SubAgent: &kagentv1alpha3.SubAgentToolBinding{
				Name: "child", Description: "delegate", TemplateRef: &corev1.LocalObjectReference{Name: child.Name},
			}}},
		},
	}
	harness := harness("team-a", "kagent", map[string]string{"runtime": "python"})
	harness.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
	harness.Spec.Workload.Image = "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	harness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"}}
	templates := krt.NewStaticCollection(nil, []*kagentv1alpha3.AgentTemplate{root, child}, opts.WithName("AgentTemplates")...)
	mock := krttest.NewMock(t, []any{
		harness,
		&kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5"}},
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}},
	})
	agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(root, harness)}, opts.WithName("Agents")...)
	modelConfigs := krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	secrets := krttest.GetMockCollection[*corev1.Secret](mock)
	_, resolvedModelConfigs := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)
	reconciliations := newAgentReconciliations(
		agents, v2translator.Collections{
			Harnesses: krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), AgentTemplates: templates, ResolvedModelConfigs: resolvedModelConfigs,
			RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
			ConfigMaps:       configMaps, Secrets: secrets,
			WorkerPools: krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
		}, krttest.GetMockCollection[AgentRuntimeObservation](mock), opts,
	)
	var initial string
	waitFor(t, func() bool {
		for _, state := range reconciliations.List() {
			if state.Agent.Name == root.Name && state.Failure == nil {
				initial = state.RevisionID.String()
				return true
			}
		}
		return false
	})
	updated := child.DeepCopy()
	updated.Spec.SystemPrompt = "after"
	templates.UpdateObject(updated)
	waitFor(t, func() bool {
		for _, state := range reconciliations.List() {
			if state.Agent.Name == root.Name {
				return state.Failure == nil && state.RevisionID.String() != initial
			}
		}
		return false
	})
}

func harness(namespace, name string, matchLabels map[string]string) *kagentv1alpha3.Harness {
	return &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       kagentv1alpha3.HarnessSpec{},
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func testAgent(template *kagentv1alpha3.AgentTemplate, harness *kagentv1alpha3.Harness) *kagentv1alpha3.Agent {
	return &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: template.Name, UID: template.UID}, Spec: kagentv1alpha3.AgentSpec{
		TemplateRef: &corev1.LocalObjectReference{Name: template.Name}, HarnessRef: &corev1.LocalObjectReference{Name: harness.Name},
	}}
}

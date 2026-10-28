// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestWorkspacePreparationRecoversAfterCredentialCorrection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opts := krt.NewOptionsBuilder(t.Context().Done(), "workspace-recovery", nil)
		behavior := &kagentv1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"},
			Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &corev1.LocalObjectReference{Name: "model"}, SystemPrompt: "help"},
		}
		runtime := harness("team-a", "kagent", nil)
		runtime.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
		runtime.Spec.Workload.Image = new("example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		runtime.Spec.Substrate = kagentv1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"}}
		agent := testAgent(behavior, runtime)
		agent.Spec.Workspace = &kagentv1alpha3.WorkspaceSpec{Git: &kagentv1alpha3.GitWorkspaceSource{
			URL: "https://github.com/org/private.git", Ref: "main",
			CredentialRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "repo"}, Key: "authorization"},
		}}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "repo"}, Data: map[string][]byte{"authorization": []byte("invalid-token")}}
		secrets := krt.NewStaticCollection(nil, []*corev1.Secret{secret}, opts.WithName("Secrets")...)
		agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{agent}, opts.WithName("Agents")...)
		mock := krttest.NewMock(t, []any{
			behavior, runtime,
			&atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}},
			&kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5"}},
		})
		configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
		_, models := newModelConfigReconciliations(krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock), configMaps, secrets, opts)
		observations := krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("Observations")...)
		states := newAgentReconciliations(agents, translator.Collections{
			AgentTemplates:       krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock),
			Harnesses:            krttest.GetMockCollection[*kagentv1alpha3.Harness](mock),
			ResolvedModelConfigs: models, ConfigMaps: configMaps, Secrets: secrets,
			WorkerPools:      krttest.GetMockCollection[*atev1.WorkerPool](mock),
			RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
		}, observations, opts, testRuntimeDefaults(), nil)
		synctest.Wait()
		initial := states.GetKey("team-a/assistant")
		require.NotNil(t, initial)
		require.Nil(t, initial.CompilationFailure)
		require.NotNil(t, initial.Target)
		failed := proto.CloneOf(initial.Target.ActorTemplate)
		failed.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "GoldenActorCrashed: golden actor crashed before its snapshot was taken"}}
		failed.Metadata.Uid = "failed-uid"
		observations.UpdateObject(AgentRuntimeObservation{Namespace: agent.Namespace, AgentName: agent.Name, RevisionID: initial.Target.RevisionID, Template: failed})
		synctest.Wait()
		require.True(t, states.GetKey(initial.ResourceName()).PreparationFailure.Retryable)
		backend := &fakeActorTemplates{template: failed, deleteErr: errors.New("cleanup unavailable")}
		store := &fakeRuntimeRevisionStore{}
		reconciler := &Reconciler{collections: Collections{Agents: agents, Reconciliations: states, AgentRuntimeObservations: observations}, templates: backend, store: store}
		require.ErrorContains(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()), "cleanup unavailable")
		synctest.Wait()
		require.True(t, states.GetKey(initial.ResourceName()).PreparationFailure.Retryable)
		corrected := secret.DeepCopy()
		corrected.Data["authorization"] = []byte("correct-token")
		secrets.UpdateObject(corrected)
		synctest.Wait()
		require.Equal(t, initial.Target.RevisionID, states.GetKey(initial.ResourceName()).Target.RevisionID)

		// The next attempt removes the terminal golden. A lost cleanup reply
		// still allows recreation on the following attempt.
		backend.deletedBeforeError = true
		require.ErrorContains(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()), "cleanup unavailable")
		synctest.Wait()
		require.Nil(t, backend.template)
		require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
		synctest.Wait()
		require.NotEqual(t, "failed-uid", backend.template.Metadata.Uid)
		require.Equal(t, backend.template.Metadata.Uid, store.revision.ActorTemplateUID)
		require.False(t, store.markedSuccessful)

		// Another transient checkout failure also recovers without an input edit.
		backend.template = proto.CloneOf(backend.template)
		backend.template.Status = failed.Status
		backend.deleteErr = nil
		require.ErrorContains(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()), "retrying")
		synctest.Wait()
		require.Nil(t, backend.template)
		require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
		synctest.Wait()
		backend.template = proto.CloneOf(backend.template)
		backend.template.Status = sandboxGoldenStatus()
		require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
		synctest.Wait()
		require.Nil(t, states.GetKey(initial.ResourceName()).PreparationFailure)
		require.True(t, store.markedSuccessful)

		// Once published, even unusable rotated credentials keep the snapshot.
		prepared := backend.template
		secrets.UpdateObject(secret)
		synctest.Wait()
		require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
		require.Same(t, prepared, backend.template)
		require.Equal(t, initial.Target.RevisionID, states.GetKey(initial.ResourceName()).Target.RevisionID)
	})
}

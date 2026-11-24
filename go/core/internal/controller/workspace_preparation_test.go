// Copyright 2026 The kagent Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestWorkspaceSecretUpdatesPreservePreparedAgent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opts := krt.NewOptionsBuilder(t.Context().Done(), "workspace-recovery", nil)
		behavior := &kagentv1alpha3.AgentTemplate{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"},
			Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: &kagentv1alpha3.ObjectReference{Name: "model"}, SystemPrompt: "help"},
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
		observations.UpdateObject(AgentRuntimeObservation{Namespace: agent.Namespace, AgentName: agent.Name, RevisionID: initial.Target.RevisionID, Golden: goldenProgress{phase: goldenReady}})
		synctest.Wait()

		// Given a prepared source, when its Secret rotates, the preparation and
		// bootstrap bundle remain unchanged and its ready observation survives.
		corrected := secret.DeepCopy()
		corrected.Data["authorization"] = []byte("correct-token")
		secrets.UpdateObject(corrected)
		synctest.Wait()
		updated := states.GetKey(initial.ResourceName())
		require.True(t, initial.Target.equals(updated.Target))
		require.True(t, updated.Golden.ready())

		// Missing dependencies block compilation without losing the observation.
		secrets.DeleteObject(secret.Namespace + "/" + secret.Name)
		synctest.Wait()
		require.NotNil(t, states.GetKey(initial.ResourceName()).CompilationFailure)
		secrets.UpdateObject(corrected)
		synctest.Wait()
		require.True(t, states.GetKey(initial.ResourceName()).Golden.ready())

		// A different source must not borrow the previous preparation's readiness.
		changed := agent.DeepCopy()
		changed.Spec.Workspace.Git.Ref = "release"
		agents.UpdateObject(changed)
		synctest.Wait()
		updated = states.GetKey(initial.ResourceName())
		require.NotEqual(t, initial.Target.RevisionID, updated.Target.RevisionID)
		require.False(t, updated.Golden.ready())
	})
}

func TestWorkspaceSecretUpdatesPreservePreparedSandbox(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opts := krt.NewOptionsBuilder(t.Context().Done(), "sandbox-workspace", nil)
		template := sandboxTestTemplate()
		template.Spec.Workspace = &kagentv1alpha3.WorkspaceSpec{Git: &kagentv1alpha3.GitWorkspaceSource{
			URL: "https://git.example/repo", Ref: "main",
			CredentialRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "git"}, Key: "authorization"},
		}}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "git"}, Data: map[string][]byte{"authorization": []byte("old")}}
		secrets := krt.NewStaticCollection(nil, []*corev1.Secret{secret}, opts.WithName("Secrets")...)
		templates := krt.NewStaticCollection(nil, []*kagentv1alpha3.SandboxTemplate{template}, opts.WithName("Templates")...)
		pools := krt.NewStaticCollection(nil, []*atev1.WorkerPool{{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}}, opts.WithName("Pools")...)
		states := newSandboxCollections(Collections{SandboxTemplates: templates, Secrets: secrets, WorkerPools: pools}, substrate.SandboxPolicy{GuestImage: "guest@sha256:" + strings.Repeat("a", 64), CPU: "1", Memory: "1Gi"}, opts)
		synctest.Wait()
		initial := states.states.GetKey(template.Namespace + "/" + template.Name)
		require.NotNil(t, initial)
		require.Nil(t, initial.Failure)
		require.NotNil(t, initial.DesiredActorTemplate.GoldenEgressPolicy)
		states.observations.UpdateObject(sandboxRuntimeObservation{Key: initial.ResourceName(), RevisionID: initial.RevisionID, Golden: goldenProgress{phase: goldenReady}})
		synctest.Wait()

		// Given a ready Sandbox source, when its credential rotates, assert that
		// the compiled guest, golden policy, and readiness are retained.
		rotated := secret.DeepCopy()
		rotated.Data["authorization"] = []byte("rotated")
		secrets.UpdateObject(rotated)
		synctest.Wait()
		updated := states.states.GetKey(initial.ResourceName())
		require.Equal(t, initial.RevisionID, updated.RevisionID)
		require.True(t, substrate.ActorTemplateSpecEqual(initial.DesiredActorTemplate, updated.DesiredActorTemplate))
		require.True(t, updated.Golden.ready())

		secrets.DeleteObject(secret.Namespace + "/" + secret.Name)
		synctest.Wait()
		require.NotNil(t, states.states.GetKey(initial.ResourceName()).Failure)
		secrets.UpdateObject(rotated)
		synctest.Wait()
		require.True(t, states.states.GetKey(initial.ResourceName()).Golden.ready())

		changed := template.DeepCopy()
		changed.Spec.Workspace.Git.Ref = "release"
		templates.UpdateObject(changed)
		synctest.Wait()
		updated = states.states.GetKey(initial.ResourceName())
		require.NotEqual(t, initial.RevisionID, updated.RevisionID)
		require.False(t, updated.Golden.ready())
	})
}

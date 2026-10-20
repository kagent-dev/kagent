package controller

import (
	"strings"
	"testing"

	atev1alpha1 "go.ate.dev/substrate/pkg/api/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
)

func TestGitSkillSecretChangesReconcileReferences(t *testing.T) {
	for _, shared := range []bool{false, true} {
		name := "root"
		if shared {
			name = "shared child"
		}
		t.Run(name, func(t *testing.T) {
			stop := make(chan struct{})
			t.Cleanup(func() { close(stop) })
			opts := krt.NewOptionsBuilder(stop, "git-skills", nil)
			template := &kagentv1alpha3.AgentTemplate{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant"},
				Spec: kagentv1alpha3.AgentTemplateSpec{SystemPrompt: "help", ModelConfig: &corev1.LocalObjectReference{Name: "model"},
					Skills: []kagentv1alpha3.AgentTemplateSkill{{Name: "review", Source: kagentv1alpha3.ArtifactSource{Git: &kagentv1alpha3.GitArtifact{
						URL: "https://github.com/acme/skills", Commit: strings.Repeat("a", 40),
						AuthorizationFrom: &kagentv1alpha3.SecretKeyReference{Name: "git-auth", Key: "authorization"},
					}}}},
				},
			}
			templates := []*kagentv1alpha3.AgentTemplate{template}
			if shared {
				child := template.DeepCopy()
				child.Name = "child"
				template.Spec.Skills = nil
				template.Spec.Tools = []kagentv1alpha3.ToolBinding{{SubAgent: &kagentv1alpha3.SubAgentToolBinding{
					Name: "reviewer", Description: "Review changes", TemplateRef: &corev1.LocalObjectReference{Name: child.Name},
				}}}
				templates = append(templates, child)
			}
			selectedHarness := harness("team-a", "kagent", nil)
			selectedHarness.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
			selectedHarness.Spec.Workload.Image = new("example.com/kagent@sha256:" + strings.Repeat("a", 64))
			selectedHarness.Spec.Substrate = kagentv1alpha3.RuntimeSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
			}
			mock := krttest.NewMock(t, []any{selectedHarness, &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}}})
			secrets := krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...)
			models := krt.NewStaticCollection(nil, []*kagentv1alpha3.ModelConfig{{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5"}}}, opts.WithName("Models")...)
			configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
			_, resolvedModels := newModelConfigReconciliations(models, configMaps, secrets, opts)
			agents := krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, selectedHarness)}, opts.WithName("Agents")...)
			states := newAgentReconciliations(agents, translator.Collections{
				AgentTemplates: krt.NewStaticCollection(nil, templates, opts.WithName("Templates")...),
				Harnesses:      krttest.GetMockCollection[*kagentv1alpha3.Harness](mock), ResolvedModelConfigs: resolvedModels,
				RemoteMCPServers: krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock), ConfigMaps: configMaps,
				Secrets: secrets, WorkerPools: krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
			}, krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("Observations")...), opts, testRuntimeDefaults(), nil)
			statuses := newAgentStatuses(agents, states, opts)
			awaitReferenceFailure := func(message string) {
				t.Helper()
				waitFor(t, func() bool {
					updates := statuses.List()
					if len(updates) != 1 {
						return false
					}
					condition := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionResolvedRefs)
					return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "ReferenceResolutionFailed" && strings.Contains(condition.Message, message)
				})
			}
			awaitReferenceFailure("not found")
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "git-auth"}}
			secrets.UpdateObject(secret.DeepCopy())
			awaitReferenceFailure("non-empty key")
			secret.Data = map[string][]byte{"authorization": []byte("Bearer fixture-only")}
			secrets.UpdateObject(secret.DeepCopy())
			awaitReferenceFailure("must authorize origin")
			secret.Annotations = map[string]string{kagentv1alpha3.GitCredentialOriginAnnotation: "https://github.com"}
			secrets.UpdateObject(secret.DeepCopy())
			waitFor(t, func() bool {
				values := states.List()
				return len(values) == 1 && values[0].CompilationFailure == nil && values[0].Target != nil
			})
			// Revoking the grant must invalidate compilation without editing the Agent.
			secret.Annotations = nil
			secrets.UpdateObject(secret.DeepCopy())
			awaitReferenceFailure("must authorize origin")
		})
	}
}

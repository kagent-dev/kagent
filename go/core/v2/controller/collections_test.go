package controller

import (
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAgentTemplateHarnessPairs(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	template := &kagentv1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{
		Namespace: "team-a", Name: "assistant", Labels: map[string]string{"runtime": "python"},
	}}
	matching := harness("team-a", "matching", map[string]string{"runtime": "python"})
	harnesses := krt.NewStaticCollection(nil, []*kagentv1alpha3.Harness{
		matching,
		harness("team-a", "denied", map[string]string{"runtime": "go"}),
		harness("team-b", "other-namespace", map[string]string{"runtime": "python"}),
		{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "no-admission"}},
	}, opts.WithName("Harnesses")...)
	templates := krt.NewStaticCollection(nil, []*kagentv1alpha3.AgentTemplate{template}, opts.WithName("AgentTemplates")...)
	pairs := newPairCollection(templates, harnesses, opts)

	if !pairs.WaitUntilSynced(stop) {
		t.Fatal("pair collection did not sync")
	}
	waitForPairs(t, pairs, "team-a/assistant/matching")

	harnesses.UpdateObject(harness("team-a", "matching", map[string]string{"runtime": "go"}))
	waitForPairs(t, pairs)
	harnesses.UpdateObject(matching)
	waitForPairs(t, pairs, "team-a/assistant/matching")
}

func TestReconciliationCollectionsCompileAndObserveRevision(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	template := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid", Labels: map[string]string{"runtime": "python"}},
		Spec: kagentv1alpha3.AgentTemplateSpec{
			ModelConfig:  kagentv1alpha3.AgentTemplateLocalReference{Name: "model"},
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

	collections := Collections{
		AgentTemplates:   krt.NewStaticCollection(nil, []*kagentv1alpha3.AgentTemplate{template}, opts.WithName("AgentTemplates")...),
		Harnesses:        krt.NewStaticCollection(nil, []*kagentv1alpha3.Harness{matchingHarness}, opts.WithName("Harnesses")...),
		ModelConfigs:     modelConfigs,
		RemoteMCPServers: krt.NewStaticCollection[*kagentv1alpha3.RemoteMCPServer](nil, nil, opts.WithName("RemoteMCPServers")...),
		ConfigMaps:       krt.NewStaticCollection[*corev1.ConfigMap](nil, nil, opts.WithName("ConfigMaps")...),
		Secrets:          krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...),
		WorkerPools:      krt.NewStaticCollection(nil, []*atev1alpha1.WorkerPool{{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}}}, opts.WithName("WorkerPools")...),
		ActorTemplates:   krt.NewStaticCollection[*atev1alpha1.ActorTemplate](nil, nil, opts.WithName("ActorTemplates")...),
	}
	collections.Pairs = newPairCollection(collections.AgentTemplates, collections.Harnesses, opts)
	collections.Reconciliations = newPairReconciliations(
		collections.Pairs, collections.AgentTemplates, collections.ModelConfigs, collections.RemoteMCPServers,
		collections.ConfigMaps, collections.Secrets, collections.WorkerPools, collections.ActorTemplates, opts,
	)
	collections.AgentTemplateStatuses = newAgentTemplateStatuses(collections.AgentTemplates, collections.Reconciliations, opts)

	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		return len(states) == 1 && states[0].Failure == nil && states[0].DesiredActorTemplate != nil
	})
	state := collections.Reconciliations.List()[0]
	if state.ObservedActorTemplate != nil {
		t.Fatal("ActorTemplate was observed before it existed")
	}
	waitFor(t, func() bool {
		updates := collections.AgentTemplateStatuses.List()
		if len(updates) != 1 || len(updates[0].Status.Harnesses) != 1 {
			return false
		}
		ready := apimeta.FindStatusCondition(updates[0].Status.Harnesses[0].Conditions, kagentv1alpha3.AgentTemplateConditionReady)
		return ready != nil && ready.Status == metav1.ConditionFalse
	})

	observed := state.DesiredActorTemplate.DeepCopy()
	observed.UID = "actor-template-uid"
	observed.Status.Phase = atev1alpha1.PhaseReady
	observed.Status.GoldenSnapshot = "golden"
	collections.ActorTemplates.(krt.StaticCollection[*atev1alpha1.ActorTemplate]).UpdateObject(observed)
	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		updates := collections.AgentTemplateStatuses.List()
		if len(states) != 1 || states[0].ObservedActorTemplate == nil || len(updates) != 1 || len(updates[0].Status.Harnesses) != 1 {
			return false
		}
		ready := apimeta.FindStatusCondition(updates[0].Status.Harnesses[0].Conditions, kagentv1alpha3.AgentTemplateConditionReady)
		return ready != nil && ready.Status == metav1.ConditionTrue && updates[0].Status.Harnesses[0].LatestSuccessfulRevision == state.RevisionID.String()
	})

	modelConfigs.UpdateObject(&kagentv1alpha3.ModelConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5.1"}})
	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		return len(states) == 1 && states[0].RevisionID != state.RevisionID && states[0].ObservedActorTemplate == nil
	})
}

func TestReconciliationTracksSharedAgentTemplate(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	child := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "child", Labels: map[string]string{"runtime": "python"}},
		Spec:       kagentv1alpha3.AgentTemplateSpec{ModelConfig: kagentv1alpha3.AgentTemplateLocalReference{Name: "model"}, SystemPrompt: "before"},
	}
	root := &kagentv1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "root", Labels: map[string]string{"runtime": "python"}},
		Spec: kagentv1alpha3.AgentTemplateSpec{
			ModelConfig: kagentv1alpha3.AgentTemplateLocalReference{Name: "model"},
			Tools: []kagentv1alpha3.ToolBinding{{Agent: &kagentv1alpha3.AgentToolBinding{
				Name: "child", Description: "delegate", TemplateRef: kagentv1alpha3.AgentTemplateLocalReference{Name: child.Name},
			}}},
		},
	}
	harness := harness("team-a", "kagent", map[string]string{"runtime": "python"})
	harness.Spec.Kagent = &kagentv1alpha3.KagentHarness{}
	harness.Spec.Workload.Image = "example.com/kagent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	harness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"}}
	templates := krt.NewStaticCollection(nil, []*kagentv1alpha3.AgentTemplate{root, child}, opts.WithName("AgentTemplates")...)
	pairs := newPairCollection(templates, krt.NewStaticCollection(nil, []*kagentv1alpha3.Harness{harness}, opts.WithName("Harnesses")...), opts)
	reconciliations := newPairReconciliations(
		pairs, templates,
		krt.NewStaticCollection(nil, []*kagentv1alpha3.ModelConfig{{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model"}, Spec: kagentv1alpha3.ModelConfigSpec{Provider: kagentv1alpha3.ModelProviderOpenAI, Model: "gpt-5"}}}, opts.WithName("ModelConfigs")...),
		krt.NewStaticCollection[*kagentv1alpha3.RemoteMCPServer](nil, nil, opts.WithName("RemoteMCPServers")...),
		krt.NewStaticCollection[*corev1.ConfigMap](nil, nil, opts.WithName("ConfigMaps")...),
		krt.NewStaticCollection[*corev1.Secret](nil, nil, opts.WithName("Secrets")...),
		krt.NewStaticCollection(nil, []*atev1alpha1.WorkerPool{{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "default"}}}, opts.WithName("WorkerPools")...),
		krt.NewStaticCollection[*atev1alpha1.ActorTemplate](nil, nil, opts.WithName("ActorTemplates")...), opts,
	)
	var initial string
	waitFor(t, func() bool {
		for _, state := range reconciliations.List() {
			if state.Pair.AgentTemplate.Name == root.Name && state.Failure == nil {
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
			if state.Pair.AgentTemplate.Name == root.Name {
				return state.Failure == nil && state.RevisionID.String() != initial
			}
		}
		return false
	})
}

func harness(namespace, name string, matchLabels map[string]string) *kagentv1alpha3.Harness {
	return &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: kagentv1alpha3.HarnessSpec{AllowedAgentTemplates: &kagentv1alpha3.HarnessAgentTemplateAdmission{
			Selector: metav1.LabelSelector{MatchLabels: matchLabels},
		}},
	}
}

func waitForPairs(t *testing.T, pairs krt.Collection[AgentTemplateHarnessPair], want ...string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got := pairs.List()
		if len(got) == len(want) {
			matched := true
			for i := range got {
				if got[i].ResourceName() != want[i] {
					matched = false
				}
			}
			if matched {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pairs = %v, want %v", pairs.List(), want)
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

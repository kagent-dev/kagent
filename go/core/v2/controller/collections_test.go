package controller

import (
	"testing"
	"time"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"istio.io/istio/pkg/kube/krt"
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

package controller

import (
	"testing"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStatusForPairPublishesCompilationWarnings(t *testing.T) {
	warnings := []string{"partial MCP selection is not enforced"}
	state := PairReconciliation{
		Pair:       AgentTemplateHarnessPair{Harness: &kagentv1alpha3.Harness{}},
		Revision:   &v2translator.Revision{},
		Warnings:   warnings,
		RevisionID: v2translator.RevisionID{1},
	}
	status := statusForPair(state, 1, "")
	if len(status.Warnings) != 1 || status.Warnings[0] != warnings[0] {
		t.Fatalf("warnings = %v, want %v", status.Warnings, warnings)
	}
	warnings[0] = "changed"
	if status.Warnings[0] == warnings[0] {
		t.Fatal("status aliases mutable compilation warnings")
	}
}

func TestHarnessStatusReadyWhenWorkerPoolExists(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test-harness-status", nil)

	harness := &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-a",
			Name:      "kagent",
		},
		Spec: kagentv1alpha3.HarnessSpec{
			Substrate: kagentv1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "default"},
			},
		},
	}
	workerPool := &atev1alpha1.WorkerPool{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-a",
			Name:      "default",
		},
	}

	mock := krttest.NewMock(t, []any{harness, workerPool})
	harnesses := krttest.GetMockCollection[*kagentv1alpha3.Harness](mock)
	workerPools := krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock)

	statuses := newHarnessStatuses(harnesses, workerPools, opts)

	waitFor(t, func() bool {
		status := statuses.GetKey("team-a/kagent")
		if status == nil {
			return false
		}
		condition := apimeta.FindStatusCondition(
			status.Status.Conditions,
			kagentv1alpha3.HarnessConditionTypeReady,
		)
		return condition != nil && condition.Status == metav1.ConditionTrue
	})
}

func TestHarnessStatusNotReadyWhenWorkerPoolMissing(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test-harness-status-missing", nil)

	harness := &kagentv1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-a",
			Name:      "kagent",
		},
		Spec: kagentv1alpha3.HarnessSpec{
			Substrate: kagentv1alpha3.HarnessSubstratePolicy{
				WorkerPoolRef: corev1.LocalObjectReference{Name: "missing"},
			},
		},
	}

	mock := krttest.NewMock(t, []any{harness})
	harnesses := krttest.GetMockCollection[*kagentv1alpha3.Harness](mock)
	workerPools := krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock)

	statuses := newHarnessStatuses(harnesses, workerPools, opts)

	waitFor(t, func() bool {
		status := statuses.GetKey("team-a/kagent")
		if status == nil {
			return false
		}
		condition := apimeta.FindStatusCondition(
			status.Status.Conditions,
			kagentv1alpha3.HarnessConditionTypeReady,
		)
		return condition != nil &&
			condition.Status == metav1.ConditionFalse &&
			condition.Reason == "WorkerPoolNotFound"
	})
}

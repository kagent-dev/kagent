package app

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCheckpointActivationDrainsLegacyWriters(t *testing.T) {
	for _, scenario := range []struct {
		name                                  string
		phase                                 corev1.PodPhase
		terminating, compatible, otherRelease bool
		ready                                 bool
	}{
		{name: "running legacy writer", phase: corev1.PodRunning},
		{name: "unready legacy writer", phase: corev1.PodPending},
		{name: "terminating legacy writer", phase: corev1.PodRunning, terminating: true},
		{name: "exited legacy writer", phase: corev1.PodSucceeded, ready: true},
		{name: "failed legacy writer", phase: corev1.PodFailed, ready: true},
		{name: "compatible writer", phase: corev1.PodRunning, compatible: true, ready: true},
		{name: "another release", phase: corev1.PodRunning, otherRelease: true, ready: true},
	} {
		t.Run("Given a "+scenario.name+", when recovery checks activation, assert that only drained or compatible writers permit execution", func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			self := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "new", UID: "new-uid", Labels: map[string]string{
				"app.kubernetes.io/name": "custom-name", "app.kubernetes.io/instance": "release-a", controllerComponentLabel: "controller",
				"pod-template-hash": "new-generation",
			}}}
			old := self.DeepCopy()
			old.Name = "previous"
			old.UID = "old-uid"
			old.Labels["pod-template-hash"] = "old-generation"
			old.Status.Phase = scenario.phase
			if scenario.compatible {
				old.Annotations = map[string]string{checkpointClaimsAnnotation: checkpointClaimsVersion}
			}
			if scenario.otherRelease {
				old.Labels["app.kubernetes.io/instance"] = "release-b"
			}
			if scenario.terminating {
				now := metav1.Now()
				old.DeletionTimestamp = &now
				old.Finalizers = []string{"test/retain"}
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(self, old).Build()
			gate := &checkpointActivation{reader: reader, namespace: self.Namespace, podName: self.Name, podUID: self.UID}
			ready, err := gate.drained(t.Context())
			require.NoError(t, err)
			require.Equal(t, scenario.ready, ready)
			require.False(t, gate.NeedLeaderElection(), "followers must activate their API admission too")
		})
	}
}

func TestCheckpointActivationFailsClosedOnMissingIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	gate := &checkpointActivation{reader: reader, namespace: "kagent", podName: "missing", podUID: "missing-uid"}
	ready, err := gate.drained(t.Context())
	require.Error(t, err)
	require.False(t, ready)
	// Local controller development has no Pod identity and is safe only after
	// any old controller Pods in the namespace have been stopped.
	gate.podName = ""
	gate.podUID = ""
	ready, err = gate.drained(t.Context())
	require.NoError(t, err)
	require.True(t, ready)
}

func TestCheckpointActivationWaitsForLegacyReplicaSetScaleDown(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		desired   int32
		observed  int64
		remaining int32
		ready     bool
	}{
		{name: "old Pod can be recreated", desired: 1, observed: 2},
		{name: "scale-down not yet observed", observed: 1},
		{name: "old replica still reported", observed: 2, remaining: 1},
		{name: "scale-down completed", observed: 2, ready: true},
	} {
		t.Run("Given no live legacy Pods, when "+scenario.name+", assert that activation requires confirmed retirement", func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			previous := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "previous", Generation: 2, Labels: map[string]string{controllerComponentLabel: "controller"}},
				Spec: appsv1.ReplicaSetSpec{Replicas: new(scenario.desired)}, Status: appsv1.ReplicaSetStatus{ObservedGeneration: scenario.observed, Replicas: scenario.remaining},
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(previous).Build()
			gate := &checkpointActivation{reader: reader, namespace: "kagent"}
			ready, err := gate.drained(t.Context())
			require.NoError(t, err)
			require.Equal(t, scenario.ready, ready)
		})
	}
}

func TestCheckpointActivationOpensAfterLegacyExit(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	legacy := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "legacy", UID: "legacy-uid", Labels: map[string]string{controllerComponentLabel: "controller"}}}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacy).Build()
	gate := &checkpointActivation{reader: reader, namespace: "kagent", ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- gate.Start(ctx) }()
	t.Cleanup(func() { cancel(); require.NoError(t, <-done) })
	// Given a non-leader API's closed startup gate, when the legacy Pod exits,
	// assert that the running gate opens and continues until shutdown.
	require.Never(t, func() bool {
		select {
		case <-gate.ready:
			return true
		default:
			return false
		}
	}, 20*time.Millisecond, time.Millisecond)
	require.NoError(t, reader.Delete(t.Context(), legacy))
	select {
	case <-gate.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("activation remained closed after legacy retirement")
	}
	select {
	case err := <-done:
		t.Fatalf("gate stopped before controller shutdown: %v", err)
	default:
	}
}

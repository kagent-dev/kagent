package upgrade

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func TestControllerHandoff(t *testing.T) {
	old := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", UID: "old-uid"}}
	replacement := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", UID: "new-uid"}}
	t.Run("Given a terminating previous writer, when a replacement is created, assert that the overlap is rejected", func(t *testing.T) {
		handoff := newControllerHandoff([]corev1.Pod{old})
		terminating := old.DeepCopy()
		now := metav1.Now()
		terminating.DeletionTimestamp = &now
		require.NoError(t, handoff.observe(watch.Event{Type: watch.Modified, Object: terminating}))
		require.ErrorContains(t, handoff.observe(watch.Event{Type: watch.Added, Object: replacement}), "previous writers still exist")
		require.False(t, handoff.sawReplacement)
	})
	t.Run("Given the previous writer has exited, when a replacement reuses its name, assert that the new UID completes the handoff", func(t *testing.T) {
		handoff := newControllerHandoff([]corev1.Pod{old})
		require.NoError(t, handoff.observe(watch.Event{Type: watch.Deleted, Object: &old}))
		require.NoError(t, handoff.observe(watch.Event{Type: watch.Added, Object: replacement}))
		require.Empty(t, handoff.old)
		require.True(t, handoff.sawReplacement)
	})
	t.Run("Given an active handoff watch, when Kubernetes reports a watch error, assert that verification fails closed", func(t *testing.T) {
		handoff := newControllerHandoff([]corev1.Pod{old})
		require.ErrorContains(t, handoff.observe(watch.Event{Type: watch.Error, Object: &metav1.Status{Reason: metav1.StatusReasonExpired}}), "watch failed")
		require.False(t, handoff.sawReplacement)
	})
	for _, phase := range []corev1.PodPhase{corev1.PodSucceeded, corev1.PodFailed} {
		t.Run("Given a previous writer, when Kubernetes reports "+string(phase)+", assert that a replacement can start before object deletion", func(t *testing.T) {
			handoff := newControllerHandoff([]corev1.Pod{old})
			exited := old.DeepCopy()
			exited.Status.Phase = phase
			require.NoError(t, handoff.observe(watch.Event{Type: watch.Modified, Object: exited}))
			require.NoError(t, handoff.observe(watch.Event{Type: watch.Added, Object: replacement}))
			require.True(t, handoff.sawReplacement)
		})
	}
}

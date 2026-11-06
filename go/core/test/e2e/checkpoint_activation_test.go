package e2e_test

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
)

// This is a controller activation test, independent of harness execution. The
// unmarked, deliberately unready Pod represents a legacy process still alive;
// it does not fabricate a checkpoint, backend phase, or old-binary upgrade.
func TestCheckpointActivationDuringControllerReplacement(t *testing.T) {
	target := interactionTarget(t)
	image := kagentenv.E2ERuntimeImage.Get()
	require.NotEmpty(t, image, "requires the digest-pinned E2E shell fixture")
	kube := interactionKubeClient(t)
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e")
	var pods corev1.PodList
	require.NoError(t, kube.List(ctx, &pods, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, pods.Items, 1)
	old := pods.Items[0]
	labels := maps.Clone(old.Labels)
	delete(labels, "pod-template-hash")
	legacy := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: old.Namespace, GenerateName: "checkpoint-legacy-writer-", Labels: labels}, Spec: corev1.PodSpec{
		TerminationGracePeriodSeconds: new(int64(30)),
		Containers: []corev1.Container{{Name: "writer", Image: image, Command: []string{"/bin/sh", "-c", "exec sleep 3600"},
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "exit 1"}}}},
			Lifecycle:      &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Sleep: &corev1.SleepAction{Seconds: 5}}},
		}},
	}}
	require.NoError(t, kube.Create(ctx, legacy))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := kube.Delete(cleanup, legacy); !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	})
	require.NoError(t, wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(legacy), legacy); err != nil {
			return false, err
		}
		return legacy.Status.Phase == corev1.PodRunning, nil
	}))
	// Given an unready legacy replica, when the actual controller is replaced,
	// assert that its public health and readiness recover without activating
	// checkpoint mutations. The fake replica cannot receive Service traffic.
	restartControllerPod(t, kube, old)
	conn := newControllerConn(t, target)
	checkpoints := apiv1.NewCheckpointServiceClient(conn)
	create := &apiv1.CreateCheckpointRequest{SessionId: uuid.NewString(), RequestId: uuid.NewString(), ExpectedHeadTaskId: "activation-probe"}
	remove := &apiv1.DeleteCheckpointRequest{CheckpointId: uuid.NewString()}
	_, err := checkpoints.CreateCheckpoint(ctx, create)
	require.Equal(t, codes.Unavailable, status.Code(err))
	_, err = checkpoints.DeleteCheckpoint(ctx, remove)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.NoError(t, kube.Delete(ctx, legacy))
	// A deletion timestamp alone must not activate recovery during preStop.
	_, err = checkpoints.CreateCheckpoint(ctx, create)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.NoError(t, wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, time.Minute, true, func(ctx context.Context) (bool, error) {
		_, err := checkpoints.CreateCheckpoint(ctx, create)
		if status.Code(err) == codes.Unavailable {
			return false, nil
		}
		// NotFound proves admission reached the real store after activation;
		// the probe intentionally never admits synthetic checkpoint work.
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, err
	}))
	_, err = checkpoints.DeleteCheckpoint(ctx, remove)
	require.Equal(t, codes.NotFound, status.Code(err))
}

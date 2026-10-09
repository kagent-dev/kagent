package session

import (
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestDirectoryReleasedEvent(t *testing.T) {
	session := &apiv1alpha1.Session{
		Id:    "0199a6f0-session",
		Agent: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "coder"},
		VolumeSource: &apiv1alpha1.SessionVolumeSource{
			Volume: &apiv1alpha1.SessionVolume{CsiDriver: "nfs.csi.k8s.io", VolumeHandle: "nfs-server#share#workspace##"},
			Mounts: []*apiv1alpha1.SessionVolumeMount{
				{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
				{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			},
		},
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	event, err := directoryReleasedEvent(session, now)
	require.NoError(t, err)
	require.Equal(t, "kagent", event.Namespace)
	require.Equal(t, ReasonSessionDirectoryReleased, event.Reason)
	require.Equal(t, corev1.EventTypeNormal, event.Type)
	require.Equal(t, corev1.ObjectReference{APIVersion: "api.kagent.dev/v1alpha3", Kind: "Agent", Namespace: "kagent", Name: "coder"}, event.InvolvedObject)
	require.Equal(t, map[string]string{
		AnnotationSessionID:    "0199a6f0-session",
		AnnotationCSIDriver:    "nfs.csi.k8s.io",
		AnnotationVolumeHandle: "nfs-server#share#workspace##",
		AnnotationSubPath:      "sessions/0199a6f0-session",
	}, event.Annotations)
	require.True(t, event.FirstTimestamp.Time.Equal(now))

	session.VolumeSource.Mounts = session.VolumeSource.Mounts[:1]
	_, err = directoryReleasedEvent(session, now)
	require.ErrorContains(t, err, "no directory of its own")
}

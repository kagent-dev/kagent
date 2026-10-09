package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// The Event a deleted session with a volume source leaves for the owner of
// the volume, which watches Events of this reason (a field selector on
// reason) and deletes the directory the annotations name.
const (
	// ReasonSessionDirectoryReleased is the Event's reason.
	ReasonSessionDirectoryReleased = "SessionDirectoryReleased"
	// AnnotationSessionID names the deleted session.
	AnnotationSessionID = "kagent.dev/session-id"
	// AnnotationCSIDriver and AnnotationVolumeHandle name the volume.
	AnnotationCSIDriver    = "kagent.dev/csi-driver"
	AnnotationVolumeHandle = "kagent.dev/volume-handle"
	// AnnotationSubPath is the session's own directory on the volume, the one
	// it mounted read-write.
	AnnotationSubPath = "kagent.dev/sub-path"

	directoryEventComponent = "kagent-controller"
)

// EventDirectoryReleaser tells the owner of a session's volume through a
// Kubernetes Event on the session's Agent, in the Agent's namespace.
type EventDirectoryReleaser struct {
	client ctrlclient.Writer
	now    func() time.Time
}

func NewEventDirectoryReleaser(client ctrlclient.Writer) *EventDirectoryReleaser {
	return &EventDirectoryReleaser{client: client, now: time.Now}
}

func (r *EventDirectoryReleaser) ReleaseSessionDirectory(ctx context.Context, session *apiv1alpha1.Session) error {
	event, err := directoryReleasedEvent(session, r.now())
	if err != nil {
		return err
	}
	if err := r.client.Create(ctx, event); err != nil {
		return fmt.Errorf("record %s for session %s: %w", ReasonSessionDirectoryReleased, session.GetId(), err)
	}
	return nil
}

// directoryReleasedEvent is the Event that releases a deleted session's own
// directory of its volume source.
func directoryReleasedEvent(session *apiv1alpha1.Session, now time.Time) (*corev1.Event, error) {
	source := session.GetVolumeSource()
	var subPath string
	for _, mount := range source.GetMounts() {
		if mount.GetMountPath() == substrate.WorkspaceMountPath && !mount.GetReadOnly() {
			subPath = strings.ReplaceAll(mount.GetSubPath(), substrate.SessionIDPlaceholder, session.GetId())
		}
	}
	if subPath == "" {
		return nil, fmt.Errorf("session %s has no directory of its own on a volume", session.GetId())
	}
	agent := session.GetAgent()
	volume := source.GetVolume()
	timestamp := metav1.NewTime(now)
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "session-directory-released-",
			Namespace:    agent.GetNamespace(),
			Annotations: map[string]string{
				AnnotationSessionID:    session.GetId(),
				AnnotationCSIDriver:    volume.GetCsiDriver(),
				AnnotationVolumeHandle: volume.GetVolumeHandle(),
				AnnotationSubPath:      subPath,
			},
		},
		InvolvedObject: corev1.ObjectReference{
			APIVersion: kagentv1alpha3.GroupVersion.String(), Kind: "Agent",
			Namespace: agent.GetNamespace(), Name: agent.GetName(),
		},
		Reason:         ReasonSessionDirectoryReleased,
		Message:        fmt.Sprintf("Session %s was deleted: its directory %s of volume %s (%s) may be deleted", session.GetId(), subPath, volume.GetVolumeHandle(), volume.GetCsiDriver()),
		Type:           corev1.EventTypeNormal,
		Source:         corev1.EventSource{Component: directoryEventComponent},
		FirstTimestamp: timestamp, LastTimestamp: timestamp, Count: 1,
		ReportingController: directoryEventComponent,
	}, nil
}

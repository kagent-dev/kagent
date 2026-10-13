package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestCreateSessionVolumeSourceNeedsAnAdmission proves on the installed
// controller, which configures no Session volume admission, that every create
// naming a volume source or carrying an admission token is refused before
// anything is reserved, while the same request without them goes on to the
// agent lookup as it always did.
func TestCreateSessionVolumeSourceNeedsAnAdmission(t *testing.T) {
	target := interactionTarget(t)
	conn := newControllerConn(t, target)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), time.Minute)
	t.Cleanup(cancel)
	sessions := apiv1alpha1.NewSessionServiceClient(conn)
	// The agent does not exist: the refusal has to come from the admission gate,
	// which runs before the store looks the agent up.
	agent := &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "no-such-agent-" + uuid.NewString()[:8]}
	source := &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "efs.csi.aws.com", VolumeHandle: "fs-0123456789abcdef0::fsap-0123456789abcdef0"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}
	for _, tc := range []struct {
		name    string
		request *apiv1alpha1.CreateSessionRequest
	}{
		{name: "volume source", request: &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), VolumeSource: source}},
		{name: "admission token", request: &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), AdmissionToken: "grant"}},
		{name: "both", request: &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), VolumeSource: source, AdmissionToken: "grant"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sessions.CreateSession(ctx, tc.request)
			require.Equal(t, codes.FailedPrecondition, status.Code(err), "error = %v", err)
			require.ErrorContains(t, err, "this installation admits no Session volume")
		})
	}
	t.Run("without the fields", func(t *testing.T) {
		_, err := sessions.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString()})
		require.Equal(t, codes.FailedPrecondition, status.Code(err), "error = %v", err)
		require.ErrorContains(t, err, "Agent does not have a ready prepared revision")
	})
	t.Run("malformed source", func(t *testing.T) {
		malformed := &apiv1alpha1.SessionVolumeSource{
			Volume: &apiv1alpha1.SessionVolume{CsiDriver: "efs.csi.aws.com", VolumeHandle: "fs-0123456789abcdef0::fsap-0123456789abcdef0"},
			Mounts: []*apiv1alpha1.SessionVolumeMount{{SubPath: "sessions/${SESSION_ID}", MountPath: "/data/workspace"}},
		}
		_, err := sessions.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), VolumeSource: malformed})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "error = %v", err)
	})
}

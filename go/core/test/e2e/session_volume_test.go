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

// TestCreateSessionVolumeSourceNeedsAGrant proves on the installed controller,
// whose lane configures the jwt Session admission, that a create naming a
// volume source is refused before anything is reserved unless it carries a
// grant of the admission's issuer, while the same request without the fields
// goes on to the agent lookup as it always did.
func TestCreateSessionVolumeSourceNeedsAGrant(t *testing.T) {
	target := interactionTarget(t)
	conn := newControllerConn(t, target)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", workspaceGrantSubject), time.Minute)
	t.Cleanup(cancel)
	sessions := apiv1alpha1.NewSessionServiceClient(conn)
	// The agent does not exist: the refusal has to come from the admission,
	// which runs before the store looks the agent up.
	agent := &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "no-such-agent-" + uuid.NewString()[:8]}
	source := &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "nfs.csi.k8s.io", VolumeHandle: "nfs-server.default.svc.cluster.local##workspace##"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}
	for _, tc := range []struct {
		name    string
		request *apiv1alpha1.CreateSessionRequest
		code    codes.Code
		message string
	}{
		{name: "volume source without a grant", request: &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), VolumeSource: source}, code: codes.PermissionDenied, message: "no admission grant"},
		{name: "volume source with a forged grant", request: &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), VolumeSource: source, AdmissionToken: "grant"}, code: codes.PermissionDenied},
		{name: "a grant without a volume source", request: &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), AdmissionToken: "grant"}, code: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sessions.CreateSession(ctx, tc.request)
			require.Equal(t, tc.code, status.Code(err), "error = %v", err)
			if tc.message != "" {
				require.ErrorContains(t, err, tc.message)
			}
		})
	}
	t.Run("without the fields", func(t *testing.T) {
		_, err := sessions.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString()})
		require.Equal(t, codes.FailedPrecondition, status.Code(err), "error = %v", err)
		require.ErrorContains(t, err, "Agent does not have a ready prepared revision")
	})
	t.Run("malformed source", func(t *testing.T) {
		malformed := &apiv1alpha1.SessionVolumeSource{
			Volume: source.GetVolume(),
			Mounts: []*apiv1alpha1.SessionVolumeMount{{SubPath: "sessions/${SESSION_ID}", MountPath: "/data/workspace"}},
		}
		_, err := sessions.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: uuid.NewString(), VolumeSource: malformed, AdmissionToken: "grant"})
		require.Equal(t, codes.InvalidArgument, status.Code(err), "error = %v", err)
	})
}

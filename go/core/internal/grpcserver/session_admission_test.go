package grpcserver

import (
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/service/session/jwtadmission"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/test/grant"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestSessionAdmittedByAGrantOnTheWire drives the session server with the jwt
// admission against the PostgreSQL store: a Session created with a grant shows
// the grant's providers and changed list on create, get and list, and the
// grant is in no response and not on the row; a refused grant reserves nothing.
func TestSessionAdmittedByAGrantOnTheWire(t *testing.T) {
	const issuer, audience = "https://workspaces.example.test", "kagent"
	store, db := volumeTestStore(t)
	signer, err := grant.NewSigner(issuer, audience)
	require.NoError(t, err)
	admission, err := jwtadmission.New(jwtadmission.Config{JWKSURL: signer.Serve(t), Issuer: issuer, Audience: audience}, nil)
	require.NoError(t, err)
	server := &sessionServer{service: sessionsvc.NewService(store, &auth.NoopAuthorizer{}, staticSessionWorkflow{}, sessionsvc.WithVolumeAdmission(admission))}

	ctx := auth.AuthSessionTo(t.Context(), volumeTestSession{userID: "alice"})
	agent := &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}
	source := &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "nfs.csi.k8s.io", VolumeHandle: "nfs.example.test#exports#workspace-1##"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}
	token, err := signer.SignGrant("alice", grant.Grant{
		Volume:    grant.Volume{Driver: "nfs.csi.k8s.io", Handle: "nfs.example.test#exports#workspace-1##"},
		Mounts:    []grant.Mount{{SubPath: "sessions/${SESSION_ID}"}, {SubPath: "mirrors", ReadOnly: true}},
		Changed:   []string{"acme/api"},
		Providers: []grant.Provider{{Hostname: "github.com", Audience: "https://github.com", Scheme: "Bearer", Broker: "workspaces"}},
	}, time.Minute)
	require.NoError(t, err)
	wantProvider := &apiv1alpha1.SessionProvider{Hostname: "github.com", Audience: "https://github.com", Scheme: "Bearer", Broker: "workspaces"}

	created, err := server.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: "with-grant", VolumeSource: source, AdmissionToken: token})
	require.NoError(t, err)
	id := created.GetSession().GetId()
	got, err := server.GetSession(ctx, &apiv1alpha1.GetSessionRequest{SessionId: id})
	require.NoError(t, err)
	listed, err := server.ListSessions(ctx, &apiv1alpha1.ListSessionsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.GetSessions(), 1)
	for name, session := range map[string]*apiv1alpha1.Session{"create": created.GetSession(), "get": got.GetSession(), "list": listed.GetSessions()[0]} {
		require.Len(t, session.GetProviders(), 1, "%s shows the providers", name)
		require.True(t, proto.Equal(wantProvider, session.GetProviders()[0]), "%s provider = %v", name, session.GetProviders()[0])
		require.Equal(t, []string{"acme/api"}, session.GetChanged(), "%s shows the changed list", name)
	}
	for name, response := range map[string]proto.Message{"create": created, "get": got, "list": listed} {
		wire, err := proto.Marshal(response)
		require.NoError(t, err)
		require.NotContains(t, string(wire), token, "the %s response must not carry the grant", name)
	}
	var row []byte
	require.NoError(t, db.QueryRow(t.Context(), `SELECT data FROM session WHERE id = $1`, id).Scan(&row))
	require.NotContains(t, string(row), token, "the row must not carry the grant")

	// Bob cannot start a Session with Alice's grant.
	bob := auth.AuthSessionTo(t.Context(), volumeTestSession{userID: "bob"})
	_, err = server.CreateSession(bob, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: "stolen-grant", VolumeSource: source, AdmissionToken: token})
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied), "error = %v", err)
	require.ErrorContains(t, err, "admission grant names another person")
	listed, err = server.ListSessions(bob, &apiv1alpha1.ListSessionsRequest{})
	require.NoError(t, err)
	require.Empty(t, listed.GetSessions(), "a refused grant reserves no Session")
}

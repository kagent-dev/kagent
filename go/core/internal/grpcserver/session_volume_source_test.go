package grpcserver

import (
	"context"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// admitEverySource stands in for an installation's configured admission.
type admitEverySource struct{}

func (admitEverySource) Admit(context.Context, string, *apiv1alpha1.SessionVolumeSource, string) error {
	return nil
}

type volumeTestSession struct{ userID string }

func (s volumeTestSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: s.userID}}
}

// TestSessionVolumeSourceOnTheWire drives the session server against the
// PostgreSQL store: the source a create names comes back on create, get and
// list; the admission token is in no response and not on the row; without an
// admission the same create is refused before the agent is looked up.
func TestSessionVolumeSourceOnTheWire(t *testing.T) {
	const token = "admission-token-never-stored"
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(t.Context(), &database.PostgresConfig{URL: dsn})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db)
	revision := database.RuntimeRevision{
		Revision: "revision-1", Namespace: "team-a", AgentName: "assistant", AgentUID: "template-uid",
		SourceSnapshot: []byte("{}"),
		AgentCard:      &a2apb.AgentCard{Name: "assistant"}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "assistant-kagent-revision", ActorTemplateUID: "actor-template-uid",
	}
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{
		Namespace: revision.Namespace, AgentName: revision.AgentName, AgentUID: revision.AgentUID,
		DesiredRevision: revision.Revision,
	}))
	require.NoError(t, store.RecordRuntimeRevision(t.Context(), revision, true))
	ctx := auth.AuthSessionTo(t.Context(), volumeTestSession{userID: "alice"})
	agent := &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}
	source := &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "efs.csi.aws.com", VolumeHandle: "fs-0123456789abcdef0::fsap-0123456789abcdef0"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}

	admitting := &sessionServer{service: sessionsvc.NewService(store, &auth.NoopAuthorizer{}, staticSessionWorkflow{}, sessionsvc.WithVolumeAdmission(admitEverySource{}))}
	created, err := admitting.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: "with-volume", VolumeSource: source, AdmissionToken: token})
	require.NoError(t, err)
	id := created.GetSession().GetId()
	require.True(t, proto.Equal(source, created.GetSession().GetVolumeSource()), "create = %v", created.GetSession().GetVolumeSource())
	got, err := admitting.GetSession(ctx, &apiv1alpha1.GetSessionRequest{SessionId: id})
	require.NoError(t, err)
	require.True(t, proto.Equal(source, got.GetSession().GetVolumeSource()), "get = %v", got.GetSession().GetVolumeSource())
	listed, err := admitting.ListSessions(ctx, &apiv1alpha1.ListSessionsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.GetSessions(), 1)
	require.True(t, proto.Equal(source, listed.GetSessions()[0].GetVolumeSource()), "list = %v", listed.GetSessions()[0].GetVolumeSource())
	for name, response := range map[string]proto.Message{"create": created, "get": got, "list": listed} {
		wire, err := proto.Marshal(response)
		require.NoError(t, err)
		require.NotContains(t, string(wire), token, "the %s response must not carry the admission token", name)
	}
	var row []byte
	require.NoError(t, db.QueryRow(t.Context(), `SELECT data FROM session WHERE id = $1`, id).Scan(&row))
	require.NotContains(t, string(row), token, "the row must not carry the admission token")

	plain, err := admitting.CreateSession(ctx, &apiv1alpha1.CreateSessionRequest{Agent: agent, RequestId: "plain"})
	require.NoError(t, err)
	require.Nil(t, plain.GetSession().GetVolumeSource())

	bare := &sessionServer{service: sessionsvc.NewService(store, &auth.NoopAuthorizer{}, staticSessionWorkflow{})}
	for name, request := range map[string]*apiv1alpha1.CreateSessionRequest{
		"volume source":   {Agent: agent, RequestId: "refused-source", VolumeSource: source},
		"admission token": {Agent: agent, RequestId: "refused-token", AdmissionToken: token},
	} {
		_, err := bare.CreateSession(ctx, request)
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition), "%s: error = %v", name, err)
		require.ErrorContains(t, err, "this installation admits no Session volume")
		_, err = bare.GetSession(ctx, &apiv1alpha1.GetSessionRequest{SessionId: id})
		require.NoError(t, err, "%s: a refusal leaves the store readable", name)
	}
	listed, err = bare.ListSessions(ctx, &apiv1alpha1.ListSessionsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.GetSessions(), 2, "a refused request reserves no Session")
}

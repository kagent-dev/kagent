package database

import (
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func volumeSourceFixture() *apiv1alpha1.SessionVolumeSource {
	return &apiv1alpha1.SessionVolumeSource{
		Volume: &apiv1alpha1.SessionVolume{CsiDriver: "efs.csi.aws.com", VolumeHandle: "fs-0123456789abcdef0::fsap-0123456789abcdef0"},
		Mounts: []*apiv1alpha1.SessionVolumeMount{
			{SubPath: "sessions/${SESSION_ID}", MountPath: "/workspace"},
			{SubPath: "mirrors", MountPath: "/mirrors", ReadOnly: true},
		},
	}
}

func TestSessionVolumeSourceRoundTrips(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	source := volumeSourceFixture()
	request := newSessionRequest(uuid.NewString(), "assistant", "kagent", "")
	request.VolumeSource = source

	created, wasCreated, err := client.CreateSession(ctx, request, "with-volume")
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.True(t, proto.Equal(source, created.GetVolumeSource()), "created = %v", created.GetVolumeSource())
	read, err := client.GetSession(ctx, created.Id, "alice")
	require.NoError(t, err)
	require.True(t, proto.Equal(source, read.GetVolumeSource()), "get = %v", read.GetVolumeSource())
	unscoped, err := client.GetSessionByID(ctx, created.Id)
	require.NoError(t, err)
	require.True(t, proto.Equal(source, unscoped.GetVolumeSource()), "get by id = %v", unscoped.GetVolumeSource())
	listed, err := client.ListSessions(ctx, SessionQuery{UserID: "alice", Limit: 10})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, proto.Equal(source, listed[0].GetVolumeSource()), "list = %v", listed[0].GetVolumeSource())

	// The source is part of the request's identity: a retry with the same one
	// returns the session, a retry with another one or with none is a conflict.
	retry := proto.CloneOf(request)
	retry.Id = uuid.NewString()
	retried, wasCreated, err := client.CreateSession(ctx, retry, "with-volume")
	require.NoError(t, err)
	require.False(t, wasCreated)
	require.Equal(t, created.Id, retried.Id)
	elsewhere := proto.CloneOf(request) // another session directory
	elsewhere.Id, elsewhere.VolumeSource.Mounts[0].SubPath = uuid.NewString(), "sessions/other"
	_, _, err = client.CreateSession(ctx, elsewhere, "with-volume")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	_, _, err = client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "with-volume")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
}

// A session created without a volume source stores the row every session
// stored before the field existed: the field is absent from the wire, not
// present and empty, so the bytes are the same as before.
func TestSessionWithoutAVolumeSourceStoresTheRowItAlwaysDid(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	id := uuid.NewString()

	created, wasCreated, err := client.CreateSession(ctx, newSessionRequest(id, "assistant", "kagent", "Plain"), "plain")
	require.NoError(t, err)
	require.True(t, wasCreated)
	require.Nil(t, created.GetVolumeSource())
	read, err := client.GetSession(ctx, id, "alice")
	require.NoError(t, err)
	require.Nil(t, read.GetVolumeSource())

	var data []byte
	require.NoError(t, client.db.QueryRow(ctx, `SELECT data FROM session WHERE id = $1`, id).Scan(&data))
	volumeSource := (&apiv1alpha1.Session{}).ProtoReflect().Descriptor().Fields().ByName("volume_source").Number()
	require.NotContains(t, wireFieldNumbers(t, data), volumeSource, "the row must not carry the field")
	// A retry of the plain request keeps its identity, as it always did.
	retry := newSessionRequest(uuid.NewString(), "assistant", "kagent", "Plain")
	retried, wasCreated, err := client.CreateSession(ctx, retry, "plain")
	require.NoError(t, err)
	require.False(t, wasCreated)
	require.Equal(t, id, retried.Id)
}

// wireFieldNumbers lists the field numbers a protobuf wire encoding carries,
// in order, including repeats.
func wireFieldNumbers(t *testing.T, data []byte) []protowire.Number {
	t.Helper()
	var numbers []protowire.Number
	for len(data) > 0 {
		number, _, length := protowire.ConsumeField(data)
		require.Positive(t, length, "malformed wire data")
		numbers = append(numbers, number)
		data = data[length:]
	}
	return numbers
}

package checkpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCreateHidesReservationReleaseFailuresAfterCancellation(t *testing.T) {
	store := &failedReservationRelease{testStore: &testStore{reserveErr: errors.New("private reservation connection detail")}}
	owner, cancel := context.WithCancel(auth.AuthSessionTo(t.Context(), testSession{userID: "alice"}))
	cancel()
	_, err := NewService(store, testAuthorizer{}, &testTags{}, nil).Create(owner, uuid.NewString(), uuid.NewString(), "task-1")
	require.Equal(t, 1, store.calls)
	require.NoError(t, store.contextErr, "reservation retirement must outlive caller cancellation")
	require.True(t, store.bounded, "retirement must keep the five-second bound")
	require.Equal(t, serviceerrors.CodeInternal, serviceerrors.CodeOf(err))
	require.NotContains(t, serviceerrors.MessageOf(err), "private")
}

func TestCreateClassifiesPublicationFailures(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		err         error
		serviceCode serviceerrors.Code
		grpcCode    codes.Code
	}{
		{"storage failure", errors.New("private PostgreSQL connection detail"), serviceerrors.CodeInternal, codes.Unknown},
		{"lost reply", status.Error(codes.Unavailable, "private finalization reply"), "", codes.Unavailable},
	} {
		t.Run("Given "+scenario.name+", when checkpoint publication fails, assert that only a classified public error is returned", func(t *testing.T) {
			store := &testStore{finalizeErr: scenario.err}
			tags := &testTags{snapshotURI: "s3://snapshots/snapshot-1"}
			owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
			_, err := NewService(store, testAuthorizer{}, tags, nil).Create(owner, uuid.NewString(), uuid.NewString(), "task-1")
			require.Error(t, err)
			require.Equal(t, 1, tags.createCalls)
			require.Equal(t, scenario.serviceCode, serviceerrors.CodeOf(err))
			require.Equal(t, scenario.grpcCode, status.Code(err))
			require.NotContains(t, serviceerrors.MessageOf(err), "private")
		})
	}
}

type failedReservationRelease struct {
	*testStore
	calls      int
	contextErr error
	bounded    bool
}

func (s *failedReservationRelease) ReleaseCheckpointReservation(ctx context.Context, _, _ string, _ uuid.UUID) error {
	s.calls++
	s.contextErr = ctx.Err()
	deadline, ok := ctx.Deadline()
	s.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	return errors.New("private retirement connection detail")
}

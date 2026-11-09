package checkpoint

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

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

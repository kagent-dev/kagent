package checkpoint

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestCreateRetiresReservationThatCommitsAfterReplyLoss(t *testing.T) {
	dsn := dbtest.StartT(context.WithoutCancel(t.Context()), t)
	require.NoError(t, dbtest.Migrate(dsn, false))
	pool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn), Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	persistence := database.NewClient(pool, "public")
	source, head := recoverySource(t, persistence)
	gate := &reservationCommitGate{entered: make(chan struct{}), resume: make(chan struct{})}
	config := dbtest.PoolConfig(dsn)
	config.ConnConfig.Tracer = gate
	latePool, err := database.Connect(t.Context(), &database.PostgresConfig{Pool: config, Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(latePool.Close)
	var once sync.Once
	resume := func() { once.Do(func() { close(gate.resume) }) }
	t.Cleanup(resume)
	lost := &lostUncommittedReservation{Client: persistence, reserving: database.NewClient(latePool, "public"), gate: gate,
		committed: make(chan error, 1), releasing: make(chan struct{})}
	owner := auth.AuthSessionTo(t.Context(), testSession{userID: "alice"})
	tags := &testTags{snapshotURI: "s3://snapshots/source"}
	request := uuid.NewString()
	first := make(chan error, 1)
	go func() {
		_, err := NewService(lost, testAuthorizer{}, tags, nil).Create(owner, source.Id, request, head)
		first <- err
	}()
	select {
	case <-lost.releasing:
	case <-time.After(5 * time.Second):
		t.Fatal("reservation cleanup was not attempted")
	}
	select {
	case err := <-first:
		t.Fatalf("Create returned before retiring its uncommitted reservation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	resume()
	require.NoError(t, <-lost.committed, "the reservation must actually commit")
	require.Error(t, <-first, "reply loss remains an error after its claim is retired")
	require.Zero(t, tags.createCalls)
	service := NewService(persistence, testAuthorizer{}, tags, nil)
	result, err := service.Create(owner, source.Id, request, head)
	require.NoError(t, err, "the same request must reclaim immediately without waiting for expiry")
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, result.GetState())
	require.Equal(t, 1, tags.createCalls)
	require.NoError(t, service.Delete(owner, result.Id))
}

type reservationCommitGate struct {
	entered, resume chan struct{}
	once            sync.Once
}

func (g *reservationCommitGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.EqualFold(data.SQL, "commit") {
		g.once.Do(func() {
			close(g.entered)
			select {
			case <-g.resume:
			case <-ctx.Done():
			}
		})
	}
	return ctx
}

func (*reservationCommitGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// PostgreSQL still commits the real reservation; only its transport reply is
// interrupted before completion becomes visible. No stored phase is fabricated.
type lostUncommittedReservation struct {
	*database.Client
	reserving *database.Client
	gate      *reservationCommitGate
	committed chan error
	releasing chan struct{}
}

func (s *lostUncommittedReservation) ReserveSessionCheckpoint(ctx context.Context, checkpoint *apiv1.Checkpoint, owner, request string, executor uuid.UUID) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error) {
	attempt, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	go func() {
		defer cancel()
		_, _, err := s.reserving.ReserveSessionCheckpoint(attempt, checkpoint, owner, request, executor)
		s.committed <- err
	}()
	<-s.gate.entered
	return nil, nil, status.Error(codes.Unavailable, "reservation reply interrupted")
}

func (s *lostUncommittedReservation) ReleaseCheckpointReservation(ctx context.Context, sessionID, id string, executor uuid.UUID) error {
	close(s.releasing)
	return s.Client.ReleaseCheckpointReservation(ctx, sessionID, id, executor)
}

func (s *lostUncommittedReservation) ReleaseCheckpointOperation(ctx context.Context, id string, executor uuid.UUID) error {
	close(s.releasing)
	return s.Client.ReleaseCheckpointOperation(ctx, id, executor)
}

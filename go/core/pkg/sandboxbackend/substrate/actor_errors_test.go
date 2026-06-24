package substrate

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWrapResumeActorError_NoFreeWorkers(t *testing.T) {
	t.Parallel()

	grpcErr := status.Error(codes.FailedPrecondition, "no free workers available")
	got := wrapResumeActorError("actor-1", grpcErr)
	requireIsNoFreeWorkers(t, got)
}

func TestWrapResumeActorError_OtherError(t *testing.T) {
	t.Parallel()

	got := wrapResumeActorError("actor-1", fmt.Errorf("boom"))
	if errors.Is(got, ErrNoFreeWorkers) {
		t.Fatal("expected non-capacity error")
	}
}

func requireIsNoFreeWorkers(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrNoFreeWorkers) {
		t.Fatalf("errors.Is(err, ErrNoFreeWorkers) = false, err = %v", err)
	}
}

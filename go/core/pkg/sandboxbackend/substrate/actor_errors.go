package substrate

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNoFreeWorkers is returned when ate-api cannot assign a WorkerPool worker to resume an actor.
var ErrNoFreeWorkers = errors.New("substrate worker pool has no free workers; try again later or increase WorkerPool replicas")

func wrapResumeActorError(actorID string, err error) error {
	if err == nil {
		return nil
	}
	if isNoFreeWorkersError(err) {
		return fmt.Errorf("%w", ErrNoFreeWorkers)
	}
	return fmt.Errorf("substrate ResumeActor %q: %w", actorID, err)
}

func isNoFreeWorkersError(err error) bool {
	if errors.Is(err, ErrNoFreeWorkers) {
		return true
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.FailedPrecondition && strings.Contains(strings.ToLower(st.Message()), "no free workers") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "no free workers")
}

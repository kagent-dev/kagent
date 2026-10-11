package e2e_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
)

// scriptedSessions answers DeleteSession from a script of codes, repeating
// the last one once the script is used up. The embedded client is nil: the
// helper under test calls nothing else.
type scriptedSessions struct {
	apiv1alpha1.SessionServiceClient
	script []codes.Code
	calls  int
}

var _ apiv1alpha1.SessionServiceClient = (*scriptedSessions)(nil)

func (s *scriptedSessions) DeleteSession(context.Context, *apiv1alpha1.DeleteSessionRequest, ...grpc.CallOption) (*apiv1alpha1.DeleteSessionResponse, error) {
	s.calls++
	code := s.script[min(s.calls, len(s.script))-1]
	if code == codes.OK {
		return &apiv1alpha1.DeleteSessionResponse{}, nil
	}
	return nil, status.Errorf(code, "answer %d", s.calls)
}

// deleteSessionTestBackoff keeps the retries of a unit test to milliseconds.
var deleteSessionTestBackoff = wait.Backoff{Duration: time.Millisecond, Factor: 2, Steps: 10, Cap: 4 * time.Millisecond}

func TestDeleteSessionWhenIdleRetriesWhileLifecycleWorkOwnsTheSession(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		script []codes.Code
		calls  int
	}{
		{name: "aborted twice then ok", script: []codes.Code{codes.Aborted, codes.Aborted, codes.OK}, calls: 3},
		{name: "failed precondition then not found", script: []codes.Code{codes.FailedPrecondition, codes.NotFound}, calls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sessions := &scriptedSessions{script: test.script}
			if err := deleteSessionWhenIdle(t.Context(), sessions, "session", deleteSessionTestBackoff); err != nil {
				t.Fatalf("deleteSessionWhenIdle() = %v, want nil", err)
			}
			if sessions.calls != test.calls {
				t.Fatalf("DeleteSession calls = %d, want %d", sessions.calls, test.calls)
			}
		})
	}
}

func TestDeleteSessionWhenIdleFailsOnAnyOtherCode(t *testing.T) {
	t.Parallel()
	sessions := &scriptedSessions{script: []codes.Code{codes.PermissionDenied, codes.OK}}
	err := deleteSessionWhenIdle(t.Context(), sessions, "session", deleteSessionTestBackoff)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("deleteSessionWhenIdle() = %v, want %s", err, codes.PermissionDenied)
	}
	if sessions.calls != 1 {
		t.Fatalf("DeleteSession calls = %d, want 1", sessions.calls)
	}
}

func TestDeleteSessionWhenIdleFailsOnTheDeadlineWithTheLastAnswer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	sessions := &scriptedSessions{script: []codes.Code{codes.Aborted}}
	err := deleteSessionWhenIdle(ctx, sessions, "session", deleteSessionTestBackoff)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deleteSessionWhenIdle() = %v, want the deadline", err)
	}
	if !strings.Contains(err.Error(), "code = Aborted") {
		t.Fatalf("deleteSessionWhenIdle() = %v, want the last answer named", err)
	}
	if sessions.calls < 2 {
		t.Fatalf("DeleteSession calls = %d, want retries before the deadline", sessions.calls)
	}
}

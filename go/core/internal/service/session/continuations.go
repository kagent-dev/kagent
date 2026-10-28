package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	nativea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type continuationStore interface {
	RegisterTaskContinuation(context.Context, string, string, string) error
	ClaimTaskContinuations(context.Context, int) ([]database.TaskContinuation, error)
	CompleteTaskContinuation(context.Context, database.TaskContinuation, string) error
	LatestContinuationTask(context.Context, string, string) (*a2a.Task, error)
}

// Continuations resumes a caller after a synchronous delegation paused for input.
// Dependencies and native input IDs fence restarts and concurrent controllers.
type Continuations struct {
	store    continuationStore
	gateway  a2asrv.RequestHandler
	sessions *Service
}

var _ manager.Runnable = (*Continuations)(nil)
var _ manager.LeaderElectionRunnable = (*Continuations)(nil)

func NewContinuations(store continuationStore, gateway a2asrv.RequestHandler, sessions *Service) *Continuations {
	return &Continuations{store: store, gateway: gateway, sessions: sessions}
}

func (*Continuations) NeedLeaderElection() bool { return false }

// Register authorizes both sessions before persisting a native dependency.
func (c *Continuations) Register(ctx context.Context, parentID, childID, messageID string) error {
	if _, err := c.sessions.Get(ctx, parentID); err != nil {
		return err
	}
	if _, err := c.sessions.Get(ctx, childID); err != nil {
		return err
	}
	return c.store.RegisterTaskContinuation(ctx, parentID, childID, messageID)
}

// Start recovers pending dependencies on every controller replica.
func (c *Continuations) Start(ctx context.Context) error {
	ctx = auth.AuthSessionTo(ctx, auth.ControlPlaneSession{})
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := c.tick(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to resume delegated tasks", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// tick releases database claims before dispatching through the public gateway.
func (c *Continuations) tick(ctx context.Context) error {
	values, err := c.store.ClaimTaskContinuations(ctx, 4)
	if err != nil {
		return err
	}
	var group sync.WaitGroup
	for _, value := range values {
		group.Go(func() {
			attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			taskID, err := c.resume(attempt, value)
			if err != nil {
				logging.FromContext(ctx).WarnContext(ctx, "delegated task resume failed", "error", err, "continuation", value.ID)
			}
			if finishErr := c.store.CompleteTaskContinuation(attempt, value, taskID); finishErr != nil && ctx.Err() == nil {
				logging.FromContext(ctx).WarnContext(ctx, "failed to finish task continuation", "error", finishErr, "continuation", value.ID)
			}
		})
	}
	group.Wait()
	return nil
}

// resume uses native admission; a retry of an accepted message returns its task.
func (c *Continuations) resume(ctx context.Context, value database.TaskContinuation) (string, error) {
	parent, err := c.sessions.Get(ctx, value.ParentSessionID)
	if err != nil {
		return "", err
	}
	child, err := c.store.LatestContinuationTask(ctx, value.ChildSessionID, *value.ChildTaskID)
	if err != nil {
		return "", err
	}
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(fmt.Sprintf(
		"Delegated task %s settled with state %s.\n%s\nContinue from this result; reuse the existing child session if more work is required.", child.ID, child.Status.State, nativea2a.TaskText(child))))
	message.ID = "kagent-continuation-" + value.ID.String()
	message.ContextID = parent.Id
	message.ReferenceTasks = []a2a.TaskID{a2a.TaskID(value.ParentTaskID), a2a.TaskID(*value.ChildTaskID)}
	result, err := c.gateway.SendMessage(ctx, &a2a.SendMessageRequest{
		Tenant:  parent.GetAgent().GetNamespace() + "/" + parent.GetAgent().GetName(),
		Message: message, Config: &a2a.SendMessageConfig{ReturnImmediately: true},
	})
	if err != nil {
		return "", err
	}
	return string(result.TaskInfo().TaskID), nil
}

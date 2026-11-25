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

// continuationMessageIDPrefix identifies controller inputs that resume a
// delegating Session. Admission reserves it for the control plane.
const continuationMessageIDPrefix = "kagent-continuation-"

type delegatingSessionKey struct{}

// WithDelegatingSession asks A2A admission to record that sessionID waits for
// the task this send creates. Admission persists the dependency before
// dispatch, for new and existing target Sessions alike.
func WithDelegatingSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, delegatingSessionKey{}, sessionID)
}

// DelegatingSessionFrom returns the Session recorded by WithDelegatingSession.
func DelegatingSessionFrom(ctx context.Context) string {
	id, _ := ctx.Value(delegatingSessionKey{}).(string)
	return id
}

// registerContinuation authorizes both Sessions before persisting the dependency.
func (s *InteractionService) registerContinuation(ctx context.Context, childID, messageID string) error {
	parentID := DelegatingSessionFrom(ctx)
	if parentID == "" {
		return nil
	}
	for _, id := range []string{parentID, childID} {
		if _, err := s.sessions.getAuthorized(ctx, id, auth.VerbGet); err != nil {
			return interactionError(ctx, err)
		}
	}
	if err := s.store.RegisterTaskContinuation(ctx, parentID, childID, messageID); err != nil {
		return interactionStoreError(ctx, err)
	}
	return nil
}

type continuationStore interface {
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
			completion, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer finish()
			if finishErr := c.store.CompleteTaskContinuation(completion, value, taskID); finishErr != nil && ctx.Err() == nil {
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
	if _, err := c.sessions.Get(ctx, value.ChildSessionID); err != nil {
		return "", err
	}
	child, err := c.store.LatestContinuationTask(ctx, value.ChildSessionID, *value.ChildTaskID)
	if err != nil {
		return "", err
	}
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(fmt.Sprintf(
		"Delegated task %s settled with state %s.\n%s\nContinue from this result; reuse the existing child session if more work is required.", child.ID, child.Status.State, nativea2a.TaskText(child))))
	message.ID = continuationMessageIDPrefix + value.ID.String()
	message.ContextID = parent.Id
	message.ReferenceTasks = []a2a.TaskID{a2a.TaskID(value.ParentTaskID), a2a.TaskID(*value.ChildTaskID)}
	result, err := c.gateway.SendMessage(ctx, &a2a.SendMessageRequest{
		Tenant:  parent.GetAgent().GetRef().GetNamespace() + "/" + parent.GetAgent().GetRef().GetName(),
		Message: message, Config: &a2a.SendMessageConfig{ReturnImmediately: true},
	})
	if err != nil {
		return "", err
	}
	return string(result.TaskInfo().TaskID), nil
}

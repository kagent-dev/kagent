package a2a

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const (
	defaultTaskPageSize = 50
	maxTaskPageSize     = 100
)

// TaskStore is the subset of the persistent store ListTasks reads from.
// *database.Client satisfies it.
type TaskStore interface {
	GetSession(ctx context.Context, sessionID, userID string) (*dbpkg.Session, error)
	ListSessions(ctx context.Context, userID string) ([]dbpkg.Session, error)
	ListTasksForSession(ctx context.Context, sessionID string) ([]*a2atype.Task, error)
}

// storeTaskQueryHandler answers ListTasks from kagent's task store, which is
// the source of truth for persisted tasks. Every other method (including
// GetTask, which already resolves to the same store via the passthrough) is
// delegated to the embedded handler unchanged.
type storeTaskQueryHandler struct {
	a2asrv.RequestHandler
	store TaskStore
}

func newStoreTaskQueryHandler(delegate a2asrv.RequestHandler, store TaskStore) *storeTaskQueryHandler {
	return &storeTaskQueryHandler{RequestHandler: delegate, store: store}
}

// callerUserID returns the authenticated principal's user id, or "" when the
// request carries no user identity. Task queries are scoped to this id so a
// caller can never read another user's tasks.
func callerUserID(ctx context.Context) string {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return ""
	}
	return session.Principal().User.ID
}

// effectiveUserIDForContext returns the user id tasks are read as. A valid
// share token grants access to a single session, so when the request carries a
// ShareContext for exactly the requested contextId the owner's id is used
// (mirroring getEffectiveUserIDForSession in the session handlers); otherwise
// the caller's own id is used. A share token never widens an all-sessions
// query, so contextId must be set for it to apply.
func effectiveUserIDForContext(ctx context.Context, contextID string) string {
	if contextID != "" {
		if sc, ok := auth.ShareContextFrom(ctx); ok && sc.SessionID == contextID {
			return sc.UserID
		}
	}
	return callerUserID(ctx)
}

func (h *storeTaskQueryHandler) ListTasks(ctx context.Context, req *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	pageSize := clampPageSize(req.PageSize)

	userID := effectiveUserIDForContext(ctx, req.ContextID)
	if userID == "" {
		return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{}, PageSize: pageSize}, nil
	}

	tasks, err := h.collectUserTasks(ctx, userID, req.ContextID)
	if err != nil {
		return nil, err
	}

	filtered := filterTasks(tasks, req)
	// Order by task id so the page-token offset is stable across calls: task
	// ids are immutable, unlike session updated_at (which reorders on writes).
	slices.SortFunc(filtered, func(a, b *a2atype.Task) int { return cmp.Compare(a.ID, b.ID) })

	offset, err := decodePageToken(req.PageToken)
	if err != nil {
		return nil, a2atype.NewError(a2atype.ErrInvalidParams, "invalid pageToken")
	}
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := min(offset+pageSize, total)

	page := filtered[offset:end]
	shaped := make([]*a2atype.Task, 0, len(page))
	for _, t := range page {
		shaped = append(shaped, shapeTask(t, req.HistoryLength, req.IncludeArtifacts))
	}

	nextToken := ""
	if end < total {
		nextToken = encodePageToken(end)
	}

	return &a2atype.ListTasksResponse{
		Tasks:         shaped,
		PageSize:      pageSize,
		TotalSize:     total,
		NextPageToken: nextToken,
	}, nil
}

// collectUserTasks returns the caller's tasks, either for a single session
// (contextId) or across every session the user owns. Both paths are strictly
// scoped to userID.
func (h *storeTaskQueryHandler) collectUserTasks(ctx context.Context, userID, contextID string) ([]*a2atype.Task, error) {
	if contextID != "" {
		if _, err := h.store.GetSession(ctx, contextID, userID); err != nil {
			// Session does not exist or is not the caller's: no tasks to return.
			return nil, nil
		}
		return h.store.ListTasksForSession(ctx, contextID)
	}

	sessions, err := h.store.ListSessions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	var all []*a2atype.Task
	for _, s := range sessions {
		tasks, err := h.store.ListTasksForSession(ctx, s.ID)
		if err != nil {
			return nil, fmt.Errorf("list tasks for session %s: %w", s.ID, err)
		}
		all = append(all, tasks...)
	}
	return all, nil
}

func filterTasks(tasks []*a2atype.Task, req *a2atype.ListTasksRequest) []*a2atype.Task {
	filtered := make([]*a2atype.Task, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if req.Status != a2atype.TaskStateUnspecified && t.Status.State != req.Status {
			continue
		}
		if req.StatusTimestampAfter != nil {
			ts := t.Status.Timestamp
			if ts == nil || !ts.After(*req.StatusTimestampAfter) {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// shapeTask returns a copy of task with history capped and artifacts included
// only when requested. includeArtifacts defaults to false, in which case
// artifacts are omitted entirely (nil slice + omitempty).
func shapeTask(task *a2atype.Task, historyLength *int, includeArtifacts bool) *a2atype.Task {
	shaped := *task
	shaped.History = truncateHistory(task.History, historyLength)
	if includeArtifacts {
		shaped.Artifacts = task.Artifacts
	} else {
		shaped.Artifacts = nil
	}
	return &shaped
}

// truncateHistory keeps the most recent n messages. A nil historyLength keeps
// the full history; n <= 0 drops it entirely.
func truncateHistory(history []*a2atype.Message, historyLength *int) []*a2atype.Message {
	if historyLength == nil {
		return history
	}
	n := *historyLength
	if n <= 0 {
		return nil
	}
	if n >= len(history) {
		return history
	}
	return history[len(history)-n:]
}

func clampPageSize(size int) int {
	switch {
	case size <= 0:
		return defaultTaskPageSize
	case size > maxTaskPageSize:
		return maxTaskPageSize
	default:
		return size
	}
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid page token")
	}
	return offset, nil
}

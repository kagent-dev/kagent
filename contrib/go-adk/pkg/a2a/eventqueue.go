package a2a

import (
	"context"
	"fmt"
	"maps"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
	"google.golang.org/adk/server/adka2a"
)

// EventQueue wraps an eventqueue.Queue to bridge the gap between the Go-ADK
// (which sends streaming text as TaskArtifactUpdateEvent) and the UI (which
// only streams text from TaskStatusUpdateEvent).
//
// For each artifact update it emits a mirror TaskStatusUpdateEvent:
//   - Partial artifacts: text-only parts with adk_partial metadata (stripped
//     by the taskstore before persistence) so the UI can display streaming text.
//   - Non-partial artifacts: all parts, for history population.
//
// When the final status event arrives without a message, the last non-partial
// artifact text is injected so the UI has content to display.
//
// BYO executors can create an EventQueue in their Execute method:
//
//	func (e *MyExecutor) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
//	    q := a2a.NewEventQueue(queue, reqCtx)
//	    // write artifact and status events to q ...
//	}
//
// For opaque executors (e.g. adka2a.Executor), use WrapExecutorQueue instead.
type EventQueue struct {
	eventqueue.Queue
	reqCtx *a2asrv.RequestContext

	lastTextParts a2atype.ContentParts
	lastTextMeta  map[string]any
}

// NewEventQueue creates an EventQueue that wraps inner and mirrors artifact
// events as status events for UI streaming and history population.
func NewEventQueue(inner eventqueue.Queue, reqCtx *a2asrv.RequestContext) *EventQueue {
	return &EventQueue{Queue: inner, reqCtx: reqCtx}
}

// WrapExecutorQueue wraps an a2asrv.AgentExecutor so that each execution's
// queue is automatically wrapped with an EventQueue. Use this for opaque
// executors where you cannot modify the Execute method.
func WrapExecutorQueue(inner a2asrv.AgentExecutor) a2asrv.AgentExecutor {
	return &executorQueueWrapper{inner: inner}
}

type executorQueueWrapper struct {
	inner a2asrv.AgentExecutor
}

var _ a2asrv.AgentExecutor = (*executorQueueWrapper)(nil)

func (w *executorQueueWrapper) Execute(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	if reqCtx.Message == nil {
		return fmt.Errorf("A2A request message cannot be nil")
	}
	return w.inner.Execute(ctx, reqCtx, NewEventQueue(queue, reqCtx))
}

func (w *executorQueueWrapper) Cancel(ctx context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	return w.inner.Cancel(ctx, reqCtx, queue)
}

func (q *EventQueue) Write(ctx context.Context, event a2atype.Event) error {
	switch ev := event.(type) {
	case *a2atype.TaskArtifactUpdateEvent:
		return q.handleArtifactEvent(ctx, ev)
	case *a2atype.TaskStatusUpdateEvent:
		return q.handleStatusEvent(ctx, ev)
	default:
		return q.Queue.Write(ctx, event)
	}
}

func (q *EventQueue) handleArtifactEvent(ctx context.Context, ev *a2atype.TaskArtifactUpdateEvent) error {
	parts := filterNonEmptyParts(ev.Artifact.Parts)
	if len(parts) == 0 {
		return nil
	}

	isPartial := adka2a.IsPartial(ev.Metadata)

	var mirrorParts a2atype.ContentParts
	var mirrorMeta map[string]any

	if isPartial {
		mirrorParts = filterTextParts(parts)
		mirrorMeta = maps.Clone(ev.Metadata)
	} else if !ev.LastChunk {
		mirrorParts = parts
		if tp := filterTextParts(parts); len(tp) > 0 {
			q.lastTextParts = tp
			q.lastTextMeta = ev.Metadata
		}
	}

	if len(mirrorParts) > 0 {
		msg := a2atype.NewMessage(a2atype.MessageRoleAgent, mirrorParts...)
		msg.Metadata = mirrorMeta
		status := a2atype.NewStatusUpdateEvent(q.reqCtx, a2atype.TaskStateWorking, msg)
		status.Metadata = mirrorMeta
		if err := q.Queue.Write(ctx, status); err != nil {
			return fmt.Errorf("mirror status write failed: %w", err)
		}
	}

	return q.Queue.Write(ctx, ev)
}

func (q *EventQueue) handleStatusEvent(ctx context.Context, ev *a2atype.TaskStatusUpdateEvent) error {
	if ev.Final && ev.Status.Message == nil && len(q.lastTextParts) > 0 {
		msg := a2atype.NewMessage(a2atype.MessageRoleAgent, q.lastTextParts...)
		msg.Metadata = maps.Clone(q.lastTextMeta)
		ev.Status.Message = msg
	}
	return q.Queue.Write(ctx, ev)
}

// ---------------------------------------------------------------------------
// Part filters
// ---------------------------------------------------------------------------

// isEmptyDataPart returns true if the part is a DataPart with nil or empty Data.
// The ADK processor emits such parts as cleanup signals for streaming partial
// artifacts and as a fallback for unrecognized GenAI part types.
func isEmptyDataPart(part a2atype.Part) bool {
	dp, ok := part.(a2atype.DataPart)
	return ok && len(dp.Data) == 0
}

// filterNonEmptyParts returns a2a parts with empty DataParts removed.
// Returns nil when no parts pass the filter.
func filterNonEmptyParts(parts a2atype.ContentParts) a2atype.ContentParts {
	var filtered a2atype.ContentParts
	for _, p := range parts {
		if !isEmptyDataPart(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// filterTextParts returns only TextParts from the given parts.
func filterTextParts(parts a2atype.ContentParts) a2atype.ContentParts {
	var out a2atype.ContentParts
	for _, p := range parts {
		if _, ok := p.(a2atype.TextPart); ok {
			out = append(out, p)
		}
	}
	return out
}

package anthropicselfhostedsandboxworker

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type sessionEvent struct {
	id, kind, thread, toolID, permission, verdict, name string
	input                                               any
}

type pendingTool struct {
	event sessionEvent
	order int
}

type sessionJournal struct {
	seen    map[string]bool
	pending map[string]pendingTool
}

func (j *sessionJournal) accept(e sessionEvent) (changed []pendingTool, settled, interrupted []string, err error) {
	if e.id != "" {
		if j.seen[e.id] {
			return nil, nil, nil, nil
		}
		j.seen[e.id] = true
	}
	switch e.kind {
	case "agent.tool_use":
		if e.id != "" && e.permission != "deny" {
			p := pendingTool{event: e, order: len(j.seen)}
			j.pending[e.id] = p
			changed = append(changed, p)
		}
	case "user.tool_confirmation":
		if p, ok := j.pending[e.toolID]; ok {
			if e.verdict == "allow" {
				p.event.permission = "allow"
				j.pending[e.toolID] = p
				changed = append(changed, p)
			} else {
				delete(j.pending, e.toolID)
				interrupted = append(interrupted, e.toolID)
			}
		}
	case "user.tool_result":
		delete(j.pending, e.toolID)
		settled = append(settled, e.toolID)
	case "user.interrupt":
		for id, p := range j.pending {
			if e.thread == "" || p.event.thread == e.thread {
				delete(j.pending, id)
				interrupted = append(interrupted, id)
			}
		}
	case "session.status_terminated", "session.deleted":
		return nil, nil, nil, errSessionEnded
	}
	return changed, settled, interrupted, nil
}

func (r *Runner) journalEvent(ctx context.Context, w assignment, pending []pendingTool, settled, interrupted []string) error {
	call, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	if err := r.store.settle(call, w, settled); err != nil {
		return err
	}
	if err := r.store.interrupt(call, w, interrupted); err != nil {
		return err
	}
	for _, p := range pending {
		e := p.event
		if e.permission != "" && e.permission != "allow" {
			continue
		}
		input, err := json.Marshal(e.input)
		if err != nil {
			return err
		}
		if err := r.store.addCommand(call, w, command{id: e.id, name: e.name, input: input}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) events(ctx context.Context, w assignment, auth []option.RequestOption, ready, wake chan<- struct{}, activity *atomic.Int64) error {
	api := r.provider.Beta.Sessions.Events
	// Open SSE first so events generated during history replay are buffered.
	stream := api.StreamEvents(ctx, w.sessionID, anthropic.BetaSessionEventStreamParams{}, auth...)
	defer func() {
		if err := stream.Close(); err != nil {
			r.warn(ctx, "close event stream", providerError("close stream", err))
		}
	}()
	if err := stream.Err(); err != nil {
		return streamError(err)
	}
	history, cancel := context.WithTimeout(ctx, 3*apiTimeout)
	defer cancel()
	pager := api.ListAutoPaging(history, w.sessionID, anthropic.BetaSessionEventListParams{
		Limit: anthropic.Int(1000), Order: anthropic.BetaSessionEventListParamsOrderAsc,
	}, auth...)
	journal := sessionJournal{seen: make(map[string]bool), pending: make(map[string]pendingTool)}
	var settled, interrupted []string
	for pager.Next() {
		e := pager.Current()
		_, done, dropped, err := journal.accept(sessionEvent{
			id: e.ID, kind: e.Type, thread: e.SessionThreadID, toolID: e.ToolUseID, name: e.Name, input: e.Input,
			permission: string(e.EvaluatedPermission), verdict: string(e.AsUserToolConfirmation().Result),
		})
		if err != nil {
			return err
		}
		settled, interrupted = append(settled, done...), append(interrupted, dropped...)
	}
	if err := pager.Err(); err != nil {
		return streamError(err)
	}
	cancel()
	pending := make([]pendingTool, 0, len(journal.pending))
	for _, p := range journal.pending {
		pending = append(pending, p)
	}
	slices.SortFunc(pending, func(a, b pendingTool) int { return a.order - b.order })
	if err := r.journalEvent(ctx, w, pending, settled, interrupted); err != nil {
		return err
	}
	close(ready)
	for stream.Next() {
		e := stream.Current()
		if !journal.seen[e.ID] && !strings.HasPrefix(e.Type, "span.") && !strings.HasPrefix(e.Type, "event_") {
			markActivity(activity)
		}
		changed, done, dropped, err := journal.accept(sessionEvent{
			id: e.ID, kind: e.Type, thread: e.SessionThreadID, toolID: e.ToolUseID, name: e.Name, input: e.Input,
			permission: string(e.EvaluatedPermission), verdict: string(e.AsUserToolConfirmation().Result),
		})
		if err != nil {
			return err
		}
		if len(changed)+len(done)+len(dropped) > 0 {
			if err := r.journalEvent(ctx, w, changed, done, dropped); err != nil {
				return err
			}
			// A buffered hint covers the race between an empty scan and waiting.
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
	if err := stream.Err(); err != nil {
		return streamError(err)
	}
	return errors.New("event stream disconnected")
}

func streamError(err error) error {
	if providerStatus(err, 404) || providerStatus(err, 410) {
		return errSessionEnded
	}
	return providerError("read session events", err)
}

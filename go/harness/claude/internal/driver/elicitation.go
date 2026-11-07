package driver

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// observedElicitation is a URL mode elicitation the Elicitation hook answered
// with cancel while the process ran. It becomes durable only when the process
// ends without another pending input.
type observedElicitation struct {
	SessionID     string
	Server        string
	Message       string
	URL           string
	ElicitationID string
}

// validateElicitation checks a record that parks a URL elicitation. The call
// that asked for it has already ended, so the answer resumes the session with
// a prompt rather than replaying the call.
func (r *deferredRecord) validateElicitation() error {
	e := r.Elicitation
	if e.ID != r.RequestID || e.Server == "" || apia2a.ValidateElicitationURL(e.URL) != nil ||
		r.Tool.ID != "" || r.Tool.Name != "" || r.Tool.Input != nil || r.Decision != nil || r.Answer != nil || r.Canceling {
		return fmt.Errorf("invalid Claude URL elicitation")
	}
	if r.Action != nil {
		switch r.Action.Action {
		case apia2a.URLElicitationAccept, apia2a.URLElicitationDecline, apia2a.URLElicitationCancel:
		default:
			return fmt.Errorf("invalid Claude URL elicitation action %q", r.Action.Action)
		}
		if r.Action.ID != r.RequestID {
			return fmt.Errorf("Claude URL elicitation action answers another request")
		}
	}
	switch r.Phase {
	case phaseWaiting:
		if r.Action != nil {
			return fmt.Errorf("waiting Claude URL elicitation already has an action")
		}
	case phaseResuming, phaseComplete:
		if r.Action == nil {
			return fmt.Errorf("Claude URL elicitation continuation has no action")
		}
	case phaseCanceled, phaseAbandoned:
	default:
		return fmt.Errorf("unknown Claude URL elicitation phase %q", r.Phase)
	}
	return nil
}

// elicitationPrompt tells the model what the person did. It never contains the URL.
func (r *deferredRecord) elicitationPrompt() string {
	server := r.Elicitation.Server
	switch r.Action.Action {
	case apia2a.URLElicitationAccept:
		return fmt.Sprintf("The person completed the interaction that the MCP server %q asked for at a URL. Retry the tool call that needed it.", server)
	case apia2a.URLElicitationCancel:
		return fmt.Sprintf("The person canceled the interaction that the MCP server %q asked for at a URL, so the tool call that needed it cannot complete. Do not retry it.", server)
	default:
		return fmt.Sprintf("The person declined the interaction that the MCP server %q asked for at a URL, so the tool call that needed it cannot complete. Do not retry it.", server)
	}
}

func (d *ProcessDriver) parkElicitation(turn runtime.Turn, session string, observed *observedElicitation) (runtime.Outcome, error) {
	b := d.config.ApprovalBroker
	id := uuid.NewString()
	r := deferredRecord{
		Version: 1, TaskID: turn.TaskID, ContextID: turn.ContextID, Origin: turn.Origin, SessionID: session, RequestID: id, Phase: phaseWaiting,
		Elicitation: &runtime.URLElicitationRequest{ID: id, Server: observed.Server, Message: observed.Message, URL: observed.URL, ElicitationID: observed.ElicitationID},
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := saveDeferred(b.directory, d.config.MaxEventBytes, &r); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Outcome{Pending: &elicitationTurn{driver: d, record: r}}, nil
}

type elicitationTurn struct {
	driver *ProcessDriver
	record deferredRecord
}

func (p *elicitationTurn) Request() runtime.InputRequest { return p.record.Elicitation }

func (p *elicitationTurn) TraceOrigin() runtime.TraceOrigin { return p.record.Origin }

// current requires the broker mutex. Disk is authoritative after DATA restore.
func (p *elicitationTurn) current() (*deferredRecord, error) {
	b := p.driver.config.ApprovalBroker
	r, err := loadDeferred(b.directory, p.driver.config.MaxEventBytes)
	if err != nil {
		return nil, err
	}
	if r == nil || r.Elicitation == nil || r.RequestID != p.record.RequestID || r.TaskID != p.record.TaskID || r.ContextID != p.record.ContextID || r.SessionID != p.record.SessionID {
		return nil, fmt.Errorf("Claude URL elicitation no longer matches")
	}
	return r, nil
}

// transition moves the record from one of from to phase.
func (p *elicitationTurn) transition(phase string, action *runtime.URLElicitationResponse, from ...string) error {
	b := p.driver.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := p.current()
	if err != nil {
		return err
	}
	if !slices.Contains(from, r.Phase) {
		return fmt.Errorf("Claude URL elicitation is %s, not %v", r.Phase, from)
	}
	r.Phase, r.Action = phase, action
	return saveDeferred(b.directory, p.driver.config.MaxEventBytes, r)
}

// Resume starts the retained session again with a prompt that tells the model
// the person's answer. A process that never started leaves the input waiting.
func (p *elicitationTurn) Resume(ctx context.Context, response runtime.InputResponse, sink runtime.EventSink) (outcome runtime.Outcome, returnErr error) {
	answer, ok := response.(*runtime.URLElicitationResponse)
	if !ok {
		return runtime.Outcome{}, fmt.Errorf("Claude is waiting for a URL elicitation response, not %T", response)
	}
	if answer.ID != p.record.RequestID {
		return runtime.Outcome{}, fmt.Errorf("URL elicitation response %q does not match pending request %q", answer.ID, p.record.RequestID)
	}
	if err := p.transition(phaseResuming, answer, phaseWaiting); err != nil {
		return runtime.Outcome{}, err
	}
	defer func() {
		if returnErr != nil && returnErr != context.Canceled {
			returnErr = p.abandon(returnErr)
		}
	}()
	r := p.record
	r.Action = answer
	outcome, err := p.driver.run(ctx, runtime.Turn{TaskID: r.TaskID, ContextID: r.ContextID, ContinuationID: r.SessionID, Origin: r.Origin, Prompt: r.elicitationPrompt()}, sink, nil)
	var startErr *processStartError
	if errors.As(err, &startErr) {
		if restoreErr := p.transition(phaseWaiting, nil, phaseResuming); restoreErr != nil {
			return runtime.Outcome{}, errors.Join(err, restoreErr)
		}
		logging.FromContext(ctx).ErrorContext(ctx, "failed to start claude after a URL elicitation; input remains waiting", "error", err)
		return runtime.Outcome{Pending: p}, nil
	}
	if ctx.Err() != nil {
		// The resumed process ended like any canceled turn; its session stays usable.
		if cancelErr := p.transition(phaseCanceled, answer, phaseResuming); cancelErr != nil {
			return runtime.Outcome{}, &runtime.CancellationError{Err: cancelErr}
		}
		return runtime.Outcome{}, context.Canceled
	}
	if err != nil || outcome.Pending != nil {
		return outcome, err // Another input request has already replaced the record.
	}
	if err := p.transition(phaseComplete, answer, phaseResuming); err != nil {
		return runtime.Outcome{}, err
	}
	return outcome, nil
}

// Cancel writes the tombstone. No process holds the elicitation: the call that
// asked for it already ended, so the next turn resumes the session as usual.
func (p *elicitationTurn) Cancel(context.Context, runtime.ModelCallSink) error {
	return p.transition(phaseCanceled, nil, phaseWaiting)
}

// abandon prevents a failed resume from being restored as pending input.
func (p *elicitationTurn) abandon(cause error) error {
	b := p.driver.config.ApprovalBroker
	b.mu.Lock()
	defer b.mu.Unlock()
	r, err := p.current()
	if err != nil {
		return errors.Join(cause, err)
	}
	if r.Phase == phaseComplete || r.Phase == phaseCanceled {
		return cause
	}
	r.Phase = phaseAbandoned
	if err := saveDeferred(b.directory, p.driver.config.MaxEventBytes, r); err != nil {
		return errors.Join(cause, err)
	}
	return errors.Join(runtime.ErrSessionAbandoned, cause)
}

// restoreElicitation requires the broker mutex.
func (d *ProcessDriver) restoreElicitation(r *deferredRecord) (string, string, runtime.PendingTurn, error) {
	if r.Phase != phaseWaiting {
		return "", "", nil, runtime.ErrSessionAbandoned
	}
	return r.TaskID, r.ContextID, &elicitationTurn{driver: d, record: *r}, nil
}

var _ runtime.PendingTurn = (*elicitationTurn)(nil)

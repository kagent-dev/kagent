package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	adksession "google.golang.org/adk/session"
)

const (
	jsonPreviewMaxLength = 500
	eventPersistTimeout  = 30 * time.Second
)

// Compile-time interface compliance check
var _ adksession.Service = (*SessionServiceAdapter)(nil)

// SessionServiceAdapter adapts our SessionService to Google ADK's session.Service.
type SessionServiceAdapter struct {
	service SessionService
}

// NewSessionServiceAdapter creates a new adapter.
func NewSessionServiceAdapter(service SessionService) *SessionServiceAdapter {
	return &SessionServiceAdapter{service: service}
}

// Create implements session.Service.
func (a *SessionServiceAdapter) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	if a.service == nil {
		return nil, fmt.Errorf("session service is nil")
	}

	state := make(map[string]any)
	if req.State != nil {
		state = req.State
	}

	session, err := a.service.CreateSession(ctx, req.AppName, req.UserID, state, req.SessionID)
	if err != nil {
		return nil, err
	}

	return &adksession.CreateResponse{
		Session: convertSessionToADK(session),
	}, nil
}

// Get implements session.Service.
func (a *SessionServiceAdapter) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	log := logr.FromContextOrDiscard(ctx)

	if a.service == nil {
		return nil, fmt.Errorf("session service is nil")
	}

	log.V(1).Info("SessionServiceAdapter.Get called", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)

	session, err := a.service.GetSession(ctx, req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}

	if session == nil {
		log.Info("Session not found, returning nil")
		return &adksession.GetResponse{Session: nil}, nil
	}

	log.V(1).Info("Session loaded from backend", "sessionID", session.ID, "eventsBeforeParse", len(session.Events))
	for i, e := range session.Events {
		log.V(1).Info("Event type before parseEventsToADK", "eventIndex", i, "type", fmt.Sprintf("%T", e))
	}

	session.Events = parseEventsToADK(ctx, session.Events)

	log.V(1).Info("Session events after parsing", "sessionID", session.ID, "eventsAfterParse", len(session.Events))

	return &adksession.GetResponse{
		Session: convertSessionToADK(session),
	}, nil
}

// List implements session.Service.
func (a *SessionServiceAdapter) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("List called but not fully implemented - returning empty list", "appName", req.AppName, "userID", req.UserID)
	return &adksession.ListResponse{
		Sessions: []adksession.Session{},
	}, nil
}

// Delete implements session.Service.
func (a *SessionServiceAdapter) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	if a.service == nil {
		return fmt.Errorf("session service is nil")
	}
	return a.service.DeleteSession(ctx, req.AppName, req.UserID, req.SessionID)
}

// AppendEvent implements session.Service.
func (a *SessionServiceAdapter) AppendEvent(ctx context.Context, session adksession.Session, event *adksession.Event) error {
	if a.service == nil {
		return fmt.Errorf("session service is nil")
	}
	if event == nil {
		return nil
	}

	// Persist remotely first so local state is not updated if remote fails.
	// Use detached context so client disconnect does not cancel persistence.
	persistCtx, cancel := context.WithTimeout(context.Background(), eventPersistTimeout)
	defer cancel()
	ourSession := convertADKSessionToOurs(session)
	if err := a.service.AppendEvent(persistCtx, ourSession, event); err != nil {
		return err
	}

	if ls, ok := session.(*localSession); ok {
		if err := ls.appendEvent(event); err != nil {
			return err
		}
	}

	return nil
}

// convertSessionToADK converts our Session to a localSession implementing adksession.Session.
func convertSessionToADK(sess *Session) adksession.Session {
	adkEvents := make([]*adksession.Event, 0, len(sess.Events))
	for _, e := range sess.Events {
		if adkE, ok := e.(*adksession.Event); ok {
			adkEvents = append(adkEvents, adkE)
		}
	}
	st := sess.State
	if st == nil {
		st = make(map[string]any)
	}
	return &localSession{
		appName:   sess.AppName,
		userID:    sess.UserID,
		sessionID: sess.ID,
		events:    adkEvents,
		state:     st,
	}
}

// convertADKSessionToOurs converts an adksession.Session back to our Session.
func convertADKSessionToOurs(s adksession.Session) *Session {
	state := make(map[string]any)
	for k, v := range s.State().All() {
		state[k] = v
	}
	return &Session{
		ID:      s.ID(),
		UserID:  s.UserID(),
		AppName: s.AppName(),
		State:   state,
		Events:  nil,
	}
}

// parseEventsToADK converts backend event payloads to *adksession.Event.
func parseEventsToADK(ctx context.Context, events []any) []any {
	log := logr.FromContextOrDiscard(ctx)
	out := make([]any, 0, len(events))
	skipped := 0
	for i, e := range events {
		if e == nil {
			skipped++
			continue
		}
		if adkE, ok := e.(*adksession.Event); ok {
			out = append(out, adkE)
			continue
		}

		var data []byte
		var err error
		if m, ok := e.(map[string]any); ok {
			data, err = json.Marshal(m)
			if err != nil {
				log.Info("Failed to marshal map event for ADK parse", "error", err, "eventIndex", i)
				skipped++
				continue
			}
		} else if s, ok := e.(string); ok {
			data = []byte(s)
		} else {
			skipped++
			log.Info("Event is neither *adksession.Event, map, nor string, skipping", "eventIndex", i, "type", fmt.Sprintf("%T", e))
			continue
		}

		adkE := parseRawToADKEvent(ctx, data)
		if adkE != nil {
			out = append(out, adkE)
		} else {
			skipped++
			jsonStr := string(data)
			if len(jsonStr) > jsonPreviewMaxLength {
				jsonStr = jsonStr[:jsonPreviewMaxLength] + "..."
			}
			log.Info("Event failed to parse as ADK Event, skipping", "eventIndex", i, "jsonPreview", jsonStr)
		}
	}
	if len(out) > 0 || skipped > 0 {
		log.V(1).Info("parseEventsToADK completed", "inputCount", len(events), "outputCount", len(out), "skippedCount", skipped)
	}
	return out
}

// parseRawToADKEvent unmarshals JSON bytes into *adksession.Event.
func parseRawToADKEvent(ctx context.Context, data []byte) *adksession.Event {
	log := logr.FromContextOrDiscard(ctx)
	e := new(adksession.Event)
	if err := json.Unmarshal(data, e); err != nil {
		log.Info("Failed to parse event as ADK Event", "error", err, "dataLength", len(data))
		return nil
	}

	log.V(1).Info("Parsed ADK Event fields",
		"author", e.Author,
		"invocationID", e.InvocationID,
		"partial", e.Partial,
		"hasLLMResponseContent", e.LLMResponse.Content != nil,
		"llmResponseFinishReason", e.LLMResponse.FinishReason)

	hasContent := e.LLMResponse.Content != nil
	hasAuthor := e.Author != ""
	hasInvocationID := e.InvocationID != ""
	hasLLMResponseData := e.LLMResponse.FinishReason != "" || e.Partial

	if !hasContent && !hasAuthor && !hasInvocationID && !hasLLMResponseData {
		log.Info("Parsed ADK Event has no meaningful content, treating as parse failure")
		return nil
	}
	return e
}

package adk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"iter"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	adksession "google.golang.org/adk/session"
)

// Compile-time interface compliance checks
var _ adksession.Service = (*SessionServiceAdapter)(nil)
var _ adksession.Session = (*SessionWrapper)(nil)
var _ adksession.Events = (*EventsWrapper)(nil)
var _ adksession.State = (*StateWrapper)(nil)

// ErrListNotImplemented is returned when List is called but not implemented.
var ErrListNotImplemented = errors.New("session list not implemented: underlying SessionService does not support listing")

// SessionServiceAdapter adapts our SessionService to Google ADK's session.Service.
//
// Session storage with Google ADK:
//   - Yes: we implement adk session.Service (Create, Get, List, Delete, AppendEvent).
//     The runner and adk-go only see ADK types (Session, Event). We adapt our backend
//     (our Session/Event) to that interface.
//   - adk-go provides session.Service interface and session.InMemoryService(); we
//     provide an implementation that delegates to our SessionService (e.g. KAgent API).
//
// Python (kagent-adk _session_service.py):
//   - KAgentSessionService(BaseSessionService) implements the ADK session service.
//   - create_session: POST /api/sessions → returns Session(id, user_id, state, app_name).
//   - get_session: GET /api/sessions/{id}?user_id=... → session + events; events loaded
//     as Event.model_validate_json(event_data["data"]) (ADK Event JSON).
//   - append_event: POST /api/sessions/{id}/events with {"id": event.id, "data": event.model_dump_json()};
//     then session.last_update_time = event.timestamp; super().append_event(session, event).
//   - So Python stores ADK Event JSON and keeps Session/Event as google.adk types end-to-end.
//
// We could make Go storage fully ADK-native by having the backend store/load ADK Event
// JSON (like Python) and Get() return a session whose Events() yields only *adksession.Event;
// we already append *adksession.Event in context; persistence still uses our Event for now.
type SessionServiceAdapter struct {
	service core.SessionService
	logger  logr.Logger
}

// NewSessionServiceAdapter creates a new adapter
func NewSessionServiceAdapter(service core.SessionService, logger logr.Logger) *SessionServiceAdapter {
	return &SessionServiceAdapter{
		service: service,
		logger:  logger,
	}
}

// AppendFirstSystemEvent appends the initial system event (header_update) before run.
// Matches Python _handle_request: append_event before runner.run_async.
// Ensures session has prior state; runner fetches session with full history for LLM context on resume.
func AppendFirstSystemEvent(ctx context.Context, service core.SessionService, session *core.Session) error {
	if service == nil || session == nil {
		return nil
	}
	event := map[string]interface{}{
		"InvocationID": "header_update",
		"Author":       "system",
	}
	return service.AppendEvent(ctx, session, event)
}

// Create implements session.Service interface
func (a *SessionServiceAdapter) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	if a.service == nil {
		return nil, fmt.Errorf("session service is nil")
	}

	// Convert Google ADK CreateRequest to our format
	state := make(map[string]interface{})
	if req.State != nil {
		// Convert state if needed
		state = req.State
	}

	session, err := a.service.CreateSession(ctx, req.AppName, req.UserID, state, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Convert our Session to Google ADK Session
	adkSession := convertSessionToADK(session)

	return &adksession.CreateResponse{
		Session: adkSession,
	}, nil
}

// Get implements session.Service interface (Python: get_session with Event.model_validate_json(event_data["data"])).
//
// Loads session from backend; parses each event JSON into *adksession.Event so the
// returned session holds ADK events (like Python). Events from API may be ADK JSON
// or legacy our Event JSON; we try ADK first, then fall back to our Event conversion.
func (a *SessionServiceAdapter) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	if a.service == nil {
		return nil, fmt.Errorf("session service is nil")
	}

	if a.logger.GetSink() != nil {
		a.logger.V(1).Info("SessionServiceAdapter.Get called", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)
	}

	session, err := a.service.GetSession(ctx, req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}

	if session == nil {
		if a.logger.GetSink() != nil {
			a.logger.Info("Session not found, returning nil")
		}
		return &adksession.GetResponse{
			Session: nil,
		}, nil
	}

	if a.logger.GetSink() != nil {
		a.logger.V(1).Info("Session loaded from backend", "sessionID", session.ID, "eventsBeforeParse", len(session.Events))
		// Debug: log the type of each event before parsing
		for i, e := range session.Events {
			a.logger.V(1).Info("Event type before parseEventsToADK", "eventIndex", i, "type", fmt.Sprintf("%T", e))
		}
	}

	// Parse events into *adksession.Event (Python: Event.model_validate_json(event_data["data"])).
	session.Events = parseEventsToADK(session.Events, a.logger)

	if a.logger.GetSink() != nil {
		a.logger.V(1).Info("Session events after parsing", "sessionID", session.ID, "eventsAfterParse", len(session.Events))
	}

	adkSession := convertSessionToADK(session)
	return &adksession.GetResponse{
		Session: adkSession,
	}, nil
}

// parseEventsToADK converts backend event payloads to *adksession.Event so Get()
// returns a session that yields only ADK events (same as Python: Event.model_validate_json).
// Accepts *adksession.Event (keep), map (from API JSON), or string (JSON string); unmarshals to adksession.Event only.
// Non-ADK shapes are skipped (Python has no "ours" event type).
func parseEventsToADK(events []interface{}, logger logr.Logger) []interface{} {
	out := make([]interface{}, 0, len(events))
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

		// Get JSON bytes from the event (could be map or string)
		var data []byte
		var err error
		if m, ok := e.(map[string]interface{}); ok {
			data, err = json.Marshal(m)
			if err != nil {
				if logger.GetSink() != nil {
					logger.Info("Failed to marshal map event for ADK parse", "error", err, "eventIndex", i)
				}
				skipped++
				continue
			}
		} else if s, ok := e.(string); ok {
			// Event is a JSON string - use it directly
			data = []byte(s)
		} else {
			skipped++
			if logger.GetSink() != nil {
				logger.Info("Event is neither *adksession.Event, map, nor string, skipping", "eventIndex", i, "type", fmt.Sprintf("%T", e))
			}
			continue
		}

		adkE := parseRawToADKEvent(data, logger)
		if adkE != nil {
			out = append(out, adkE)
		} else {
			skipped++
			if logger.GetSink() != nil {
				// Log first N chars of the JSON to help debug
				jsonStr := string(data)
				if len(jsonStr) > core.JSONPreviewMaxLength {
					jsonStr = jsonStr[:core.JSONPreviewMaxLength] + "..."
				}
				logger.Info("Event failed to parse as ADK Event, skipping", "eventIndex", i, "jsonPreview", jsonStr)
			}
		}
	}
	if logger.GetSink() != nil && (len(out) > 0 || skipped > 0) {
		logger.V(1).Info("parseEventsToADK completed", "inputCount", len(events), "outputCount", len(out), "skippedCount", skipped)
	}
	return out
}

// parseRawToADKEvent unmarshals JSON bytes into *adksession.Event (Python: Event.model_validate_json).
func parseRawToADKEvent(data []byte, logger logr.Logger) *adksession.Event {
	e := new(adksession.Event)
	if err := json.Unmarshal(data, e); err != nil {
		if logger.GetSink() != nil {
			logger.Info("Failed to parse event as ADK Event", "error", err, "dataLength", len(data))
		}
		return nil
	}

	// Debug: log what we got after unmarshaling
	if logger.GetSink() != nil {
		logger.Info("Parsed ADK Event fields",
			"author", e.Author,
			"invocationID", e.InvocationID,
			"partial", e.Partial,
			"hasLLMResponseContent", e.LLMResponse.Content != nil,
			"llmResponseFinishReason", e.LLMResponse.FinishReason)
	}

	// Verify the event has meaningful content (not just zero values)
	// Note: adksession.Event embeds model.LLMResponse, so Content is at e.LLMResponse.Content
	hasContent := e.LLMResponse.Content != nil
	hasAuthor := e.Author != ""
	hasInvocationID := e.InvocationID != ""

	// Also accept events that have other meaningful LLMResponse fields
	hasLLMResponseData := e.LLMResponse.FinishReason != "" || e.Partial

	if !hasContent && !hasAuthor && !hasInvocationID && !hasLLMResponseData {
		if logger.GetSink() != nil {
			logger.Info("Parsed ADK Event has no meaningful content, treating as parse failure")
		}
		return nil
	}
	return e
}

// List implements session.Service interface.
// Note: The underlying SessionService does not support listing sessions.
// This returns an empty list with no error for compatibility, but callers
// should be aware that this is a limitation of the current implementation.
func (a *SessionServiceAdapter) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	// Log that List was called but is not fully implemented
	if a.logger.GetSink() != nil {
		a.logger.V(1).Info("List called but not fully implemented - returning empty list", "appName", req.AppName, "userID", req.UserID)
	}
	// Return empty list for compatibility (List is optional for basic functionality)
	return &adksession.ListResponse{
		Sessions: []adksession.Session{},
	}, nil
}

// Delete implements session.Service interface
func (a *SessionServiceAdapter) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	if a.service == nil {
		return fmt.Errorf("session service is nil")
	}

	return a.service.DeleteSession(ctx, req.AppName, req.UserID, req.SessionID)
}

// AppendEvent implements session.Service interface (Python: append_event with event.model_dump_json()).
//
// Like Python: store ADK event in context and persist ADK Event JSON to the API.
// We append event (*adksession.Event) to the wrapper slice and call backend with
// the same ADK event so the API receives {"id": event.id, "data": event_json}.
func (a *SessionServiceAdapter) AppendEvent(ctx context.Context, session adksession.Session, event *adksession.Event) error {
	if a.service == nil {
		return fmt.Errorf("session service is nil")
	}
	if event == nil {
		return nil
	}

	// Update the session in context (like Python: super().append_event(session, event)).
	if wrapper, ok := session.(*SessionWrapper); ok {
		wrapper.session.Events = append(wrapper.session.Events, event)
	}

	// Persist ADK Event JSON to backend (Python: event_data = {"id": event.id, "data": event.model_dump_json()}).
	// Use a detached context so client disconnect (ctx canceled) does not cancel the HTTP POST;
	// otherwise SSE disconnect causes "context canceled" and events are not persisted.
	persistCtx, cancel := context.WithTimeout(context.Background(), core.EventPersistTimeout)
	defer cancel()
	ourSession := convertADKSessionToOurs(session)
	if err := a.service.AppendEvent(persistCtx, ourSession, event); err != nil {
		return err
	}
	return nil
}

// SessionWrapper wraps our Session to implement Google ADK's Session interface
type SessionWrapper struct {
	session *core.Session
	events  *EventsWrapper
	state   *StateWrapper
}

// NewSessionWrapper creates a new wrapper.
// EventsWrapper holds a reference to the session so Events().All() always sees the current
// session.Events (including events appended via AppendEvent); otherwise the ADK would see
// an outdated slice and req.Contents would be empty.
func NewSessionWrapper(session *core.Session) *SessionWrapper {
	return &SessionWrapper{
		session: session,
		events:  NewEventsWrapperForSession(session),
		state:   NewStateWrapper(session.State),
	}
}

// ID implements adksession.Session
func (s *SessionWrapper) ID() string {
	return s.session.ID
}

// AppName implements adksession.Session
func (s *SessionWrapper) AppName() string {
	return s.session.AppName
}

// UserID implements adksession.Session
func (s *SessionWrapper) UserID() string {
	return s.session.UserID
}

// State implements adksession.Session
func (s *SessionWrapper) State() adksession.State {
	return s.state
}

// Events implements adksession.Session
func (s *SessionWrapper) Events() adksession.Events {
	return s.events
}

// LastUpdateTime implements adksession.Session
func (s *SessionWrapper) LastUpdateTime() time.Time {
	// Return current time as we don't track this in our Session
	return time.Now()
}

// EventsWrapper wraps our events to implement adksession.Events.
// It holds a reference to the session so All/Len/At always read the current session.Events;
// after AppendEvent appends to session.Events, the ADK's req.Contents (built from Events().All())
// will include the new events instead of an outdated copy.
type EventsWrapper struct {
	session *core.Session
}

// NewEventsWrapperForSession creates an EventsWrapper that always reads from session.Events.
func NewEventsWrapperForSession(session *core.Session) *EventsWrapper {
	return &EventsWrapper{session: session}
}

// All implements adksession.Events.
// Python-style: session holds only *adksession.Event; yield them directly.
func (e *EventsWrapper) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		events := e.session.Events
		for _, eventInterface := range events {
			if adkE, ok := eventInterface.(*adksession.Event); ok && adkE != nil {
				if !yield(adkE) {
					return
				}
			}
		}
	}
}

// Len implements adksession.Events
func (e *EventsWrapper) Len() int {
	return len(e.session.Events)
}

// At implements adksession.Events.
// Python-style: session holds only *adksession.Event.
func (e *EventsWrapper) At(i int) *adksession.Event {
	events := e.session.Events
	if i < 0 || i >= len(events) {
		return nil
	}
	if adkE, ok := events[i].(*adksession.Event); ok {
		return adkE
	}
	return nil
}

// StateWrapper wraps our state to implement adksession.State
type StateWrapper struct {
	state map[string]interface{}
}

// NewStateWrapper creates a new state wrapper
func NewStateWrapper(state map[string]interface{}) *StateWrapper {
	if state == nil {
		state = make(map[string]interface{})
	}
	return &StateWrapper{state: state}
}

// Get implements adksession.State
func (s *StateWrapper) Get(key string) (interface{}, error) {
	if s.state == nil {
		return nil, adksession.ErrStateKeyNotExist
	}
	value, ok := s.state[key]
	if !ok {
		return nil, adksession.ErrStateKeyNotExist
	}
	return value, nil
}

// Set implements adksession.State
func (s *StateWrapper) Set(key string, value interface{}) error {
	if s.state == nil {
		s.state = make(map[string]interface{})
	}
	s.state[key] = value
	return nil
}

// All implements adksession.State
func (s *StateWrapper) All() iter.Seq2[string, interface{}] {
	return func(yield func(string, interface{}) bool) {
		if s.state == nil {
			return
		}
		for k, v := range s.state {
			if !yield(k, v) {
				return
			}
		}
	}
}

// convertSessionToADK converts our Session to Google ADK Session
func convertSessionToADK(session *core.Session) adksession.Session {
	return NewSessionWrapper(session)
}

// convertADKSessionToOurs converts Google ADK Session to our Session.
// Used only when calling backend (e.g. AppendEvent); backend needs only ID, UserID, AppName, State for the URL.
// Events are not converted (Python-style: we persist ADK events; no "ours" event type for session).
func convertADKSessionToOurs(session adksession.Session) *core.Session {
	state := make(map[string]interface{})
	for k, v := range session.State().All() {
		state[k] = v
	}
	return &core.Session{
		ID:      session.ID(),
		UserID:  session.UserID(),
		AppName: session.AppName(),
		State:   state,
		Events:  nil, // Backend AppendEvent only uses session.ID, UserID, AppName
	}
}

// Python-style: session holds only *adksession.Event; no "ours" type. A2A conversion is in
// converters.convertADKEventToA2AEvents (ADK → A2A directly, like Python convert_event_to_a2a_events).

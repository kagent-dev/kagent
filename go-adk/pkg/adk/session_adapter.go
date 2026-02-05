package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"iter"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	adksession "google.golang.org/adk/session"
)

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

	session, err := a.service.GetSession(ctx, req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}

	if session == nil {
		return &adksession.GetResponse{
			Session: nil,
		}, nil
	}

	// Parse events into *adksession.Event (Python: Event.model_validate_json(event_data["data"])).
	session.Events = parseEventsToADK(session.Events, a.logger)

	adkSession := convertSessionToADK(session)
	return &adksession.GetResponse{
		Session: adkSession,
	}, nil
}

// parseEventsToADK converts backend event payloads to *adksession.Event so Get()
// returns a session that yields only ADK events (same as Python: Event.model_validate_json).
// Accepts *adksession.Event (keep) or map (from API JSON); unmarshals to adksession.Event only.
// Non-ADK shapes are skipped (Python has no "ours" event type).
func parseEventsToADK(events []interface{}, logger logr.Logger) []interface{} {
	out := make([]interface{}, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		if adkE, ok := e.(*adksession.Event); ok {
			out = append(out, adkE)
			continue
		}
		if m, ok := e.(map[string]interface{}); ok {
			data, err := json.Marshal(m)
			if err != nil {
				if logger.GetSink() != nil {
					logger.V(1).Info("Failed to marshal event for ADK parse", "error", err)
				}
				continue
			}
			adkE := parseRawToADKEvent(data, logger)
			if adkE != nil {
				out = append(out, adkE)
			}
		}
	}
	return out
}

// parseRawToADKEvent unmarshals JSON bytes into *adksession.Event (Python: Event.model_validate_json).
func parseRawToADKEvent(data []byte, logger logr.Logger) *adksession.Event {
	var e adksession.Event
	if err := json.Unmarshal(data, &e); err != nil {
		if logger.GetSink() != nil {
			logger.V(1).Info("Failed to parse event as ADK Event", "error", err)
		}
		return nil
	}
	return &e
}

// List implements session.Service interface
func (a *SessionServiceAdapter) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	// Our SessionService doesn't have a List method, so return empty list
	// This is acceptable as List is optional for basic functionality
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
	persistCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

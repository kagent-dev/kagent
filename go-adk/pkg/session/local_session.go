package session

import (
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	adksession "google.golang.org/adk/session"
)

// localSession implements adksession.Session with mutex-guarded state.
type localSession struct {
	appName   string
	userID    string
	sessionID string

	mu        sync.RWMutex
	events    []*adksession.Event
	state     map[string]any
	updatedAt time.Time
}

func (s *localSession) ID() string      { return s.sessionID }
func (s *localSession) AppName() string { return s.appName }
func (s *localSession) UserID() string  { return s.userID }

func (s *localSession) State() adksession.State {
	return &sessionState{mu: &s.mu, state: s.state}
}

func (s *localSession) Events() adksession.Events {
	s.mu.RLock()
	snapshot := make([]*adksession.Event, len(s.events))
	copy(snapshot, s.events)
	s.mu.RUnlock()
	return events(snapshot)
}

func (s *localSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

func (s *localSession) appendEvent(event *adksession.Event) error {
	if event.Partial {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	processed := trimTempDeltaState(event)
	if err := updateSessionState(s, processed); err != nil {
		return fmt.Errorf("failed to update localSession state: %w", err)
	}

	s.events = append(s.events, event)
	s.updatedAt = event.Timestamp
	return nil
}

// events implements adksession.Events.
type events []*adksession.Event

func (e events) All() iter.Seq[*adksession.Event] {
	return func(yield func(*adksession.Event) bool) {
		for _, event := range e {
			if !yield(event) {
				return
			}
		}
	}
}

func (e events) Len() int { return len(e) }

func (e events) At(i int) *adksession.Event {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return nil
}

// sessionState implements adksession.State.
type sessionState struct {
	mu    *sync.RWMutex
	state map[string]any
}

func (s *sessionState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.state[key]
	if !ok {
		return nil, adksession.ErrStateKeyNotExist
	}
	return val, nil
}

func (s *sessionState) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
	return nil
}

func (s *sessionState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		s.mu.RLock()
		snapshot := make(map[string]any, len(s.state))
		for k, v := range s.state {
			snapshot[k] = v
		}
		s.mu.RUnlock()

		for k, v := range snapshot {
			if !yield(k, v) {
				return
			}
		}
	}
}

// trimTempDeltaState removes temporary state delta keys from the event.
func trimTempDeltaState(event *adksession.Event) *adksession.Event {
	if event == nil || len(event.Actions.StateDelta) == 0 {
		return event
	}
	filtered := make(map[string]any)
	for key, value := range event.Actions.StateDelta {
		if !strings.HasPrefix(key, adksession.KeyPrefixTemp) {
			filtered[key] = value
		}
	}
	event.Actions.StateDelta = filtered
	return event
}

// updateSessionState applies event state delta to the session.
func updateSessionState(sess *localSession, event *adksession.Event) error {
	if event == nil || event.Actions.StateDelta == nil {
		return nil
	}
	if sess.state == nil {
		sess.state = make(map[string]any)
	}
	for key, value := range event.Actions.StateDelta {
		if strings.HasPrefix(key, adksession.KeyPrefixTemp) {
			continue
		}
		sess.state[key] = value
	}
	return nil
}

var (
	_ adksession.Session = (*localSession)(nil)
	_ adksession.Events  = (*events)(nil)
	_ adksession.State   = (*sessionState)(nil)
)

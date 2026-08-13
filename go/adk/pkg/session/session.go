package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	eventPersistTimeout = 30 * time.Second
)

// ErrSessionNotFound indicates the requested persisted session does not exist.
var ErrSessionNotFound = errors.New("session not found")

type KAgentSessionService struct {
	client *controllerclient.Client
}

// NewKAgentSessionService creates a new KAgentSessionService.
func NewKAgentSessionService(client *controllerclient.Client) *KAgentSessionService {
	return &KAgentSessionService{client: client}
}

// Create implements adksession.Service.
func (s *KAgentSessionService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Creating session", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)

	state := req.State
	if state == nil {
		state = make(map[string]any)
	}

	request := &apiv1alpha1.CreateSessionRequest{
		AgentRef: req.AppName,
	}
	if req.SessionID != "" {
		request.Id = new(req.SessionID)
	}
	if name, ok := state["session_name"].(string); ok && name != "" {
		request.Name = new(name)
	}
	if source, ok := state["source"].(string); ok && source != "" {
		value, err := sessionSourceToProto(source)
		if err != nil {
			return nil, err
		}
		request.Source = &value
	}

	callContext, cancel := s.client.CallContext(ctx, req.UserID)
	defer cancel()
	response, err := s.client.SessionService().CreateSession(callContext, request)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	result := response.GetSession()
	if result == nil {
		return nil, fmt.Errorf("create session: response session is missing")
	}

	log.V(1).Info("Session created", "sessionID", result.GetId())
	return &adksession.CreateResponse{
		Session: &localSession{
			appName:   req.AppName,
			userID:    result.GetUserId(),
			sessionID: result.GetId(),
			state:     state,
		},
	}, nil
}

// Get implements adksession.Service.
// Fetches the session and its events from the KAgent API, deserialising each
// raw event payload into a typed *adksession.Event — mirroring Python's
// KAgentSessionService.get_session() which calls Event.model_validate_json().
func (s *KAgentSessionService) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Getting session", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)

	request := &apiv1alpha1.GetSessionRequest{
		SessionId: req.SessionID,
		Order:     apiv1alpha1.EventOrder_EVENT_ORDER_ASCENDING,
	}
	if !req.After.IsZero() {
		request.After = timestamppb.New(req.After)
	}
	if req.NumRecentEvents > 0 {
		if req.NumRecentEvents > math.MaxInt32 {
			return nil, fmt.Errorf("get session: recent event limit %d exceeds int32", req.NumRecentEvents)
		}
		request.Limit = new(int32(req.NumRecentEvents))
		request.Order = apiv1alpha1.EventOrder_EVENT_ORDER_DESCENDING
	}

	callContext, cancel := s.client.CallContext(ctx, req.UserID)
	defer cancel()
	response, err := s.client.SessionService().GetSession(callContext, request)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, req.SessionID)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	storedSession := response.GetSession()
	if storedSession == nil {
		return nil, fmt.Errorf("get session: response session is missing")
	}
	events := response.GetEvents()
	if req.NumRecentEvents > 0 {
		slices.Reverse(events)
	}

	log.V(1).Info("Session retrieved", "sessionID", storedSession.GetId(), "eventsCount", len(events))

	// Deserialise each raw event payload into a typed *adksession.Event.
	// Mirrors Python: events.append(Event.model_validate_json(event_data["data"]))
	adkEvents := make([]*adksession.Event, 0, len(events))
	for i, storedEvent := range events {
		eventJSON := unwrapEventJSON(json.RawMessage(storedEvent.GetData()))
		if eventJSON == nil {
			continue
		}
		e := new(adksession.Event)
		if err := json.Unmarshal(eventJSON, e); err != nil {
			log.V(1).Info("Skipping event: unmarshal failed", "eventIndex", i, "error", err)
			continue
		}
		if e.Content == nil && e.Author == "" && e.InvocationID == "" && e.FinishReason == "" && !e.Partial {
			continue
		}
		adkEvents = append(adkEvents, e)
	}

	return &adksession.GetResponse{
		Session: &localSession{
			appName:   req.AppName,
			userID:    storedSession.GetUserId(),
			sessionID: storedSession.GetId(),
			events:    adkEvents,
			state:     make(map[string]any),
		},
	}, nil
}

// List implements adksession.Service.
func (s *KAgentSessionService) List(_ context.Context, _ *adksession.ListRequest) (*adksession.ListResponse, error) {
	return &adksession.ListResponse{Sessions: []adksession.Session{}}, nil
}

// Delete implements adksession.Service.
func (s *KAgentSessionService) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	log := logr.FromContextOrDiscard(ctx)
	callContext, cancel := s.client.CallContext(ctx, req.UserID)
	defer cancel()
	_, err := s.client.SessionService().DeleteSession(callContext, &apiv1alpha1.DeleteSessionRequest{SessionId: req.SessionID})
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	log.V(1).Info("Session deleted", "sessionID", req.SessionID)
	return nil
}

// AppendEvent implements adksession.Service.
// Persists the event to the KAgent backend (mirroring Python's append_event
// which POSTs event.model_dump_json()), then updates the in-memory localSession
// so subsequent reads within the same request see the new event (mirroring
// Python's super().append_event() call).
func (s *KAgentSessionService) AppendEvent(ctx context.Context, adkSess adksession.Session, event *adksession.Event) error {
	if event == nil {
		return nil
	}

	log := logr.FromContextOrDiscard(ctx)

	// Use a detached context so a client disconnect does not cancel the write.
	persistCtx, cancel := context.WithTimeout(context.Background(), eventPersistTimeout)
	defer cancel()

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	eventID := event.ID
	if eventID == "" {
		eventID = uuid.New().String()
	}

	callContext, callCancel := s.client.CallContext(persistCtx, adkSess.UserID())
	defer callCancel()
	_, err = s.client.SessionService().AddSessionEvent(callContext, &apiv1alpha1.AddSessionEventRequest{
		SessionId: adkSess.ID(),
		Id:        eventID,
		Data:      string(eventData),
	})
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	log.V(1).Info("Event appended", "sessionID", adkSess.ID(), "eventID", eventID)

	// Update the in-memory localSession so subsequent reads within this
	// request see the new event. Mirrors Python's super().append_event().
	if ls, ok := adkSess.(*localSession); ok {
		if err := ls.appendEvent(event); err != nil {
			return fmt.Errorf("failed to update in-memory session: %w", err)
		}
	}

	return nil
}

// GetSession is a convenience wrapper used by beforeExecute to fetch a session
// without going through the ADK request/response envelope.
// Returns (nil, nil) when the session does not exist.
func (s *KAgentSessionService) GetSession(ctx context.Context, appName, userID, sessionID string) (adksession.Session, error) {
	resp, err := s.Get(ctx, &adksession.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Session, nil
}

// CreateSession is a convenience wrapper used by beforeExecute.
func (s *KAgentSessionService) CreateSession(ctx context.Context, appName, userID string, state map[string]any, sessionID string) error {
	_, err := s.Create(ctx, &adksession.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		State:     state,
		SessionID: sessionID,
	})
	return err
}

// unwrapEventJSON handles the two wire formats the backend may use:
//   - JSON string (double-encoded): `"{ ... }"` → strips outer quotes
//   - Raw JSON object: `{ ... }` → used as-is
func unwrapEventJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil
		}
		return []byte(s)
	}
	return raw
}

func sessionSourceToProto(source string) (apiv1alpha1.SessionSource, error) {
	switch source {
	case "user":
		return apiv1alpha1.SessionSource_SESSION_SOURCE_USER, nil
	case "agent":
		return apiv1alpha1.SessionSource_SESSION_SOURCE_AGENT, nil
	default:
		return apiv1alpha1.SessionSource_SESSION_SOURCE_UNSPECIFIED, fmt.Errorf("create session: unsupported source %q", source)
	}
}

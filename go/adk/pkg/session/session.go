package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	eventPersistTimeout = 30 * time.Second
)

// ErrSessionNotFound indicates the requested persisted session does not exist.
var ErrSessionNotFound = errors.New("session not found")

type KAgentSessionService struct {
	BaseURL string
	Client  *http.Client
}

// NewKAgentSessionService creates a new KAgentSessionService.
// If client is nil, http.DefaultClient is used.
func NewKAgentSessionService(baseURL string, client *http.Client) *KAgentSessionService {
	if client == nil {
		client = http.DefaultClient
	}
	return &KAgentSessionService{BaseURL: baseURL, Client: client}
}

// Create implements adksession.Service.
func (s *KAgentSessionService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Creating session", "appName", req.AppName, "userID", req.UserID, "sessionID", req.SessionID)

	state := req.State
	if state == nil {
		state = make(map[string]any)
	}

	reqData := map[string]any{
		"user_id":   req.UserID,
		"agent_ref": req.AppName,
	}
	if req.SessionID != "" {
		reqData["id"] = req.SessionID
	}
	if name, ok := state["session_name"].(string); ok && name != "" {
		reqData["name"] = name
	}
	// Propagate session source (e.g. "agent")
	if source, ok := state["source"].(string); ok && source != "" {
		reqData["source"] = source
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create session request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build create session request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-User-ID", req.UserID)

	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute create session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create session: status %d - %s", resp.StatusCode, string(b))
	}

	var result struct {
		Data struct {
			ID     string `json:"id"`
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode create session response: %w", err)
	}

	log.V(1).Info("Session created", "sessionID", result.Data.ID)
	return &adksession.CreateResponse{
		Session: &localSession{
			appName:   req.AppName,
			userID:    result.Data.UserID,
			sessionID: result.Data.ID,
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

	url := fmt.Sprintf("%s/api/sessions/%s?user_id=%s&limit=-1&order=asc", s.BaseURL, url.PathEscape(req.SessionID), url.QueryEscape(req.UserID))
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build get session request: %w", err)
	}
	httpReq.Header.Set("X-User-ID", req.UserID)

	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute get session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, req.SessionID)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get session: status %d, body: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Data struct {
			Session struct {
				ID     string `json:"id"`
				UserID string `json:"user_id"`
			} `json:"session"`
			Events []struct {
				Data json.RawMessage `json:"data"`
			} `json:"events"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode get session response: %w", err)
	}

	log.V(1).Info("Session retrieved", "sessionID", result.Data.Session.ID, "eventsCount", len(result.Data.Events))

	// Deserialise each raw event payload into a typed *adksession.Event.
	// Mirrors Python: events.append(Event.model_validate_json(event_data["data"]))
	adkEvents := make([]*adksession.Event, 0, len(result.Data.Events))
	for i, raw := range result.Data.Events {
		eventJSON := unwrapEventJSON(raw.Data)
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
		normalizeConfirmationEventRole(e)
		adkEvents = append(adkEvents, e)
	}

	return &adksession.GetResponse{
		Session: &localSession{
			appName:   req.AppName,
			userID:    result.Data.Session.UserID,
			sessionID: result.Data.Session.ID,
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
	url := fmt.Sprintf("%s/api/sessions/%s?user_id=%s", s.BaseURL, url.PathEscape(req.SessionID), url.QueryEscape(req.UserID))
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to build delete session request: %w", err)
	}
	httpReq.Header.Set("X-User-ID", req.UserID)

	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute delete session request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete session: status %d, body: %s", resp.StatusCode, string(b))
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

	normalizeConfirmationEventRole(event)

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

	reqData := map[string]any{
		"id":   eventID,
		"data": string(eventData),
	}
	body, err := json.Marshal(reqData)
	if err != nil {
		return fmt.Errorf("failed to marshal append event request: %w", err)
	}

	url := fmt.Sprintf("%s/api/sessions/%s/events?user_id=%s", s.BaseURL, url.PathEscape(adkSess.ID()), url.QueryEscape(adkSess.UserID()))
	httpReq, err := http.NewRequestWithContext(persistCtx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build append event request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-User-ID", adkSess.UserID())

	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute append event request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("append event: status %d, response: %s", resp.StatusCode, string(b))
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

// normalizeConfirmationEventRole fixes the role on adk_request_confirmation
// functionCall events from "model" to "user".
//
// The upstream Go ADK (generateRequestConfirmationEvent in functions.go) sets
// role="model" on these events. Python sets role="user", resulting in inconsistencies
// and breaking some model provider's APIs.
func normalizeConfirmationEventRole(event *adksession.Event) {
	if event == nil || event.Content == nil || len(event.Content.Parts) == 0 {
		return
	}
	if event.Content.Role != string(genai.RoleModel) {
		return
	}
	// Check if ALL parts are adk_request_confirmation functionCalls.
	for _, part := range event.Content.Parts {
		if part.FunctionCall == nil || part.FunctionCall.Name != "adk_request_confirmation" {
			return
		}
	}
	event.Content.Role = string(genai.RoleUser)
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

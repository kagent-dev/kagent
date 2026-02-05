package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"
)

// Session represents an agent session.
type Session struct {
	ID      string                 `json:"id"`
	UserID  string                 `json:"user_id"`
	AppName string                 `json:"app_name"`
	State   map[string]interface{} `json:"state"`
	Events  []interface{}          `json:"events"` // Placeholder for events
}

// SessionService is an interface for session management.
type SessionService interface {
	CreateSession(ctx context.Context, appName, userID string, state map[string]interface{}, sessionID string) (*Session, error)
	GetSession(ctx context.Context, appName, userID, sessionID string) (*Session, error)
	DeleteSession(ctx context.Context, appName, userID, sessionID string) error
	AppendEvent(ctx context.Context, session *Session, event interface{}) error
}

// KAgentSessionService implementation using KAgent API.
type KAgentSessionService struct {
	BaseURL string
	Client  *http.Client
	Logger  logr.Logger
}

// NewKAgentSessionServiceWithLogger creates a new KAgentSessionService with a logger.
// For no-op logging, pass logr.Discard().
func NewKAgentSessionServiceWithLogger(baseURL string, client *http.Client, logger logr.Logger) *KAgentSessionService {
	return &KAgentSessionService{
		BaseURL: baseURL,
		Client:  client,
		Logger:  logger,
	}
}

func (s *KAgentSessionService) CreateSession(ctx context.Context, appName, userID string, state map[string]interface{}, sessionID string) (*Session, error) {
	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Creating session", "appName", appName, "userID", userID, "sessionID", sessionID)
	}

	reqData := map[string]interface{}{
		ArgKeyUserID:              userID,
		SessionRequestKeyAgentRef: appName,
	}
	if sessionID != "" {
		reqData["id"] = sessionID
	}
	if state != nil {
		if name, ok := state[StateKeySessionName].(string); ok {
			reqData["name"] = name
		}
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(HeaderContentType, ContentTypeJSON)
	req.Header.Set(HeaderXUserID, userID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Try to read error response body for better error messages
		var errorBody bytes.Buffer
		if resp.Body != nil {
			_, _ = errorBody.ReadFrom(resp.Body) // best-effort read for error message
		}
		if errorBody.Len() > 0 {
			return nil, fmt.Errorf("failed to create session: status %d - %s", resp.StatusCode, errorBody.String())
		}
		return nil, fmt.Errorf("failed to create session: status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			ID     string `json:"id"`
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Session created successfully", "sessionID", result.Data.ID, "userID", result.Data.UserID)
	}

	return &Session{
		ID:      result.Data.ID,
		UserID:  result.Data.UserID,
		AppName: appName,
		State:   state,
	}, nil
}

func (s *KAgentSessionService) GetSession(ctx context.Context, appName, userID, sessionID string) (*Session, error) {
	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Getting session", "appName", appName, "userID", userID, "sessionID", sessionID)
	}

	url := fmt.Sprintf("%s/api/sessions/%s?user_id=%s&limit=-1", s.BaseURL, sessionID, userID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(HeaderXUserID, userID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if s.Logger.GetSink() != nil {
			s.Logger.Info("Session not found", "sessionID", sessionID, "userID", userID)
		}
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get session: status %d", resp.StatusCode)
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
		return nil, err
	}

	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Session retrieved successfully", "sessionID", result.Data.Session.ID, "userID", result.Data.Session.UserID, "eventsCount", len(result.Data.Events))
	}

	// Parse events from JSON (matching Python: Event.model_validate_json(event_data["data"])).
	// Try ADK Event first so session.Events holds *adksession.Event when API returns ADK JSON.
	events := make([]interface{}, 0, len(result.Data.Events))
	for _, eventData := range result.Data.Events {
		var adkE adksession.Event
		if err := json.Unmarshal(eventData.Data, &adkE); err == nil {
			events = append(events, &adkE)
			continue
		}
		// Fallback: raw map (legacy or non-ADK format); adapter's parseEventsToADK will handle it
		var event interface{}
		if err := json.Unmarshal(eventData.Data, &event); err != nil {
			if s.Logger.GetSink() != nil {
				s.Logger.V(1).Info("Failed to parse event data, skipping", "error", err)
			}
			continue
		}
		events = append(events, event)
	}

	if s.Logger.GetSink() != nil && len(events) > 0 {
		s.Logger.V(1).Info("Parsed session events", "eventsCount", len(events))
	}

	return &Session{
		ID:      result.Data.Session.ID,
		UserID:  result.Data.Session.UserID,
		AppName: appName,
		State:   make(map[string]interface{}),
		Events:  events, // Include parsed events (matching Python: session = Session(..., events=events))
	}, nil
}

func (s *KAgentSessionService) DeleteSession(ctx context.Context, appName, userID, sessionID string) error {
	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Deleting session", "appName", appName, "userID", userID, "sessionID", sessionID)
	}

	url := fmt.Sprintf("%s/api/sessions/%s?user_id=%s", s.BaseURL, sessionID, userID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(HeaderXUserID, userID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete session: status %d", resp.StatusCode)
	}

	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Session deleted successfully", "sessionID", sessionID, "userID", userID)
	}
	return nil
}

func (s *KAgentSessionService) AppendEvent(ctx context.Context, session *Session, event interface{}) error {
	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Appending event to session", "sessionID", session.ID, "userID", session.UserID)
	}

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Extract event ID if available (similar to Python's event.id)
	eventID := extractEventID(event, eventData, s.Logger)

	reqData := map[string]interface{}{
		"id":   eventID,
		"data": string(eventData),
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	url := fmt.Sprintf("%s/api/sessions/%s/events?user_id=%s", s.BaseURL, session.ID, session.UserID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set(HeaderContentType, ContentTypeJSON)
	req.Header.Set(HeaderXUserID, session.UserID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// Read response body for error details
		bodyBytes, _ := io.ReadAll(resp.Body)
		if s.Logger.GetSink() != nil {
			s.Logger.Error(fmt.Errorf("failed to append event"), "Failed to append event to session", "statusCode", resp.StatusCode, "responseBody", string(bodyBytes), "sessionID", session.ID, "eventID", eventID)
		}
		return fmt.Errorf("failed to append event: status %d, response: %s", resp.StatusCode, string(bodyBytes))
	}

	if s.Logger.GetSink() != nil {
		s.Logger.V(1).Info("Event appended to session successfully", "sessionID", session.ID, "eventID", eventID)
	}
	return nil
}

// extractEventID extracts an event ID from various event formats
// It tries multiple methods to find an ID field in the event
func extractEventID(event interface{}, eventData []byte, logger logr.Logger) string {
	// Method 1: Direct map check
	if eventMap, ok := event.(map[string]interface{}); ok {
		if id := getIDFromMap(eventMap); id != "" {
			return id
		}
	}

	// Method 2: Use reflection to check for ID field
	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}
	if eventValue.Kind() == reflect.Struct {
		if id := getIDFromStruct(eventValue); id != "" {
			return id
		}
	}

	// Method 3: Try unmarshaling JSON to map
	if len(eventData) > 0 {
		var eventMap map[string]interface{}
		if err := json.Unmarshal(eventData, &eventMap); err == nil {
			if id := getIDFromMap(eventMap); id != "" {
				return id
			}
		}
	}

	// Method 4: Generate UUID if no ID found
	eventID := uuid.New().String()
	if logger.GetSink() != nil {
		logger.V(1).Info("Generated event ID (no ID found in event)", "generatedEventID", eventID)
	}
	return eventID
}

// getIDFromMap extracts ID from a map using various key names
func getIDFromMap(m map[string]interface{}) string {
	idKeys := []string{"id", "ID", "Id", "message_id", "messageId", "MessageID", "task_id", "taskId", "TaskID"}
	for _, key := range idKeys {
		if val, ok := m[key]; ok {
			if id, ok := val.(string); ok && id != "" {
				return id
			}
		}
	}
	// Check nested message.message_id
	if message, ok := m[ArgKeyMessage].(map[string]interface{}); ok {
		messageIDKeys := []string{"message_id", "messageId", "MessageID"}
		for _, key := range messageIDKeys {
			if id, ok := message[key].(string); ok && id != "" {
				return id
			}
		}
	}
	return ""
}

// getIDFromStruct extracts ID from a struct using reflection
func getIDFromStruct(v reflect.Value) string {
	// Try various ID field names
	idFields := []string{"ID", "Id", "id", "MessageID", "MessageId", "message_id", "TaskID", "TaskId", "task_id"}
	for _, fieldName := range idFields {
		if idField := v.FieldByName(fieldName); idField.IsValid() {
			if id := extractStringFromField(idField); id != "" {
				return id
			}
		}
	}

	// Check nested Message field for MessageID
	if messageField := v.FieldByName("Message"); messageField.IsValid() {
		if messageField.Kind() == reflect.Ptr && !messageField.IsNil() {
			messageValue := messageField.Elem()
			if messageIDField := messageValue.FieldByName("MessageID"); messageIDField.IsValid() {
				if id := extractStringFromField(messageIDField); id != "" {
					return id
				}
			}
		}
	}
	return ""
}

// extractStringFromField extracts a string value from a reflect.Value field
func extractStringFromField(field reflect.Value) string {
	if field.Kind() == reflect.String {
		return field.String()
	}
	if field.Kind() == reflect.Ptr && !field.IsNil() {
		if field.Elem().Kind() == reflect.String {
			return field.Elem().String()
		}
	}
	return ""
}

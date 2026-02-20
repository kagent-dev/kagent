package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
)

// Session represents an agent session.
type Session struct {
	ID      string         `json:"id"`
	UserID  string         `json:"user_id"`
	AppName string         `json:"app_name"`
	State   map[string]any `json:"state"`
	Events  []any          `json:"events"`
}

// SessionService is an interface for session management.
type SessionService interface {
	CreateSession(ctx context.Context, appName, userID string, state map[string]any, sessionID string) (*Session, error)
	GetSession(ctx context.Context, appName, userID, sessionID string) (*Session, error)
	DeleteSession(ctx context.Context, appName, userID, sessionID string) error
	AppendEvent(ctx context.Context, session *Session, event any) error
	AppendFirstSystemEvent(ctx context.Context, session *Session) error
}

// Compile-time interface compliance check
var _ SessionService = (*KAgentSessionService)(nil)

// KAgentSessionService implements SessionService using the KAgent API.
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
	return &KAgentSessionService{
		BaseURL: baseURL,
		Client:  client,
	}
}

func (s *KAgentSessionService) CreateSession(ctx context.Context, appName, userID string, state map[string]any, sessionID string) (*Session, error) {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Creating session", "appName", appName, "userID", userID, "sessionID", sessionID)

	reqData := map[string]any{
		"user_id":   userID,
		"agent_ref": appName,
	}
	if sessionID != "" {
		reqData["id"] = sessionID
	}
	if state != nil {
		if name, ok := state["session_name"].(string); ok {
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			return nil, fmt.Errorf("failed to create session: status %d - %s", resp.StatusCode, string(body))
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

	log.V(1).Info("Session created successfully", "sessionID", result.Data.ID, "userID", result.Data.UserID)

	return &Session{
		ID:      result.Data.ID,
		UserID:  result.Data.UserID,
		AppName: appName,
		State:   state,
	}, nil
}

func (s *KAgentSessionService) GetSession(ctx context.Context, appName, userID, sessionID string) (*Session, error) {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Getting session", "appName", appName, "userID", userID, "sessionID", sessionID)

	url := fmt.Sprintf("%s/api/sessions/%s?user_id=%s&limit=-1", s.BaseURL, sessionID, userID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-User-ID", userID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Info("Session not found", "sessionID", sessionID, "userID", userID)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get session: status %d, body: %s", resp.StatusCode, string(body))
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

	log.V(1).Info("Session retrieved successfully", "sessionID", result.Data.Session.ID, "userID", result.Data.Session.UserID, "eventsCount", len(result.Data.Events))

	events := make([]any, 0, len(result.Data.Events))
	for i, eventData := range result.Data.Events {
		var eventJSON []byte

		rawPreview := string(eventData.Data)
		if len(rawPreview) > 200 {
			rawPreview = rawPreview[:200] + "..."
		}
		log.V(1).Info("Processing event from backend", "eventIndex", i, "rawDataPreview", rawPreview)

		if len(eventData.Data) > 0 && eventData.Data[0] == '"' {
			var jsonStr string
			if err := json.Unmarshal(eventData.Data, &jsonStr); err != nil {
				log.Info("Failed to unmarshal event data string, skipping", "error", err, "eventIndex", i)
				continue
			}
			eventJSON = []byte(jsonStr)
		} else {
			eventJSON = eventData.Data
		}

		var event map[string]any
		if err := json.Unmarshal(eventJSON, &event); err != nil {
			log.Info("Failed to parse event data as map, skipping", "error", err, "eventIndex", i)
			continue
		}
		log.V(1).Info("Parsed event as map", "eventIndex", i, "mapKeys", getMapKeys(event))
		events = append(events, event)
	}

	log.V(1).Info("Parsed session events", "totalEvents", len(result.Data.Events), "outputEvents", len(events))

	return &Session{
		ID:      result.Data.Session.ID,
		UserID:  result.Data.Session.UserID,
		AppName: appName,
		State:   make(map[string]any),
		Events:  events,
	}, nil
}

func (s *KAgentSessionService) DeleteSession(ctx context.Context, appName, userID, sessionID string) error {
	log := logr.FromContextOrDiscard(ctx)
	log.V(1).Info("Deleting session", "appName", appName, "userID", userID, "sessionID", sessionID)

	url := fmt.Sprintf("%s/api/sessions/%s?user_id=%s", s.BaseURL, sessionID, userID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-User-ID", userID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete session: status %d, body: %s", resp.StatusCode, string(body))
	}

	log.V(1).Info("Session deleted successfully", "sessionID", sessionID, "userID", userID)
	return nil
}

func (s *KAgentSessionService) AppendEvent(ctx context.Context, session *Session, event any) error {
	log := logr.FromContextOrDiscard(ctx)

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	eventID := extractEventID(ctx, event, eventData)

	jsonPreview := string(eventData)
	if len(jsonPreview) > 300 {
		jsonPreview = jsonPreview[:300] + "..."
	}
	log.V(1).Info("Appending event to session", "sessionID", session.ID, "userID", session.UserID, "eventID", eventID, "eventType", fmt.Sprintf("%T", event), "jsonPreview", jsonPreview)

	reqData := map[string]any{
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", session.UserID)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Error(fmt.Errorf("failed to append event"), "Failed to append event to session", "statusCode", resp.StatusCode, "responseBody", string(bodyBytes), "sessionID", session.ID, "eventID", eventID)
		return fmt.Errorf("failed to append event: status %d, response: %s", resp.StatusCode, string(bodyBytes))
	}

	log.V(1).Info("Event appended to session successfully", "sessionID", session.ID, "eventID", eventID)
	return nil
}

// AppendFirstSystemEvent appends the initial system event (header_update) before run.
func (s *KAgentSessionService) AppendFirstSystemEvent(ctx context.Context, session *Session) error {
	event := map[string]any{
		"InvocationID": "header_update",
		"Author":       "system",
	}
	return s.AppendEvent(ctx, session, event)
}

func extractEventID(ctx context.Context, event any, eventData []byte) string {
	log := logr.FromContextOrDiscard(ctx)

	if eventMap, ok := event.(map[string]any); ok {
		if id := getIDFromMap(eventMap); id != "" {
			return id
		}
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}
	if eventValue.Kind() == reflect.Struct {
		if id := getIDFromStruct(eventValue); id != "" {
			return id
		}
	}

	if len(eventData) > 0 {
		var eventMap map[string]any
		if err := json.Unmarshal(eventData, &eventMap); err == nil {
			if id := getIDFromMap(eventMap); id != "" {
				return id
			}
		}
	}

	eventID := uuid.New().String()
	log.V(1).Info("Generated event ID (no ID found in event)", "generatedEventID", eventID)
	return eventID
}

func getIDFromMap(m map[string]any) string {
	idKeys := []string{"id", "ID", "Id", "message_id", "messageId", "MessageID", "task_id", "taskId", "TaskID"}
	for _, key := range idKeys {
		if val, ok := m[key]; ok {
			if id, ok := val.(string); ok && id != "" {
				return id
			}
		}
	}
	if message, ok := m["message"].(map[string]any); ok {
		messageIDKeys := []string{"message_id", "messageId", "MessageID"}
		for _, key := range messageIDKeys {
			if id, ok := message[key].(string); ok && id != "" {
				return id
			}
		}
	}
	return ""
}

func getIDFromStruct(v reflect.Value) string {
	idFields := []string{"ID", "Id", "id", "MessageID", "MessageId", "message_id", "TaskID", "TaskId", "task_id"}
	for _, fieldName := range idFields {
		if idField := v.FieldByName(fieldName); idField.IsValid() {
			if id := extractStringFromField(idField); id != "" {
				return id
			}
		}
	}

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

func getMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

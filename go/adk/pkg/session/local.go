package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/gorm"
)

// Service is the session surface kagent wires through the runtime: the upstream ADK session
// service plus the executor's convenience helpers. Implemented by KAgentSessionService (HTTP →
// controller database) and LocalSessionService (sqlite in the actor's durableDir volume).
type Service interface {
	adksession.Service
	GetSession(ctx context.Context, appName, userID, sessionID string) (adksession.Session, error)
	CreateSession(ctx context.Context, appName, userID string, state map[string]any, sessionID string) error
}

var (
	_ Service = (*KAgentSessionService)(nil)
	_ Service = (*LocalSessionService)(nil)
)

// LocalSessionService stores ADK session state in a local sqlite DB. On substrate sandbox
// agents the DB lives inside the actor's durableDir volume (KAGENT_SESSION_DB_URL, injected by
// the controller), so session state survives suspend/resume without round-tripping events to
// the controller. It wraps the upstream ADK database session service, sharing nothing with the
// HTTP KAgentSessionService but the Service interface.
type LocalSessionService struct {
	adksession.Service
}

// NewLocalSessionService opens (creating if needed) the sqlite DB named by dbURL
// (e.g. "sqlite:////data/sessions.db") and migrates the upstream ADK schema.
func NewLocalSessionService(dbURL string) (*LocalSessionService, error) {
	path, err := sqlitePathFromURL(dbURL)
	if err != nil {
		return nil, err
	}
	svc, err := database.NewSessionService(sqlite.Open(path))
	if err != nil {
		return nil, fmt.Errorf("open local session DB %q: %w", path, err)
	}
	if err := database.AutoMigrate(svc); err != nil {
		return nil, fmt.Errorf("migrate local session DB %q: %w", path, err)
	}
	return &LocalSessionService{Service: svc}, nil
}

// sqlitePathFromURL extracts the absolute file path from a sqlite session DB URL. The
// controller injects "sqlite:////data/sessions.db" (scheme + empty authority + absolute path,
// mirroring the python SQLAlchemy convention without a driver segment).
func sqlitePathFromURL(dbURL string) (string, error) {
	rest, ok := strings.CutPrefix(dbURL, "sqlite:")
	if !ok {
		return "", fmt.Errorf("unsupported session DB URL %q: expected sqlite:////<path>", dbURL)
	}
	path := "/" + strings.TrimLeft(rest, "/")
	if path == "/" {
		return "", fmt.Errorf("session DB URL %q has no path", dbURL)
	}
	return path, nil
}

// GetSession implements the executor's convenience lookup, mapping a missing session to
// ErrSessionNotFound like the HTTP service does.
func (s *LocalSessionService) GetSession(ctx context.Context, appName, userID, sessionID string) (adksession.Session, error) {
	resp, err := s.Get(ctx, &adksession.GetRequest{AppName: appName, UserID: userID, SessionID: sessionID})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if resp == nil || resp.Session == nil {
		return nil, ErrSessionNotFound
	}
	return resp.Session, nil
}

// CreateSession implements the executor's convenience create.
func (s *LocalSessionService) CreateSession(ctx context.Context, appName, userID string, state map[string]any, sessionID string) error {
	_, err := s.Create(ctx, &adksession.CreateRequest{AppName: appName, UserID: userID, State: state, SessionID: sessionID})
	return err
}

// localEventRow is the controller event wire shape ({id, data, created_at}), the same rows the
// HTTP session service would have written to the controller database. The controller's
// GET /api/sessions/{id}/events?source=sandbox splices these into its standard envelope.
type localEventRow struct {
	ID        string `json:"id"`
	Data      string `json:"data"`
	CreatedAt string `json:"created_at"`
}

// EventsHandler serves GET /local/sessions/{id}/events from the local store in the controller
// event wire shape, ascending. A session with no local rows yet returns an empty list — the
// route existing at all is what tells the controller the runtime supports durable-dir sessions
// (a 404 means it does not).
func (s *LocalSessionService) EventsHandler(appName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		userID := r.URL.Query().Get("user_id")

		rows := []localEventRow{}
		sess, err := s.GetSession(r.Context(), appName, userID, sessionID)
		if err != nil && err != ErrSessionNotFound {
			http.Error(w, fmt.Sprintf("load session %q: %v", sessionID, err), http.StatusInternalServerError)
			return
		}
		if sess != nil {
			for event := range sess.Events().All() {
				data, err := json.Marshal(event)
				if err != nil {
					http.Error(w, fmt.Sprintf("marshal event: %v", err), http.StatusInternalServerError)
					return
				}
				id := event.ID
				if id == "" {
					id = uuid.New().String()
				}
				rows = append(rows, localEventRow{
					ID:        id,
					Data:      string(data),
					CreatedAt: event.Timestamp.UTC().Format("2006-01-02T15:04:05.999999Z07:00"),
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	})
}

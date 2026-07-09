package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/glebarez/sqlite"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/gorm"
)

// Service is the session surface kagent wires through the runtime:
// Implemented by KAgentSessionService (HTTP → postgre database)
// and LocalSessionService (sqlite in the actor's durableDir volume).
type Service interface {
	adksession.Service
	GetSession(ctx context.Context, appName, userID, sessionID string) (adksession.Session, error)
	CreateSession(ctx context.Context, appName, userID string, state map[string]any, sessionID string) error
}

var (
	_ Service = (*KAgentSessionService)(nil)
	_ Service = (*LocalSessionService)(nil)
)

// LocalSessionService is used by substrate sandbox agents to store ADK session state in a local sqlite DB.
// The DB lives inside the actor's durableDir volume (AgentConfig.session_db_url, set by the controller
// in the rendered config Secret).
type LocalSessionService struct {
	adksession.Service
}

// NewService builds the session service a runtime should use: dbURL (AgentConfig.session_db_url,
// set by the controller for durable-dir sandbox agents) selects the actor-local sqlite store;
// otherwise kagentURL selects the controller HTTP session service; otherwise nil (caller decides;
// typically in-memory sessions). BYO agents building their own executor should use this to
// populate KAgentExecutorConfig.SessionService so they honor the same contract as the
// declarative runtime.
func NewService(dbURL, kagentURL string, httpClient *http.Client) (Service, error) {
	if dbURL != "" {
		return NewLocalSessionService(dbURL)
	}
	if kagentURL != "" {
		return NewKAgentSessionService(kagentURL, httpClient), nil
	}
	return nil, nil
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
// controller sets "sqlite:////data/sessions.db" for the Go runtime; python's SQLAlchemy form
// with a driver segment ("sqlite+aiosqlite:////data/sessions.db") is accepted too so a BYO
// image built with this SDK works regardless of which dialect it was handed.
func sqlitePathFromURL(dbURL string) (string, error) {
	scheme, rest, ok := strings.Cut(dbURL, ":")
	if !ok || (scheme != "sqlite" && !strings.HasPrefix(scheme, "sqlite+")) {
		return "", fmt.Errorf("unsupported session DB URL %q: expected sqlite[+driver]:////<path>", dbURL)
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

package session

import (
	"context"
	"errors"
	"fmt"
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
// The DB lives inside the actor's durableDir volume (KAGENT_SESSION_DB_URL, injected by the controller)
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

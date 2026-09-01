package session

import (
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
)

// LocalSessionService is used by substrate sandbox agents to store ADK session state in a local sqlite DB.
// The DB lives inside the actor's durableDir volume (AgentConfig.session_db_url, set by the controller
// in the rendered config Secret).
type LocalSessionService struct {
	adksession.Service
}

// NewService builds the actor-local session service selected by AgentConfig.session_db_url.
// A missing URL leaves session selection to the caller, which typically uses in-memory state.
func NewService(dbURL string) (adksession.Service, error) {
	if dbURL != "" {
		return NewLocalSessionService(dbURL)
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

package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
)

// LocalSessionService stores one Actor's native ADK conversation in SQLite on its
// durableDir volume (AgentConfig.session_db_url). The private database provides
// isolation, so different Actors can safely use the same local conversation key.
// This service must not be used to multiplex independent conversations in one DB.
//
// The ADK A2A executor selects state by app name, caller user ID, and context ID.
// Here, a fork gets a new public context ID but copies the native database, and a
// shared Session can be invoked by a different user. Neither change should select
// an empty native conversation. We therefore keep the configured app name and
// normalize both user and session lookup keys to the fixed local key below.
// Forks continue the copied history and write only to their own database.
//
// Shared in-process subagents participate in this same native conversation.
// A child with an independent conversation needs its own store or distinct keys;
// remote A2A child context/task IDs are separate and are not rewritten here.
//
// Returned ADK sessions expose these private keys. Routing, lineage, and external
// correlation must use the public A2A context ID carried by the executor instead.
// Authorization and user memory must use the actual caller, never the local user
// key. Normalizing storage keys does not grant access to a public Session.
type LocalSessionService struct {
	// AppendEvent already receives a native Session from Create/Get, so it can
	// use the upstream implementation without another identity rewrite.
	adksession.Service
}

// localConversationID is a storage key scoped to one Actor's private database,
// not a globally unique Session ID or an authenticated user identity.
const localConversationID = "conversation"

func (s *LocalSessionService) Create(ctx context.Context, request *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	local := *request
	local.UserID, local.SessionID = localConversationID, localConversationID
	return s.Service.Create(ctx, &local)
}

func (s *LocalSessionService) Get(ctx context.Context, request *adksession.GetRequest) (*adksession.GetResponse, error) {
	local := *request
	local.UserID, local.SessionID = localConversationID, localConversationID
	return s.Service.Get(ctx, &local)
}

func (s *LocalSessionService) List(ctx context.Context, request *adksession.ListRequest) (*adksession.ListResponse, error) {
	local := *request
	local.UserID = localConversationID
	return s.Service.List(ctx, &local)
}

func (s *LocalSessionService) Delete(ctx context.Context, request *adksession.DeleteRequest) error {
	local := *request
	local.UserID, local.SessionID = localConversationID, localConversationID
	return s.Service.Delete(ctx, &local)
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

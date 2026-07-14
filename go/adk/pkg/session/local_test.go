package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestSqlitePathFromURL(t *testing.T) {
	t.Parallel()
	got, err := sqlitePathFromURL("sqlite:////data/sessions.db")
	require.NoError(t, err)
	require.Equal(t, "/data/sessions.db", got)

	got, err = sqlitePathFromURL("sqlite:///data/sessions.db")
	require.NoError(t, err)
	require.Equal(t, "/data/sessions.db", got)

	// The python SQLAlchemy dialect must work too: a BYO image built with this SDK may be
	// handed the python-form URL.
	got, err = sqlitePathFromURL("sqlite+aiosqlite:////data/sessions.db")
	require.NoError(t, err)
	require.Equal(t, "/data/sessions.db", got)

	_, err = sqlitePathFromURL("postgres://x")
	require.Error(t, err)
	_, err = sqlitePathFromURL("sqlite://")
	require.Error(t, err)
}

// TestNewService covers the runtime session-service selection shared by the declarative binary
// and BYO agents: AgentConfig.session_db_url (local sqlite store) > KAGENT_URL (controller HTTP
// sessions) > nil (in-memory fallback).
func TestNewService(t *testing.T) {
	t.Parallel()

	svc, err := NewService("sqlite:///"+filepath.Join(t.TempDir(), "sessions.db"), "http://kagent:8083", nil)
	require.NoError(t, err)
	require.IsType(t, &LocalSessionService{}, svc, "session_db_url must win over kagentURL")

	svc, err = NewService("", "http://kagent:8083", nil)
	require.NoError(t, err)
	require.IsType(t, &KAgentSessionService{}, svc)

	svc, err = NewService("", "", nil)
	require.NoError(t, err)
	require.Nil(t, svc)

	_, err = NewService("postgres://nope", "", nil)
	require.Error(t, err, "an invalid session DB URL must fail loud, not fall back")
}

var eventClock = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

// textEvent builds an event with a strictly increasing timestamp
func textEvent(id, author, text string) *adksession.Event {
	eventClock = eventClock.Add(time.Second)
	return &adksession.Event{
		ID:        id,
		Timestamp: eventClock,
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: author, Parts: []*genai.Part{genai.NewPartFromText(text)}},
		},
	}
}

// TestLocalSessionServiceRoundTrip covers the durable-dir store: create a session, append
// events across service restarts (simulating actor suspend/resume: a fresh process opens the
// same sqlite file), and read everything back.
func TestLocalSessionServiceRoundTrip(t *testing.T) {
	t.Parallel()
	dbURL := "sqlite:///" + filepath.Join(t.TempDir(), "sessions.db")
	ctx := context.Background()

	get := func(svc *LocalSessionService, sessionID string) (adksession.Session, error) {
		resp, err := svc.Get(ctx, &adksession.GetRequest{AppName: "app", UserID: "u1", SessionID: sessionID})
		if err != nil {
			return nil, err
		}
		return resp.Session, nil
	}

	svc, err := NewLocalSessionService(dbURL)
	require.NoError(t, err)
	_, err = svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u1", SessionID: "s1"})
	require.NoError(t, err)
	sess, err := get(svc, "s1")
	require.NoError(t, err)
	require.NoError(t, svc.AppendEvent(ctx, sess, textEvent("e1", "user", "my favorite color is teal")))

	// "Resume": a brand-new service instance over the same file must see the prior event and
	// accept more appends — this is the property suspend/resume durability rests on.
	svc2, err := NewLocalSessionService(dbURL)
	require.NoError(t, err)
	sess2, err := get(svc2, "s1")
	require.NoError(t, err)
	require.Equal(t, 1, sess2.Events().Len())
	require.NoError(t, svc2.AppendEvent(ctx, sess2, textEvent("e2", "model", "noted: teal")))

	sess3, err := get(svc2, "s1")
	require.NoError(t, err)
	require.Equal(t, 2, sess3.Events().Len())

	// Unknown sessions fail the lookup.
	_, err = get(svc2, "missing")
	require.Error(t, err)
}

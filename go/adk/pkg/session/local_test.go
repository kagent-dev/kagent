package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
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

	_, err = sqlitePathFromURL("postgres://x")
	require.Error(t, err)
	_, err = sqlitePathFromURL("sqlite://")
	require.Error(t, err)
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

	svc, err := NewLocalSessionService(dbURL)
	require.NoError(t, err)
	require.NoError(t, svc.CreateSession(ctx, "app", "u1", nil, "s1"))
	sess, err := svc.GetSession(ctx, "app", "u1", "s1")
	require.NoError(t, err)
	require.NoError(t, svc.AppendEvent(ctx, sess, textEvent("e1", "user", "my favorite color is teal")))

	// "Resume": a brand-new service instance over the same file must see the prior event and
	// accept more appends — this is the property suspend/resume durability rests on.
	svc2, err := NewLocalSessionService(dbURL)
	require.NoError(t, err)
	sess2, err := svc2.GetSession(ctx, "app", "u1", "s1")
	require.NoError(t, err)
	require.Equal(t, 1, sess2.Events().Len())
	require.NoError(t, svc2.AppendEvent(ctx, sess2, textEvent("e2", "model", "noted: teal")))

	sess3, err := svc2.GetSession(ctx, "app", "u1", "s1")
	require.NoError(t, err)
	require.Equal(t, 2, sess3.Events().Len())

	// Unknown sessions map to ErrSessionNotFound like the HTTP service.
	_, err = svc2.GetSession(ctx, "app", "u1", "missing")
	require.ErrorIs(t, err, ErrSessionNotFound)
}

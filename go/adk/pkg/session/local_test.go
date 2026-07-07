package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// textEvent builds an event with a strictly increasing timestamp (the store orders events
// chronologically; real runner events always carry timestamps).
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

// TestLocalEventsHandler covers the controller read-through contract: rows in the controller
// event wire shape ({id, data, created_at}, ascending), an empty list for unknown sessions
// (the feature is on; 404 is reserved for "runtime does not support durable-dir sessions").
func TestLocalEventsHandler(t *testing.T) {
	t.Parallel()
	dbURL := "sqlite:///" + filepath.Join(t.TempDir(), "sessions.db")
	ctx := context.Background()

	svc, err := NewLocalSessionService(dbURL)
	require.NoError(t, err)
	require.NoError(t, svc.CreateSession(ctx, "app", "u1", nil, "s1"))
	sess, err := svc.GetSession(ctx, "app", "u1", "s1")
	require.NoError(t, err)
	require.NoError(t, svc.AppendEvent(ctx, sess, textEvent("e1", "user", "hello")))
	require.NoError(t, svc.AppendEvent(ctx, sess, textEvent("e2", "model", "hi there")))

	mux := http.NewServeMux()
	mux.Handle("GET /local/sessions/{id}/events", svc.EventsHandler("app"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/local/sessions/s1/events?user_id=u1")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rows []localEventRow
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	require.Len(t, rows, 2)
	require.Equal(t, "e1", rows[0].ID)
	require.Equal(t, "e2", rows[1].ID)
	for _, row := range rows {
		require.NotEmpty(t, row.CreatedAt)
		// The data blob must round-trip as an ADK event, exactly like rows the HTTP session
		// service writes to the controller database.
		var ev adksession.Event
		require.NoError(t, json.Unmarshal([]byte(row.Data), &ev))
		require.NotNil(t, ev.Content)
	}

	// Unknown session: empty list, not an error.
	resp2, err := http.Get(srv.URL + "/local/sessions/never/events?user_id=u1")
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	var empty []localEventRow
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&empty))
	require.Empty(t, empty)
}

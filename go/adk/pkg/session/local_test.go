package session

import (
	"context"
	"os"
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

// TestNewService covers actor-local session-service selection.
func TestNewService(t *testing.T) {
	t.Parallel()
	svc, err := NewService("sqlite:///" + filepath.Join(t.TempDir(), "sessions.db"))
	require.NoError(t, err)
	require.IsType(t, &LocalSessionService{}, svc)

	svc, err = NewService("")
	require.NoError(t, err)
	require.Nil(t, svc)

	_, err = NewService("postgres://nope")
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

	// A fork has a new public context but continues the copied native session.
	fork, err := get(svc2, "fork-context")
	require.NoError(t, err)
	require.Equal(t, 2, fork.Events().Len())
}

func TestLocalSessionServiceRestoredDatabase(t *testing.T) {
	t.Parallel()
	for _, populated := range []bool{false, true} {
		name := "golden"
		if populated {
			name = "existing conversation"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			svc, err := NewLocalSessionService("sqlite:///" + path)
			require.NoError(t, err)
			ctx := t.Context()
			create := &adksession.CreateRequest{AppName: "app", UserID: "source-user", SessionID: "source"}
			get := &adksession.GetRequest{AppName: "app", UserID: "fork-user", SessionID: "fork"}
			if populated {
				created, err := svc.Create(ctx, create)
				require.NoError(t, err)
				require.NoError(t, svc.AppendEvent(ctx, created.Session, &adksession.Event{
					ID: "before", Author: "user", Timestamp: time.Now(),
					LLMResponse: model.LLMResponse{Content: genai.NewContentFromText("remember teal", genai.RoleUser)},
					Actions:     adksession.EventActions{StateDelta: map[string]any{"color": "teal"}},
				}))
			}

			// A VM restore retains the service in memory but gives /data new backing
			// files. Preserve the old inode, then install the snapshot at the same path.
			snapshot, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.Rename(path, path+".before-restore"))
			require.NoError(t, os.WriteFile(path, snapshot, 0o600))

			if !populated {
				_, err := svc.Create(ctx, create)
				require.NoError(t, err, "first message after golden restore must be able to create its session")
			}
			got, err := svc.Get(ctx, get)
			require.NoError(t, err)
			if populated {
				require.Equal(t, 1, got.Session.Events().Len())
				require.Equal(t, "remember teal", got.Session.Events().At(0).Content.Parts[0].Text)
				color, err := got.Session.State().Get("color")
				require.NoError(t, err)
				require.Equal(t, "teal", color)
			}
			require.NoError(t, svc.AppendEvent(ctx, got.Session, &adksession.Event{
				ID: "after", Author: "model", Timestamp: time.Now(),
				LLMResponse: model.LLMResponse{Content: genai.NewContentFromText("continued", genai.RoleModel)},
			}))
			got, err = svc.Get(ctx, get)
			require.NoError(t, err)
			wantEvents := 1
			if populated {
				wantEvents++
			}
			require.Equal(t, wantEvents, got.Session.Events().Len())
			require.Equal(t, "continued", got.Session.Events().At(wantEvents - 1).Content.Parts[0].Text)
			listed, err := svc.List(ctx, &adksession.ListRequest{AppName: "app", UserID: "fork-user"})
			require.NoError(t, err)
			require.Len(t, listed.Sessions, 1)
			require.NoError(t, svc.Delete(ctx, &adksession.DeleteRequest{AppName: "app", UserID: "fork-user", SessionID: "fork"}))
			listed, err = svc.List(ctx, &adksession.ListRequest{AppName: "app", UserID: "fork-user"})
			require.NoError(t, err)
			require.Empty(t, listed.Sessions)
		})
	}
}

package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestEmbeddedPushConfig(t *testing.T) {
	agent := types.NamespacedName{Namespace: "team", Name: "agent"}
	for _, test := range []struct {
		name   string
		change func(*a2a.SendMessageRequest)
		valid  bool
	}{
		{"http", func(*a2a.SendMessageRequest) {}, true},
		{"https tenant", func(r *a2a.SendMessageRequest) {
			r.Config.PushConfig.URL = "https://receiver/cb"
			r.Config.PushConfig.Tenant = "team/agent"
		}, true},
		{"context continuation", func(r *a2a.SendMessageRequest) { r.Message.ContextID = "context" }, true},
		{"task continuation", func(r *a2a.SendMessageRequest) { r.Message.TaskID = "task" }, true},
		{"token", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Token = "secret" }, true},
		{"missing message", func(r *a2a.SendMessageRequest) { r.Message = nil }, false},
		{"missing message id", func(r *a2a.SendMessageRequest) { r.Message.ID = "" }, false},
		{"embedded task", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.TaskID = "task" }, false},
		{"missing auth", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Auth = nil }, true},
		{"unsupported auth", func(r *a2a.SendMessageRequest) {
			r.Config.PushConfig.Auth = &a2a.PushAuthInfo{Scheme: "Basic", Credentials: "secret"}
		}, false},
		{"empty credential", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Auth.Credentials = "" }, false},
		{"wrong tenant", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Tenant = "other/agent" }, false},
		{"relative URL", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "/callback" }, false},
		{"unsupported scheme", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "ftp://receiver" }, false},
		{"missing host", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "https:///callback" }, false},
		{"credentials in URL", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "https://user:pass@receiver" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "input"}, Config: &a2a.SendMessageConfig{PushConfig: &a2a.PushConfig{URL: "http://receiver", Auth: &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: "secret"}}}}
			test.change(req)
			config, err := initialPushConfig(agent, req)
			if !test.valid {
				require.ErrorIs(t, err, a2a.ErrInvalidParams)
				return
			}
			require.NoError(t, err)
			require.NotSame(t, req.Config.PushConfig, config)
			require.Equal(t, req.Config.PushConfig.Token, config.Token)
		})
	}
}

func TestPushRequiresHTTPSWhenHTTPDisabled(t *testing.T) {
	t.Setenv("KAGENT_A2A_PUSH_ALLOW_HTTP", "false")
	agent := types.NamespacedName{Namespace: "team", Name: "agent"}
	config := &a2a.PushConfig{URL: "http://receiver/callback", Auth: &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: "secret"}}
	_, err := validatePushConfig(agent, config)
	require.ErrorIs(t, err, a2a.ErrInvalidParams)
	config.URL = "https://receiver/callback"
	config.Auth = nil
	_, err = validatePushConfig(agent, config)
	require.NoError(t, err)
}

func TestPushPageTokenIsTaskScoped(t *testing.T) {
	token := encodePushPageToken("task-a", "config-1")
	configID, err := decodePushPageToken("task-a", token)
	require.NoError(t, err)
	require.Equal(t, "config-1", configID)
	_, err = decodePushPageToken("task-b", token)
	require.Error(t, err)
	_, err = decodePushPageToken("task-a", "invalid")
	require.Error(t, err)
}

type pushTestStore struct {
	rows       []database.PushRegistration
	task       *a2a.Task
	lookupErr  error
	deliveries []database.PushDelivery
	finished   []bool
	closed     int
	bound      int
	claimErr   error
	finishErr  error
}

var _ pushStore = (*pushTestStore)(nil)

func (p *pushTestStore) ListOpenPushRegistrations(_ context.Context, after *database.PushRegistration, limit int) ([]database.PushRegistration, error) {
	start := 0
	if after != nil {
		for start < len(p.rows) && p.rows[start].ID <= after.ID {
			start++
		}
	}
	return p.rows[start:min(start+limit, len(p.rows))], nil
}
func (p *pushTestStore) GetSessionTaskByMessage(context.Context, string, string, string) (*a2a.Task, error) {
	return p.task, p.lookupErr
}
func (p *pushTestStore) BindSessionPush(context.Context, database.PushRegistration, string) (bool, error) {
	p.bound++
	return true, nil
}
func (p *pushTestStore) CloseUnboundSessionPush(context.Context, database.PushRegistration) error {
	p.closed++
	return nil
}
func (p *pushTestStore) ClaimDuePushDelivery(context.Context) (*database.PushDelivery, error) {
	if p.claimErr != nil {
		return nil, p.claimErr
	}
	if len(p.deliveries) == 0 {
		return nil, nil
	}
	delivery := p.deliveries[0]
	p.deliveries = p.deliveries[1:]
	return &delivery, nil
}
func (p *pushTestStore) FinishPushDelivery(_ context.Context, _ database.PushDelivery, delivered bool) error {
	p.finished = append(p.finished, delivered)
	return p.finishErr
}

type pushTestSender struct {
	calls int
	err   error
}

var _ pushSender = (*pushTestSender)(nil)

func (p *pushTestSender) SendPush(context.Context, *a2a.PushConfig, a2a.Event) error {
	p.calls++
	return p.err
}

func TestPushWorkerPendingAssociation(t *testing.T) {
	for _, test := range []struct {
		name          string
		age           time.Duration
		lookupErr     error
		closed, bound int
	}{
		{name: "pending", age: time.Minute, lookupErr: database.ErrNotFound},
		{name: "expired", age: 11 * time.Minute, lookupErr: database.ErrNotFound, closed: 1},
		{name: "ambiguous", lookupErr: database.ErrConflict, closed: 1},
		{name: "database failure must not expire", age: time.Hour, lookupErr: errors.New("database unavailable")},
		{name: "association before expiry", age: time.Hour, bound: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &pushTestStore{lookupErr: test.lookupErr, task: &a2a.Task{ID: "task"}}
			err := NewPushWorker(store, &pushTestSender{}).resolvePending(t.Context(), database.PushRegistration{CreatedAt: time.Now().Add(-test.age)})
			require.Equal(t, test.lookupErr != nil && !errors.Is(test.lookupErr, database.ErrNotFound) && !errors.Is(test.lookupErr, database.ErrConflict), err != nil)
			require.Equal(t, test.closed, store.closed)
			require.Equal(t, test.bound, store.bound)
		})
	}
}

func TestPushWorkerDurableHTTPDelivery(t *testing.T) {
	store, session := lifecycleFixture(t)
	session, err := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}).Create(t.Context(), session)
	require.NoError(t, err)
	events := make(chan json.RawMessage, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "secret", r.Header.Get("A2A-Notification-Token"))
		require.Equal(t, "Bearer receiver-credential", r.Header.Get("Authorization"))
		var event json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&event))
		events <- event
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer receiver.Close()
	config := &a2a.PushConfig{ID: "callback", URL: receiver.URL, Token: "secret", Auth: &a2a.PushAuthInfo{Scheme: "Bearer", Credentials: "receiver-credential"}}
	require.NoError(t, store.RegisterSessionPush(t.Context(), session.Id, "input", "", config))
	task := &a2a.Task{ID: "task", ContextID: session.ContextId, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}, History: []*a2a.Message{{ID: "input", Role: a2a.MessageRoleUser}}}
	digest := sha256.Sum256([]byte("create"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
	require.NoError(t, err)
	sender := push.NewHTTPPushSender(&push.HTTPSenderConfig{Timeout: time.Second, AllowPrivateNetworks: true, FailOnError: true})
	require.NoError(t, NewPushWorker(store, sender).poll(t.Context()))
	require.Empty(t, events)
	task.Status.State = a2a.TaskStateCompleted
	digest = sha256.Sum256([]byte("complete"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, NewPushWorker(store, sender).poll(t.Context()))
	require.Empty(t, events, "staged completion must not be notified")
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	require.NoError(t, NewPushWorker(store, sender).poll(t.Context()))
	require.Len(t, events, 1)
	var envelope struct {
		StatusUpdate *a2a.TaskStatusUpdateEvent `json:"statusUpdate"`
	}
	require.NoError(t, json.Unmarshal(<-events, &envelope))
	require.NotNil(t, envelope.StatusUpdate)
	require.Equal(t, a2a.TaskStateCompleted, envelope.StatusUpdate.Status.State)
	require.Empty(t, envelope.StatusUpdate.Metadata)
	require.NoError(t, NewPushWorker(store, sender).poll(t.Context()))
	require.Empty(t, events, "failed attempt must wait for retry delay")
}

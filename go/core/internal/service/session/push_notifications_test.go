package session

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"
)

func TestEmbeddedPushConfig(t *testing.T) {
	t.Setenv("KAGENT_A2A_PUSH_ALLOW_HTTP", "true")
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
		{"token", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Token = "secret" }, false},
		{"newline in token", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Token = "first\nsecond" }, false},
		{"null in token", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Token = "first\x00second" }, false},
		{"missing message", func(r *a2a.SendMessageRequest) { r.Message = nil }, false},
		{"missing message id", func(r *a2a.SendMessageRequest) { r.Message.ID = "" }, false},
		{"embedded task", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.TaskID = "task" }, false},
		{"missing auth", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Auth = nil }, true},
		{"unsupported auth", func(r *a2a.SendMessageRequest) {
			r.Config.PushConfig.Auth = &a2a.PushAuthInfo{Scheme: "Basic", Credentials: "secret"}
		}, false},
		{"client credential", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Auth.Credentials = "secret" }, false},
		{"empty credential", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Auth.Credentials = "" }, true},
		{"wrong tenant", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.Tenant = "other/agent" }, false},
		{"relative URL", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "/callback" }, false},
		{"unsupported scheme", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "ftp://receiver" }, false},
		{"missing host", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "https:///callback" }, false},
		{"credentials in URL", func(r *a2a.SendMessageRequest) { r.Config.PushConfig.URL = "https://user:pass@receiver" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := &a2a.SendMessageRequest{Message: &a2a.Message{ID: "input"}, Config: &a2a.SendMessageConfig{PushConfig: &a2a.PushConfig{URL: "http://receiver", Auth: &a2a.PushAuthInfo{Scheme: "Bearer"}}}}
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
	config := &a2a.PushConfig{URL: "http://receiver/callback"}
	_, err := validatePushConfig(agent, config)
	require.ErrorIs(t, err, a2a.ErrInvalidParams)
	config.URL = "https://receiver/callback"
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
	deliveries []database.PushDelivery
	finished   []bool
	claimErr   error
	finishErr  error
	expired    int
}

var _ pushStore = (*pushTestStore)(nil)

func (p *pushTestStore) ExpireUnboundPushRegistrations(context.Context) error {
	p.expired++
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

func testPushSigner(t *testing.T) *PushJWTSigner {
	t.Helper()
	signer, err := NewPushJWTSigner(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32))), "https://kagent.example")
	require.NoError(t, err)
	return signer
}

func TestPushWorkerFullBatchDoesNotWaitForIdlePoll(t *testing.T) {
	event := &a2a.TaskStatusUpdateEvent{TaskID: "task", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}
	wire, err := pbconv.ToProtoStreamResponse(event)
	require.NoError(t, err)
	payload, err := proto.Marshal(wire)
	require.NoError(t, err)

	store := &pushTestStore{deliveries: make([]database.PushDelivery, 100)}
	for i := range store.deliveries {
		store.deliveries[i].Payload = payload
	}
	sender := &pushTestSender{}
	worker := NewPushWorker(store, sender, testPushSigner(t))
	full, err := worker.poll(t.Context())
	require.NoError(t, err)
	require.True(t, full)
	require.Equal(t, 100, sender.calls)

	full, err = worker.poll(t.Context())
	require.NoError(t, err)
	require.False(t, full)
}

func TestPushWorkerDurableHTTPDelivery(t *testing.T) {
	store, session := lifecycleFixture(t)
	session, err := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}).Create(t.Context(), session)
	require.NoError(t, err)
	events := make(chan json.RawMessage, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Empty(t, r.Header.Get("A2A-Notification-Token"))
		require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
		var event json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&event))
		events <- event
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer receiver.Close()
	config := &a2a.PushConfig{ID: "callback", URL: receiver.URL}
	require.NoError(t, store.RegisterSessionPushNotification(t.Context(), session.Id, "input", "", config))
	task := &a2a.Task{ID: "task", ContextID: session.ContextId, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}, History: []*a2a.Message{{ID: "input", Role: a2a.MessageRoleUser}}}
	digest := sha256.Sum256([]byte("create"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
	require.NoError(t, err)
	sender := NewHTTPPushSender(time.Second, true, true)
	_, err = NewPushWorker(store, sender, testPushSigner(t)).poll(t.Context())
	require.NoError(t, err)
	require.Empty(t, events)
	task.Status.State = a2a.TaskStateCompleted
	digest = sha256.Sum256([]byte("complete"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
	require.NoError(t, err)
	_, err = NewPushWorker(store, sender, testPushSigner(t)).poll(t.Context())
	require.NoError(t, err)
	require.Empty(t, events, "staged completion must not be notified")
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	_, err = NewPushWorker(store, sender, testPushSigner(t)).poll(t.Context())
	require.NoError(t, err)
	require.Len(t, events, 1)
	var envelope struct {
		StatusUpdate *a2a.TaskStatusUpdateEvent `json:"statusUpdate"`
	}
	require.NoError(t, json.Unmarshal(<-events, &envelope))
	require.NotNil(t, envelope.StatusUpdate)
	require.Equal(t, a2a.TaskStateCompleted, envelope.StatusUpdate.Status.State)
	require.Empty(t, envelope.StatusUpdate.Metadata)
	_, err = NewPushWorker(store, sender, testPushSigner(t)).poll(t.Context())
	require.NoError(t, err)
	require.Empty(t, events, "failed attempt must wait for retry delay")
}

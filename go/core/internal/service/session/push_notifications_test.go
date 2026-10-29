package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/taskstore"
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
			config, err := initialPushConfig(agent, req, true)
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
	agent := types.NamespacedName{Namespace: "team", Name: "agent"}
	config := &a2a.PushConfig{URL: "http://receiver/callback"}
	_, err := validatePushConfig(agent, config, false)
	require.ErrorIs(t, err, a2a.ErrInvalidParams)
	config.URL = "https://receiver/callback"
	_, err = validatePushConfig(agent, config, false)
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
	deliveries    []database.PushDelivery
	finished      []bool
	finishMu      sync.Mutex
	claimErr      error
	finishErr     error
	stale         bool
	currentErr    error
	expired       atomic.Int32
	claimCalls    atomic.Int32
	pendingChecks atomic.Int32
	released      atomic.Int32
}

var _ pushStore = (*pushTestStore)(nil)

func (p *pushTestStore) HasPendingPushDelivery(context.Context) (bool, error) {
	p.pendingChecks.Add(1)
	return len(p.deliveries) != 0, nil
}

func (p *pushTestStore) ExpireUnboundPushRegistrations(context.Context) error {
	p.expired.Add(1)
	return nil
}

func (p *pushTestStore) ClaimDuePushDelivery(context.Context, time.Duration) (*database.PushDelivery, error) {
	p.claimCalls.Add(1)
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
func (p *pushTestStore) IsPushDeliveryCurrent(context.Context, database.PushDelivery) (bool, error) {
	return !p.stale, p.currentErr
}

func (p *pushTestStore) FinishPushDelivery(ctx context.Context, _ database.PushDelivery, delivered bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.finishMu.Lock()
	defer p.finishMu.Unlock()
	p.finished = append(p.finished, delivered)
	return p.finishErr
}

func (p *pushTestStore) ReleasePushDelivery(ctx context.Context, _ database.PushDelivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.released.Add(1)
	return p.finishErr
}

func (p *pushTestStore) outcomes() []bool {
	p.finishMu.Lock()
	defer p.finishMu.Unlock()
	return slices.Clone(p.finished)
}

type pushTestSender struct {
	calls atomic.Int32
	err   error
}

var _ pushSender = (*pushTestSender)(nil)

func (p *pushTestSender) SendPush(context.Context, *a2a.PushConfig, a2a.Event) error {
	p.calls.Add(1)
	return p.err
}

func testPushSigner(t *testing.T) *PushJWTSigner {
	t.Helper()
	signer, err := NewPushJWTSigner(testPushPrivateKeyPEM, "https://kagent.example")
	require.NoError(t, err)
	return signer
}

func testPushWorkerConfig(recoveryInterval time.Duration) PushWorkerConfig {
	return PushWorkerConfig{RecoveryInterval: recoveryInterval, MaxConcurrency: 2, SendTimeout: time.Minute}
}

// Drive one synchronous pass for publication and retry assertions. Start tests
// below exercise concurrent scheduling and shutdown.
func pollPushWorker(t *testing.T, worker *PushWorker) (bool, error) {
	t.Helper()
	return worker.poll(t.Context(), 100, func(delivery database.PushDelivery) {
		if err := worker.send(t.Context(), delivery); err != nil {
			t.Logf("push attempt: %v", err)
		}
	})
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
	worker := NewPushWorker(store, sender, testPushSigner(t), nil, testPushWorkerConfig(time.Minute))
	full, err := pollPushWorker(t, worker)
	require.NoError(t, err)
	require.True(t, full)
	require.EqualValues(t, 100, sender.calls.Load())

	full, err = pollPushWorker(t, worker)
	require.NoError(t, err)
	require.False(t, full)
}

func TestPushWorkerChecksClaimBeforeSending(t *testing.T) {
	wire, err := pbconv.ToProtoStreamResponse(&a2a.TaskStatusUpdateEvent{TaskID: "task", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}})
	require.NoError(t, err)
	payload, err := proto.Marshal(wire)
	require.NoError(t, err)
	for _, test := range []struct {
		name  string
		stale bool
		err   error
	}{
		{name: "superseded", stale: true},
		{name: "database unavailable", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &pushTestStore{stale: test.stale, currentErr: test.err}
			sender := &pushTestSender{}
			err := NewPushWorker(store, sender, nil, nil, testPushWorkerConfig(time.Minute)).send(t.Context(), database.PushDelivery{Payload: payload})
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
				require.Equal(t, []bool{false}, store.outcomes(), "failed preparation enters the normal retry schedule")
			} else {
				require.NoError(t, err)
				require.Empty(t, store.outcomes(), "skipping a stale claim must not acknowledge newer work")
			}
			require.Zero(t, sender.calls.Load())
		})
	}
}

func TestPushWorkerInflightResponseCannotAcknowledgeNewState(t *testing.T) {
	store, session := lifecycleFixture(t)
	session, err := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
	require.NoError(t, err)
	entered := make(chan *a2a.TaskStatusUpdateEvent, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			StatusUpdate *a2a.TaskStatusUpdateEvent `json:"statusUpdate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil || envelope.StatusUpdate == nil {
			t.Errorf("invalid push status update: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		entered <- envelope.StatusUpdate
		if envelope.StatusUpdate.Status.State == a2a.TaskStateInputRequired {
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	defer unblock()
	task := &a2a.Task{ID: "task", ContextID: session.ContextId, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}
	digest := sha256.Sum256([]byte("create"))
	version, err := store.CreateRuntimeTask(t.Context(), session.Id, digest[:], task, "")
	require.NoError(t, err)
	require.NoError(t, store.SaveTaskPushConfig(t.Context(), session.Id, string(task.ID), &a2a.PushConfig{ID: "callback", URL: receiver.URL}))
	task.Status.State = a2a.TaskStateInputRequired
	digest = sha256.Sum256([]byte("wait"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	worker := NewPushWorker(store, NewHTTPPushSender(5*time.Second, true, true), nil, nil, testPushWorkerConfig(time.Minute))
	done := make(chan error, 1)
	go func() {
		_, err := pollPushWorker(t, worker)
		done <- err
	}()
	select {
	case update := <-entered:
		require.Equal(t, a2a.TaskStateInputRequired, update.Status.State)
	case <-time.After(5 * time.Second):
		t.Fatal("waiting notification did not start")
	}
	task.Status.State = a2a.TaskStateCompleted
	digest = sha256.Sum256([]byte("complete"))
	version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	unblock()
	select {
	case update := <-entered:
		require.Equal(t, a2a.TaskStateCompleted, update.Status.State)
	case <-time.After(5 * time.Second):
		t.Fatal("old HTTP acknowledgement cleared the terminal notification")
	}
	require.NoError(t, <-done)
	pending, err := store.HasPendingPushDelivery(t.Context())
	require.NoError(t, err)
	require.False(t, pending)
}

func TestPushWorkerDurableHTTPDelivery(t *testing.T) {
	for _, signed := range []bool{false, true} {
		name := "unsigned"
		if signed {
			name = "signed"
		}
		t.Run(name, func(t *testing.T) {
			var signer *PushJWTSigner
			if signed {
				signer = testPushSigner(t)
			}
			store, session := lifecycleFixture(t)
			session, err := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			events := make(chan json.RawMessage, 4)
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				require.Empty(t, r.Header.Get("A2A-Notification-Token"))
				if signed {
					require.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
				} else {
					require.Empty(t, r.Header.Get("Authorization"))
				}
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
			_, err = pollPushWorker(t, NewPushWorker(store, sender, signer, nil, testPushWorkerConfig(time.Minute)))
			require.NoError(t, err)
			require.Empty(t, events)
			task.Status.State = a2a.TaskStateCompleted
			digest = sha256.Sum256([]byte("complete"))
			version, err = store.UpdateSessionTask(t.Context(), session.Id, version, digest[:], task, task, "")
			require.NoError(t, err)
			_, err = pollPushWorker(t, NewPushWorker(store, sender, signer, nil, testPushWorkerConfig(time.Minute)))
			require.NoError(t, err)
			require.Empty(t, events, "staged completion must not be notified")
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
			_, err = pollPushWorker(t, NewPushWorker(store, sender, signer, nil, testPushWorkerConfig(time.Minute)))
			require.NoError(t, err)
			require.Len(t, events, 1)
			var envelope struct {
				StatusUpdate *a2a.TaskStatusUpdateEvent `json:"statusUpdate"`
			}
			require.NoError(t, json.Unmarshal(<-events, &envelope))
			require.NotNil(t, envelope.StatusUpdate)
			require.Equal(t, a2a.TaskStateCompleted, envelope.StatusUpdate.Status.State)
			require.Empty(t, envelope.StatusUpdate.Metadata)
			_, err = pollPushWorker(t, NewPushWorker(store, sender, signer, nil, testPushWorkerConfig(time.Minute)))
			require.NoError(t, err)
			require.Empty(t, events, "failed attempt must wait for retry delay")
		})
	}
}

type pushWakeStore struct {
	pushTestStore
	pending    bool
	pendingErr error
	scans      atomic.Int32
	mu         sync.Mutex
	claimGate  <-chan struct{}
}

var _ pushStore = (*pushWakeStore)(nil)

func (p *pushWakeStore) ClaimDuePushDelivery(ctx context.Context, timeout time.Duration) (*database.PushDelivery, error) {
	p.scans.Add(1)
	if p.claimGate != nil {
		select {
		case <-p.claimGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pushTestStore.ClaimDuePushDelivery(ctx, timeout)
}

func (p *pushWakeStore) HasPendingPushDelivery(context.Context) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending, p.pendingErr
}

func startPushWorker(t *testing.T, worker *PushWorker) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- worker.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
}

func TestPushWorkerWakeAndRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &pushWakeStore{}
		wake := make(chan struct{}, 1)
		worker := NewPushWorker(store, &pushTestSender{}, nil, wake, testPushWorkerConfig(time.Minute))
		require.False(t, worker.NeedLeaderElection())
		startPushWorker(t, worker)
		synctest.Wait()
		require.Equal(t, 1, int(store.scans.Load()), "scan at startup")
		time.Sleep(59 * time.Second)
		synctest.Wait()
		require.Equal(t, 1, int(store.scans.Load()), "idle workers must not scan every five seconds")
		wake <- struct{}{}
		synctest.Wait()
		require.Equal(t, 2, int(store.scans.Load()), "a local hint wakes the worker immediately")
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 3, int(store.scans.Load()), "recover work even without a hint")
	})
}

func TestPushWorkerShortRetriesReturnToIdle(t *testing.T) {
	for _, test := range []struct {
		name       string
		pending    bool
		claimErr   error
		pendingErr error
		delay      time.Duration
	}{
		{name: "outstanding retry or lease", pending: true, delay: 5 * time.Second},
		{name: "claim error", claimErr: errors.New("database unavailable"), delay: time.Second},
		{name: "pending check error", pendingErr: errors.New("database unavailable"), delay: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := &pushWakeStore{pending: test.pending, pendingErr: test.pendingErr}
				store.claimErr = test.claimErr
				startPushWorker(t, NewPushWorker(store, &pushTestSender{}, nil, nil, testPushWorkerConfig(time.Minute)))
				synctest.Wait()
				require.Equal(t, 1, int(store.scans.Load()))
				store.mu.Lock()
				store.pending, store.claimErr, store.pendingErr = false, nil, nil
				store.mu.Unlock()
				time.Sleep(test.delay)
				synctest.Wait()
				require.Equal(t, 2, int(store.scans.Load()), "retry promptly without another settlement")
				time.Sleep(59 * time.Second)
				synctest.Wait()
				require.Equal(t, 2, int(store.scans.Load()), "return to long scans after work finishes")
				time.Sleep(time.Second)
				synctest.Wait()
				require.Equal(t, 3, int(store.scans.Load()))
			})
		})
	}
}

func TestPushWorkerRetainsHintDuringEmptyScan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		store := &pushWakeStore{claimGate: gate}
		wake := make(chan struct{}, 1)
		startPushWorker(t, NewPushWorker(store, &pushTestSender{}, nil, wake, testPushWorkerConfig(time.Hour)))
		synctest.Wait()
		require.Equal(t, 1, int(store.scans.Load()))
		// Settlement races with the last empty claim. The hint must survive
		// until the worker waits, rather than delaying work for an hour.
		wake <- struct{}{}
		close(gate)
		synctest.Wait()
		require.Equal(t, 2, int(store.scans.Load()))
	})
}

type observedPushStore struct {
	*database.Client
	idle atomic.Int32
}

var _ pushStore = (*observedPushStore)(nil)

func (p *observedPushStore) HasPendingPushDelivery(ctx context.Context) (bool, error) {
	pending, err := p.Client.HasPendingPushDelivery(ctx)
	p.idle.Add(1)
	return pending, err
}

type gatedPushSender struct {
	entered chan a2a.TaskID
	release <-chan struct{}
}

var _ pushSender = (*gatedPushSender)(nil)

func (p *gatedPushSender) SendPush(ctx context.Context, _ *a2a.PushConfig, event a2a.Event) error {
	p.entered <- event.TaskInfo().TaskID
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type pushSenderFunc func(context.Context, *a2a.PushConfig, a2a.Event) error

var _ pushSender = pushSenderFunc(nil)

func (p pushSenderFunc) SendPush(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error {
	return p(ctx, config, event)
}

func pushTestDelivery(t *testing.T, taskID a2a.TaskID) database.PushDelivery {
	t.Helper()
	wire, err := pbconv.ToProtoStreamResponse(&a2a.TaskStatusUpdateEvent{TaskID: taskID, Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}})
	require.NoError(t, err)
	payload, err := proto.Marshal(wire)
	require.NoError(t, err)
	return database.PushDelivery{Payload: payload}
}

func TestPushWorkerConcurrentDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &pushTestStore{deliveries: []database.PushDelivery{pushTestDelivery(t, "slow")}}
		// Cross multiple maintenance batches while the first request is blocked.
		for range 205 {
			store.deliveries = append(store.deliveries, pushTestDelivery(t, "fast"))
		}
		fastGate := make(chan struct{})
		started := make(chan a2a.TaskID, len(store.deliveries)+1)
		sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, event a2a.Event) error {
			id := event.TaskInfo().TaskID
			started <- id
			if id == "slow" {
				<-ctx.Done()
				return ctx.Err()
			}
			select {
			case <-fastGate:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		wake := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		worker := NewPushWorker(store, sender, nil, wake, testPushWorkerConfig(time.Hour))
		go func() { done <- worker.Start(ctx) }()
		synctest.Wait()
		require.Len(t, started, 2, "enforce the configured in-flight limit")
		require.Len(t, store.deliveries, 204, "do not lease work before a slot is available")
		close(fastGate)
		synctest.Wait()
		require.Len(t, store.outcomes(), 205, "a blocked receiver must not hold up subsequent batches")
		require.Empty(t, store.deliveries)
		require.EqualValues(t, 1, store.expired.Load(), "delivery completions must not repeat registration maintenance")

		// A new settlement must also use spare capacity while another send is busy.
		store.deliveries = append(store.deliveries, pushTestDelivery(t, "new"))
		wake <- struct{}{}
		synctest.Wait()
		require.Len(t, store.outcomes(), 206)
		cancel()
		synctest.Wait()
		require.NoError(t, <-done)
		require.Len(t, store.outcomes(), 206)
		require.EqualValues(t, 1, store.released.Load(), "shutdown waits for the interrupted attempt to be released")
	})
}

func TestPushWorkerSendTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &pushTestStore{deliveries: []database.PushDelivery{pushTestDelivery(t, "slow"), pushTestDelivery(t, "fast")}}
		sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, event a2a.Event) error {
			if event.TaskInfo().TaskID == "slow" {
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		})
		worker := NewPushWorker(store, sender, nil, nil, PushWorkerConfig{
			RecoveryInterval: time.Hour, MaxConcurrency: 1, SendTimeout: 2 * time.Second,
		})
		startPushWorker(t, worker)
		synctest.Wait()
		require.Len(t, store.deliveries, 1, "concurrency one preserves serial delivery")
		time.Sleep(1999 * time.Millisecond)
		synctest.Wait()
		require.Empty(t, store.outcomes())
		time.Sleep(time.Millisecond)
		synctest.Wait()
		require.Equal(t, []bool{false, true}, store.outcomes(), "timeout records failure and releases capacity immediately")
	})
}

type pushPendingGateStore struct {
	pushTestStore
	gate <-chan struct{}
}

var _ pushStore = (*pushPendingGateStore)(nil)

func (p *pushPendingGateStore) HasPendingPushDelivery(ctx context.Context) (bool, error) {
	pending, err := p.pushTestStore.HasPendingPushDelivery(ctx)
	select {
	case <-p.gate:
		return pending, err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func TestPushWorkerRefillsAfterQueuedCompletions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pendingGate := make(chan struct{})
		initialGate := make(chan struct{})
		store := &pushPendingGateStore{gate: pendingGate}
		for range 2 {
			store.deliveries = append(store.deliveries, pushTestDelivery(t, "initial"))
		}
		var started atomic.Int32
		sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, event a2a.Event) error {
			started.Add(1)
			if event.TaskInfo().TaskID == "initial" {
				<-initialGate
				return nil
			}
			<-ctx.Done()
			return ctx.Err()
		})
		config := testPushWorkerConfig(time.Hour)
		config.MaxConcurrency = 4
		startPushWorker(t, NewPushWorker(store, sender, nil, nil, config))
		synctest.Wait()
		require.EqualValues(t, 1, store.pendingChecks.Load())
		// Both completions queue while the worker is blocked in the pending query.
		close(initialGate)
		synctest.Wait()
		for range 4 {
			store.deliveries = append(store.deliveries, pushTestDelivery(t, "next"))
		}
		close(pendingGate)
		synctest.Wait()
		require.EqualValues(t, 6, started.Load(), "queued completions must free every slot")
		require.EqualValues(t, 1, store.pendingChecks.Load(), "refilling capacity needs no pending-work query")
		require.EqualValues(t, 1, store.expired.Load(), "completion bursts must not cause maintenance bursts")
	})
}

func TestPushWorkerSaturationKeepsOnlyPeriodicCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &pushTestStore{deliveries: []database.PushDelivery{pushTestDelivery(t, "slow")}}
		sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, _ a2a.Event) error {
			<-ctx.Done()
			return ctx.Err()
		})
		wake := make(chan struct{}, 1)
		startPushWorker(t, NewPushWorker(store, sender, nil, wake, PushWorkerConfig{
			RecoveryInterval: time.Minute, MaxConcurrency: 1, SendTimeout: time.Hour,
		}))
		synctest.Wait()
		wake <- struct{}{}
		synctest.Wait()
		require.Zero(t, store.pendingChecks.Load())
		require.EqualValues(t, 1, store.expired.Load())
		time.Sleep(time.Minute)
		synctest.Wait()
		require.EqualValues(t, 2, store.expired.Load(), "cleanup must continue while all slots are occupied")
		require.Zero(t, store.pendingChecks.Load())
		require.EqualValues(t, 1, store.claimCalls.Load(), "saturation must not poll for more deliveries")
	})
}

type pushCheckTimeoutStore struct{ pushTestStore }

var _ pushStore = (*pushCheckTimeoutStore)(nil)

func (p *pushCheckTimeoutStore) IsPushDeliveryCurrent(ctx context.Context, _ database.PushDelivery) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

func TestPushWorkerCheckTimeoutRecordsFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &pushCheckTimeoutStore{}
		sender := &pushTestSender{}
		config := testPushWorkerConfig(time.Minute)
		config.SendTimeout = 2 * time.Second
		worker := NewPushWorker(store, sender, nil, nil, config)
		started := time.Now()
		err := worker.send(t.Context(), pushTestDelivery(t, "task"))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 2*time.Second, time.Since(started))
		require.Zero(t, sender.calls.Load())
		require.Equal(t, []bool{false}, store.outcomes())
		require.Zero(t, store.released.Load(), "a send timeout still consumes an attempt")
	})
}

type pushFinishTimeoutStore struct{ pushTestStore }

var _ pushStore = (*pushFinishTimeoutStore)(nil)

func (p *pushFinishTimeoutStore) FinishPushDelivery(ctx context.Context, _ database.PushDelivery, _ bool) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestPushWorkerFinalizationIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sender := pushSenderFunc(func(context.Context, *a2a.PushConfig, a2a.Event) error {
			cancel() // Shutdown races with a successful response.
			return nil
		})
		worker := NewPushWorker(&pushFinishTimeoutStore{}, sender, nil, nil, testPushWorkerConfig(time.Minute))
		started := time.Now()
		err := worker.send(ctx, pushTestDelivery(t, "task"))
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 5*time.Second, time.Since(started), "finalization survives cancellation but cannot hang shutdown")
	})
}

func TestPushWorkerShutdownPersistsOutcomes(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		name := "interrupted"
		if delivered {
			name = "acknowledged"
		}
		t.Run(name, func(t *testing.T) {
			store, session := lifecycleFixture(t)
			session, err := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, make(chan struct{}, 1), time.Second).Create(t.Context(), session)
			require.NoError(t, err)
			request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
			require.NoError(t, store.SaveTaskPushConfig(t.Context(), session.Id, request.TaskId, &a2a.PushConfig{ID: "callback", URL: "http://receiver"}))
			require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
			started := make(chan struct{}, 1)
			sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, _ a2a.Event) error {
				started <- struct{}{}
				<-ctx.Done()
				if delivered {
					return nil // The HTTP response succeeded as shutdown began.
				}
				return ctx.Err()
			})
			restarts := 12 // Exceed the durable ten-attempt limit.
			if delivered {
				restarts = 1
			}
			for range restarts {
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				// Recover transient empty claims well before the assertion deadline.
				worker := NewPushWorker(store, sender, nil, nil, testPushWorkerConfig(10*time.Millisecond))
				go func() { done <- worker.Start(ctx) }()
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					cancel()
					require.NoError(t, <-done)
					t.Fatal("shutdown lost a pending notification or left its claim leased")
				}
				cancel()
				require.NoError(t, <-done)
			}
			if delivered {
				next, err := store.ClaimDuePushDelivery(t.Context(), time.Second)
				require.NoError(t, err)
				require.Nil(t, next, "successful delivery must remain acknowledged after shutdown")
				pending, err := store.HasPendingPushDelivery(t.Context())
				require.NoError(t, err)
				require.False(t, pending)
			} else {
				// SKIP LOCKED can return no claim while a canceled transaction unwinds.
				require.EventuallyWithT(t, func(collect *assert.CollectT) {
					next, err := store.ClaimDuePushDelivery(t.Context(), time.Second)
					require.NoError(collect, err)
					require.NotNil(collect, next, "repeated shutdowns must not exhaust the delivery budget")
				}, 5*time.Second, 10*time.Millisecond)
			}
		})
	}
}

func TestPushWorkerConcurrentHTTPDelivery(t *testing.T) {
	store, session := lifecycleFixture(t)
	creator := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, make(chan struct{}, 1), time.Second)
	session, err := creator.Create(t.Context(), session)
	require.NoError(t, err)
	request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
	slowStarted := make(chan struct{}, 1)
	fastFinished := make(chan struct{}, 1)
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			slowStarted <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		} else {
			fastFinished <- struct{}{}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	defer close(release)
	require.NoError(t, store.SaveTaskPushConfig(t.Context(), session.Id, request.TaskId, &a2a.PushConfig{
		ID: "slow", URL: receiver.URL + "/slow",
	}))
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, request.TaskId, request.Version))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	wake := make(chan struct{}, 1)
	worker := NewPushWorker(store, NewHTTPPushSender(time.Minute, true, true), nil, wake, testPushWorkerConfig(time.Minute))
	go func() { done <- worker.Start(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("slow HTTP delivery did not start")
	}
	// Publish another task only after the slow request is in flight.
	fastSession, _, err := store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: session.Agent}, uuid.NewString())
	require.NoError(t, err)
	fastSession, err = creator.Create(t.Context(), fastSession)
	require.NoError(t, err)
	fastRequest := stageQuiescenceTask(t, store.Client, fastSession, a2a.TaskStateCompleted)
	require.NoError(t, store.SaveTaskPushConfig(t.Context(), fastSession.Id, fastRequest.TaskId, &a2a.PushConfig{
		ID: "fast", URL: receiver.URL + "/fast",
	}))
	_, err = taskstore.NewService(store, nil, wake).SettleTask(settlementContext(t, fastSession), fastRequest)
	require.NoError(t, err)
	select {
	case <-fastFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("slow receiver blocked another HTTP delivery")
	}
}

// Replicas share PostgreSQL, but have independent wake channels. A settlement
// burst during HTTP sends must remain durable and drain without recovery scans.
func TestPushWorkersDrainAcrossReplicas(t *testing.T) {
	store, first := lifecycleFixture(t)
	creator := NewActorWorkflow(store, &lifecycleTestActors{actors: map[string]*ateapipb.Actor{}}, make(chan struct{}, 1), time.Hour)
	var sessions []*apiv1.Session
	var requests []*runtimev1.TaskStoreServiceSettleTaskRequest
	for i := range 12 {
		session := first
		if i != 0 {
			var err error
			session, _, err = store.CreateSession(t.Context(), &apiv1.Session{Id: uuid.NewString(), Creator: "alice", Agent: first.Agent}, uuid.NewString())
			require.NoError(t, err)
		}
		session, err := creator.Create(t.Context(), session)
		require.NoError(t, err)
		request := stageQuiescenceTask(t, store.Client, session, a2a.TaskStateCompleted)
		// The committed initial projection is still active until settlement.
		require.NoError(t, store.SaveTaskPushConfig(t.Context(), session.Id, request.TaskId, &a2a.PushConfig{ID: "callback", URL: "http://receiver"}))
		sessions = append(sessions, session)
		requests = append(requests, request)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSenders := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseSenders()
	entered := make(chan a2a.TaskID, len(requests)*2)
	writes := &observedPushStore{Client: store.Client}
	var services []*taskstore.Service
	for range 2 {
		wake := make(chan struct{}, 1)
		startPushWorker(t, NewPushWorker(writes, &gatedPushSender{entered: entered, release: release}, nil, wake, testPushWorkerConfig(time.Hour)))
		services = append(services, taskstore.NewService(store, nil, wake))
	}
	require.Eventually(t, func() bool { return writes.idle.Load() >= 2 }, 5*time.Second, time.Millisecond)
	for i := range 2 {
		_, err := services[i].SettleTask(settlementContext(t, sessions[i]), requests[i])
		require.NoError(t, err)
		require.Eventually(t, func() bool { return len(entered) == i+1 }, 5*time.Second, time.Millisecond)
	}
	for i := 2; i < len(requests); i++ {
		_, err := services[i%2].SettleTask(settlementContext(t, sessions[i]), requests[i])
		require.NoError(t, err, "a busy sender must not block settlement")
	}
	releaseSenders()
	require.Eventually(t, func() bool {
		pending, err := writes.HasPendingPushDelivery(t.Context())
		return err == nil && !pending
	}, 5*time.Second, time.Millisecond)
	require.Len(t, entered, len(requests))
	seen := make(map[a2a.TaskID]bool)
	for range len(requests) {
		id := <-entered
		require.False(t, seen[id], "an active delivery must not be sent by both replicas")
		seen[id] = true
	}
}

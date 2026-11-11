package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/version"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/telemetrytest"
)

func captureWorkTelemetry(t *testing.T) (context.Context, *sdkmetric.MeterProvider, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	logs := &bytes.Buffer{}
	logger, err := logging.New(logs, "debug")
	require.NoError(t, err)
	return logging.IntoContext(t.Context(), logger), provider, reader, logs
}

func wantWorkClaims(trigger string, count int64) map[string]int64 {
	values := map[string]int64{"signal": 0, "recovery": 0, "startup": 0, "retry": 0, "backlog": 0}
	if trigger != "" {
		values[trigger] = count
	}
	return values
}

// Assert the exported contract as well as the independently accumulated values.
func workClaimValues(t *testing.T, reader *sdkmetric.ManualReader, metricName string) map[string]int64 {
	t.Helper()
	data := telemetrytest.Collect(t, reader)
	values := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		if scope.Scope.Name != "github.com/kagent-dev/kagent/go/core/internal/service/session" {
			continue // Integration fixtures can also emit upstream HTTP metrics.
		}
		require.Equal(t, version.Version, scope.Scope.Version)
		for _, sample := range scope.Metrics {
			switch sample.Name {
			case "kagent.session.quiescence.claimed":
				require.Equal(t, "{session}", sample.Unit)
				require.Equal(t, "Number of session quiescence claims acquired.", sample.Description)
			case "kagent.push.delivery.claimed":
				require.Equal(t, "{attempt}", sample.Unit)
				require.Equal(t, "Number of push delivery attempts claimed.", sample.Description)
			default:
				t.Fatalf("unexpected metric %q: no shared, optional scan, or success metric", sample.Name)
			}
			sum, ok := sample.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.True(t, sum.IsMonotonic)
			require.Equal(t, metricdata.CumulativeTemporality, sum.Temporality)
			require.Len(t, sum.DataPoints, 5, "all trigger series must be present, including zero values")
			for _, point := range sum.DataPoints {
				require.Equal(t, 1, point.Attributes.Len(), "only the trigger; no worker kind, identities, or raw errors")
				trigger, found := point.Attributes.Value(attribute.Key("kagent.worker.trigger"))
				require.True(t, found)
				require.Contains(t, []string{"signal", "recovery", "startup", "retry", "backlog"}, trigger.AsString())
				if sample.Name == metricName {
					values[trigger.AsString()] += point.Value
				}
			}
		}
	}
	return values
}

func requireRecoveryLog(t *testing.T, logs *bytes.Buffer, operation string, id string, interval time.Duration) {
	t.Helper()
	type logRecord struct {
		Level          string  `json:"level"`
		Message        string  `json:"msg"`
		Operation      string  `json:"operation"`
		Interval       float64 `json:"recovery_interval_seconds"`
		Trigger        *string `json:"trigger"`
		ClaimedCount   *int    `json:"claimed_count"`
		SessionID      string  `json:"session_id"`
		RegistrationID string  `json:"registration_id"`
	}
	var info []logRecord
	for line := range bytes.SplitSeq(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record logRecord
		require.NoError(t, json.Unmarshal(line, &record))
		if record.Level == slog.LevelInfo.String() {
			info = append(info, record)
		}
	}
	if id == "" {
		require.Empty(t, info, "empty and non-recovery passes must be silent")
		return
	}
	require.Len(t, info, 1)
	require.Equal(t, "recovery scan found work", info[0].Message)
	require.Equal(t, operation, info[0].Operation)
	require.Equal(t, interval.Seconds(), info[0].Interval)
	require.Nil(t, info[0].Trigger)
	require.Nil(t, info[0].ClaimedCount)
	if operation == "session_quiescence" {
		require.Equal(t, id, info[0].SessionID)
		require.Empty(t, info[0].RegistrationID)
	} else {
		require.Equal(t, id, info[0].RegistrationID)
		require.Empty(t, info[0].SessionID)
	}
}

type telemetryQuiescenceStore struct {
	*idleQuiescenceStore
	remaining    atomic.Int32
	runtimeCalls atomic.Int32
	runtimeGate  <-chan struct{}
}

var _ workflowStore = (*telemetryQuiescenceStore)(nil)

func (s *telemetryQuiescenceStore) ClaimSessionQuiescence(ctx context.Context) (*database.SessionQuiescence, error) {
	for remaining := s.remaining.Load(); remaining > 0; remaining = s.remaining.Load() {
		if s.remaining.CompareAndSwap(remaining, remaining-1) {
			return &database.SessionQuiescence{Session: &apiv1.Session{Id: "private-session"}}, nil
		}
	}
	return s.idleQuiescenceStore.ClaimSessionQuiescence(ctx)
}

func (s *telemetryQuiescenceStore) GetRuntimeRevision(ctx context.Context, _ string) (*database.RuntimeRevision, error) {
	s.runtimeCalls.Add(1)
	if s.runtimeGate != nil {
		select {
		case <-s.runtimeGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("runtime unavailable") // Claims count even when execution fails.
}

func TestQuiescenceWorkClaimTriggers(t *testing.T) {
	for _, trigger := range []string{"startup", "signal", "recovery", "retry"} {
		t.Run(trigger, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, provider, reader, logs := captureWorkTelemetry(t)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				store := &telemetryQuiescenceStore{idleQuiescenceStore: &idleQuiescenceStore{}}
				store.pending.Store(trigger == "retry")
				if trigger == "startup" {
					store.remaining.Store(1)
				}
				wake := make(chan struct{}, 1)
				// Equal recovery and retry delays must still have distinct labels.
				workflow := NewActorWorkflow(store, nil, wake, time.Second, provider)
				done := make(chan error, 1)
				go func() { done <- workflow.Start(ctx) }()
				synctest.Wait()
				if trigger != "startup" {
					require.Equal(t, wantWorkClaims("", 0), workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
					requireRecoveryLog(t, logs, "session_quiescence", "", time.Second)
					store.remaining.Store(1)
					if trigger == "signal" {
						wake <- struct{}{}
					} else {
						time.Sleep(time.Second)
					}
					synctest.Wait()
				}
				require.Equal(t, wantWorkClaims(trigger, 1), workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
				id := ""
				if trigger == "recovery" {
					id = "private-session"
				}
				requireRecoveryLog(t, logs, "session_quiescence", id, time.Second)
				time.Sleep(time.Second)
				synctest.Wait()
				require.Equal(t, wantWorkClaims(trigger, 1), workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
				requireRecoveryLog(t, logs, "session_quiescence", id, time.Second)
				cancel()
				require.NoError(t, <-done)
			})
		})
	}
}

func TestQuiescenceHandoffsCountAsBacklog(t *testing.T) {
	for _, trigger := range []string{"signal", "recovery"} {
		t.Run(trigger, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, provider, reader, logs := captureWorkTelemetry(t)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				release := make(chan struct{})
				store := &telemetryQuiescenceStore{
					idleQuiescenceStore: &idleQuiescenceStore{}, runtimeGate: release,
				}
				wake := make(chan struct{}, 1)
				done := make(chan error, 1)
				go func() { done <- NewActorWorkflow(store, nil, wake, time.Second, provider).Start(ctx) }()
				synctest.Wait()
				require.Equal(t, wantWorkClaims("", 0), workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
				store.remaining.Store(12)
				if trigger == "signal" {
					wake <- struct{}{}
				} else {
					time.Sleep(time.Second)
				}
				synctest.Wait()
				require.EqualValues(t, 4, store.runtimeCalls.Load(), "handoffs enlist idle workers before runtime I/O finishes")
				initial := workClaimValues(t, reader, "kagent.session.quiescence.claimed")
				discoveryClaims := initial[trigger]
				require.GreaterOrEqual(t, discoveryClaims, int64(1))
				require.LessOrEqual(t, discoveryClaims, int64(4))
				want := wantWorkClaims(trigger, discoveryClaims)
				want["backlog"] = 4 - discoveryClaims
				require.Equal(t, want, initial)
				discoveries := bytes.Count(logs.Bytes(), []byte("recovery scan found work"))
				if trigger == "recovery" {
					// Independent timers can each discover work; handoffs cannot.
					require.EqualValues(t, discoveryClaims, discoveries)
				} else {
					require.Zero(t, discoveries)
					require.EqualValues(t, 1, discoveryClaims)
				}
				close(release)
				synctest.Wait()
				want["backlog"] = 12 - discoveryClaims
				require.Equal(t, want, workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
				require.Equal(t, discoveries, bytes.Count(logs.Bytes(), []byte("recovery scan found work")), "continued claims must not log per item")
				// Once empty, a fresh signal can discover new work.
				store.remaining.Store(1)
				wake <- struct{}{}
				synctest.Wait()
				want["signal"]++
				require.Equal(t, want, workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
				cancel()
				require.NoError(t, <-done)
			})
		})
	}
}

func TestPushRecoveryLogsBeforeNextClaimCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Given an idle worker and a store that can block after its first claim.
		ctx, provider, reader, logs := captureWorkTelemetry(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		claims := make(chan struct{}, 1)
		claims <- struct{}{} // Allow the empty startup scan.
		store := &pushWakeStore{claimGate: claims}
		worker := NewPushWorker(store, &pushTestSender{}, nil, nil, testPushWorkerConfig(time.Minute), provider)
		done := make(chan error, 1)
		go func() { done <- worker.Start(ctx) }()
		synctest.Wait()
		store.mu.Lock()
		first, second := pushTestDelivery(t, "first"), pushTestDelivery(t, "second")
		first.ID, second.ID = uuid.New(), uuid.New()
		store.deliveries = []database.PushDelivery{first, second}
		store.mu.Unlock()

		// When recovery acquires work and its next claim blocks.
		claims <- struct{}{}
		time.Sleep(time.Minute)
		synctest.Wait()

		// Assert that discovery is already visible before the batch completes.
		require.Equal(t, wantWorkClaims("recovery", 1), workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", first.ID.String(), time.Minute)
		close(claims)
		synctest.Wait()
		want := wantWorkClaims("recovery", 1)
		want["backlog"] = 1
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", first.ID.String(), time.Minute)
		cancel()
		require.NoError(t, <-done)
	})
}

func TestPushWorkClaimTriggers(t *testing.T) {
	for _, trigger := range []string{"startup", "signal", "recovery", "retry"} {
		t.Run(trigger, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, provider, reader, logs := captureWorkTelemetry(t)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				store := &pushWakeStore{pending: trigger == "retry"}
				if trigger == "startup" {
					store.deliveries = make([]database.PushDelivery, 5)
				}
				wake := make(chan struct{}, 1)
				worker := NewPushWorker(store, &pushTestSender{}, nil, wake, testPushWorkerConfig(time.Second), provider)
				done := make(chan error, 1)
				go func() { done <- worker.Start(ctx) }()
				synctest.Wait()
				if trigger != "startup" {
					require.Equal(t, wantWorkClaims("", 0), workClaimValues(t, reader, "kagent.push.delivery.claimed"))
					requireRecoveryLog(t, logs, "push_delivery", "", time.Second)
					store.mu.Lock()
					store.deliveries = make([]database.PushDelivery, 5)
					store.mu.Unlock()
					if trigger == "signal" {
						wake <- struct{}{}
					} else {
						time.Sleep(time.Second)
					}
					synctest.Wait()
				}
				want := wantWorkClaims(trigger, 1)
				want["backlog"] = 4
				require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
				id := ""
				if trigger == "recovery" {
					id = uuid.Nil.String()
				}
				requireRecoveryLog(t, logs, "push_delivery", id, time.Second)
				time.Sleep(time.Second)
				synctest.Wait()
				require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
				requireRecoveryLog(t, logs, "push_delivery", id, time.Second)
				cancel()
				require.NoError(t, <-done)
			})
		})
	}
}

// A database error after a successful claim must not erase discovery telemetry.
type partialPushClaimStore struct {
	pushWakeStore
	failOnEmpty bool
}

var _ pushStore = (*partialPushClaimStore)(nil)

func (p *partialPushClaimStore) ClaimDuePushDelivery(ctx context.Context, timeout time.Duration) (*database.PushDelivery, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failOnEmpty && len(p.deliveries) == 0 {
		p.failOnEmpty = false
		return nil, errors.New("database unavailable")
	}
	return p.pushTestStore.ClaimDuePushDelivery(ctx, timeout)
}

func TestPushRecoveryRetainsClaimsBeforeScanError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, provider, reader, logs := captureWorkTelemetry(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		store := &partialPushClaimStore{}
		release := make(chan struct{})
		sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, _ a2a.Event) error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		worker := NewPushWorker(store, sender, nil, nil, testPushWorkerConfig(time.Minute), provider)
		done := make(chan error, 1)
		go func() { done <- worker.Start(ctx) }()
		synctest.Wait()
		store.mu.Lock()
		store.deliveries = []database.PushDelivery{pushTestDelivery(t, "first")}
		store.failOnEmpty = true
		store.mu.Unlock()
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, wantWorkClaims("recovery", 1), workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", uuid.Nil.String(), time.Minute)
		require.Contains(t, logs.String(), "failed to process push notifications")
		store.mu.Lock()
		store.deliveries = []database.PushDelivery{pushTestDelivery(t, "retry")}
		store.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		want := wantWorkClaims("recovery", 1)
		want["retry"] = 1
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"), "an error recheck starts a retry drain")
		requireRecoveryLog(t, logs, "push_delivery", uuid.Nil.String(), time.Minute)
		close(release)
		synctest.Wait()
		cancel()
		require.NoError(t, <-done)
	})
}

func TestPushCompletionClaimsCountAsBacklog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, provider, reader, logs := captureWorkTelemetry(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		store := &pushWakeStore{}
		store.deliveries = make([]database.PushDelivery, 3)
		config := testPushWorkerConfig(time.Hour)
		config.MaxConcurrency = 1
		worker := NewPushWorker(store, &pushTestSender{}, nil, nil, config, provider)
		done := make(chan error, 1)
		go func() { done <- worker.Start(ctx) }()
		synctest.Wait()
		want := wantWorkClaims("startup", 1)
		want["backlog"] = 2
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", "", time.Second)
		cancel()
		require.NoError(t, <-done)
	})
}

func TestWorkerClaimMetricsRemainSeparate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, provider, reader, logs := captureWorkTelemetry(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		quiescence := &telemetryQuiescenceStore{idleQuiescenceStore: &idleQuiescenceStore{}}
		quiescence.remaining.Store(1)
		push := &pushWakeStore{}
		push.deliveries = make([]database.PushDelivery, 2)
		quiescenceDone, pushDone := make(chan error, 1), make(chan error, 1)
		go func() {
			quiescenceDone <- NewActorWorkflow(quiescence, nil, make(chan struct{}, 1), time.Hour, provider).Start(ctx)
		}()
		go func() {
			pushDone <- NewPushWorker(push, &pushTestSender{}, nil, nil, testPushWorkerConfig(time.Hour), provider).Start(ctx)
		}()
		synctest.Wait()
		require.Equal(t, wantWorkClaims("startup", 1), workClaimValues(t, reader, "kagent.session.quiescence.claimed"))
		want := wantWorkClaims("startup", 1)
		want["backlog"] = 1
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "", "", time.Second)
		cancel()
		require.NoError(t, <-quiescenceDone)
		require.NoError(t, <-pushDone)
	})
}

func TestPushRecoveryDiscoveryExcludesBacklog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, provider, reader, logs := captureWorkTelemetry(t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		oldGate, recoveryGate := make(chan struct{}), make(chan struct{})
		sender := pushSenderFunc(func(ctx context.Context, _ *a2a.PushConfig, event a2a.Event) error {
			gate := recoveryGate
			if event.TaskInfo().TaskID == "old" {
				gate = oldGate
			}
			select {
			case <-gate:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		store := &pushWakeStore{}
		store.deliveries = []database.PushDelivery{pushTestDelivery(t, "old")}
		wake := make(chan struct{}, 1)
		worker := NewPushWorker(store, sender, nil, wake, testPushWorkerConfig(time.Second), provider)
		done := make(chan error, 1)
		go func() { done <- worker.Start(ctx) }()
		synctest.Wait()
		require.Equal(t, wantWorkClaims("startup", 1), workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		// The startup drain found no more work, while its send is still blocked.
		store.mu.Lock()
		for range 3 {
			store.deliveries = append(store.deliveries, pushTestDelivery(t, "recovery"))
		}
		store.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		want := wantWorkClaims("startup", 1)
		want["recovery"] = 1
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", uuid.Nil.String(), time.Second)
		claims := store.claimCalls.Load()
		// New signalled work arriving during the drain must not inflate recovery.
		store.mu.Lock()
		for range 2 {
			store.deliveries = append(store.deliveries, pushTestDelivery(t, "arrived"))
		}
		store.mu.Unlock()
		wake <- struct{}{}
		synctest.Wait()
		require.Equal(t, claims, store.claimCalls.Load(), "a saturated wake must not query or relabel the drain")
		close(oldGate)
		synctest.Wait()
		want["backlog"] = 1
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"), "an old send's completion must count continued claims as backlog")
		close(recoveryGate)
		synctest.Wait()
		want["backlog"] = 4
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", uuid.Nil.String(), time.Second)
		// Once empty, a new settlement is a signal drain.
		store.mu.Lock()
		store.deliveries = []database.PushDelivery{pushTestDelivery(t, "fresh")}
		store.mu.Unlock()
		wake <- struct{}{}
		synctest.Wait()
		want["signal"] = 1
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		requireRecoveryLog(t, logs, "push_delivery", uuid.Nil.String(), time.Second)
		// A later recovery wake is a new discovery, not continuation of this one.
		store.mu.Lock()
		store.deliveries = []database.PushDelivery{pushTestDelivery(t, "later")}
		store.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		want["recovery"]++
		require.Equal(t, want, workClaimValues(t, reader, "kagent.push.delivery.claimed"))
		require.Equal(t, 2, bytes.Count(logs.Bytes(), []byte("recovery scan found work")))
		cancel()
		require.NoError(t, <-done)
	})
}

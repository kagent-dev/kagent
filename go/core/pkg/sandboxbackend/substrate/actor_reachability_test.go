package substrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

type stubActorGetter struct {
	status atomic.Int32
}

func (s *stubActorGetter) GetActor(context.Context, string) (*ateapipb.Actor, error) {
	return &ateapipb.Actor{Status: ateapipb.Actor_Status(s.status.Load())}, nil
}

func TestProbeActorViaAtenetRouterSetsActorHost(t *testing.T) {
	t.Parallel()

	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	status, err := probeActorViaAtenetRouter(
		context.Background(),
		srv.Client(),
		srv.URL+"/health",
		"asr-kagent-demo.actors.resources.substrate.ate.dev",
	)
	if err != nil {
		t.Fatalf("probeActorViaAtenetRouter() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if gotHost != "asr-kagent-demo.actors.resources.substrate.ate.dev" {
		t.Fatalf("Host = %q", gotHost)
	}
}

func TestWaitForActorReachableViaAtenetRetriesUntilHealthy(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	actors := &stubActorGetter{}
	actors.status.Store(int32(ateapipb.Actor_STATUS_RUNNING))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	err := waitForActorReachableViaAtenet(
		ctx,
		actors,
		srv.Client(),
		srv.URL,
		"asr-kagent-demo",
	)
	if err != nil {
		t.Fatalf("waitForActorReachableViaAtenet() error = %v", err)
	}
	if attempts.Load() < 3 {
		t.Fatalf("attempts = %d, want >= 3", attempts.Load())
	}
}

func TestWaitForActorReachableViaAtenetWaitsForRunningStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	actors := &stubActorGetter{}
	actors.status.Store(int32(ateapipb.Actor_STATUS_RESUMING))

	go func() {
		time.Sleep(750 * time.Millisecond)
		actors.status.Store(int32(ateapipb.Actor_STATUS_RUNNING))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	err := waitForActorReachableViaAtenet(
		ctx,
		actors,
		srv.Client(),
		srv.URL,
		"asr-kagent-demo",
	)
	if err != nil {
		t.Fatalf("waitForActorReachableViaAtenet() error = %v", err)
	}
}

func TestWaitForActorReachableViaAtenetTimesOut(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	actors := &stubActorGetter{}
	actors.status.Store(int32(ateapipb.Actor_STATUS_RUNNING))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	err := waitForActorReachableViaAtenet(
		ctx,
		actors,
		srv.Client(),
		srv.URL,
		"asr-kagent-demo",
	)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

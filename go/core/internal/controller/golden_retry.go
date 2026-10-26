package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/controllers"
	"k8s.io/apimachinery/pkg/util/wait"

	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
)

// goldenCrashPrefix starts Substrate's "<Reason>: <detail>" error_message when
// the golden actor crashed before its snapshot. Other reasons stay terminal.
// Substrate exposes no structured reason; TestGoldenCrashFormatMatchesSubstrate
// pins this against its source.
const goldenCrashPrefix = "GoldenActorCrashed:"

const goldenRetryingReason = "ActorTemplateRetrying"

type goldenRetryPolicy struct {
	Delays            []time.Duration
	MaxJitterFraction float64
}

var defaultGoldenRetryPolicy = goldenRetryPolicy{
	Delays:            []time.Duration{15 * time.Second, time.Minute, 4 * time.Minute, 10 * time.Minute},
	MaxJitterFraction: 0.2,
}

func (p goldenRetryPolicy) jitteredDelays(jitter func(time.Duration, float64) time.Duration) []time.Duration {
	delays := make([]time.Duration, len(p.Delays))
	for i, delay := range p.Delays {
		delays[i] = jitter(delay, p.MaxJitterFraction)
	}
	return delays
}

func isGoldenCrash(template *ateapipb.ActorTemplate) bool {
	return strings.HasPrefix(template.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage(), goldenCrashPrefix)
}

type goldenRetryStore interface {
	EnsureGoldenRetry(ctx context.Context, revision, crashedTemplateUID string, now time.Time, delays []time.Duration) (database.GoldenRetry, error)
	GoldenRetryAttempts(ctx context.Context, revision string) (int, error)
}

type goldenRetries struct {
	store     goldenRetryStore
	templates actorTemplateClient
	queue     controllers.Queue
	policy    goldenRetryPolicy
	jitter    func(time.Duration, float64) time.Duration

	mu      sync.Mutex
	timers  map[string]*time.Timer
	stopped bool
}

func newGoldenRetries(store goldenRetryStore, templates actorTemplateClient, queue controllers.Queue) *goldenRetries {
	return &goldenRetries{store: store, templates: templates, queue: queue, policy: defaultGoldenRetryPolicy, jitter: wait.Jitter, timers: map[string]*time.Timer{}}
}

func (g *goldenRetries) retry(ctx context.Context, revision, key string, crashed *ateapipb.ActorTemplate) (*ReconciliationFailure, error) {
	ref := crashed.GetMetadata()
	crash := crashed.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage()
	now := time.Now()
	retry, err := g.store.EnsureGoldenRetry(ctx, revision, ref.GetUid(), now, g.policy.jitteredDelays(g.jitter))
	if err != nil {
		return nil, err
	}
	if delay := retry.RetryAt.Sub(now); delay > 0 {
		g.arm(key, delay)
		return goldenRetrying(fmt.Sprintf("golden build crashed, retry %d at %s: %s", retry.Retries, retry.RetryAt.UTC().Format(time.RFC3339), crash)), nil
	}
	if err := g.templates.DeleteActorTemplateUID(ctx, ref.GetAtespace(), ref.GetName(), ref.GetUid()); err != nil {
		return goldenRetrying(fmt.Sprintf("golden build crashed, retry %d waits to delete the crashed template (%s): %s", retry.Retries, status.Code(err), crash)),
			fmt.Errorf("delete crashed ActorTemplate %s/%s: %w", ref.GetAtespace(), ref.GetName(), err)
	}
	g.queue.Add(key)
	return goldenRetrying(fmt.Sprintf("golden build crashed, retry %d started: %s", retry.Retries, crash)), nil
}

func (g *goldenRetries) arm(key string, wait time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	if previous, ok := g.timers[key]; ok {
		previous.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(wait, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.stopped || g.timers[key] != timer {
			return
		}
		delete(g.timers, key)
		g.queue.Add(key)
	})
	g.timers[key] = timer
}

func (g *goldenRetries) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
	for key, timer := range g.timers {
		timer.Stop()
		delete(g.timers, key)
	}
}

func (f *ReconciliationFailure) awaitsGoldenRetry() bool {
	return f != nil && f.Reason == goldenRetryingReason
}

func goldenRebuildMessage(retries int, waiting string) string {
	if retries == 0 {
		return waiting
	}
	return fmt.Sprintf("rebuilding after golden crash, retry %d; %s", retries, waiting)
}

func goldenRetrying(message string) *ReconciliationFailure {
	return &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionReady, Reason: goldenRetryingReason, Message: message, Retryable: true}
}

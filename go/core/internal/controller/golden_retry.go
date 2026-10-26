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

// The store picks the delay by its persisted attempt count, so draw them all.
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

// goldenRetryStore holds the persisted retry schedule. The database, not the
// timers, is the source of truth for attempts and the next retry time.
type goldenRetryStore interface {
	ScheduleGoldenRetry(ctx context.Context, revision, crashedTemplateUID string, now time.Time, delays []time.Duration) (database.GoldenRetry, error)
	GoldenRetryAttempts(ctx context.Context, revision string) (int, error)
}

// goldenRetries wakes one reconcile queue for its crashed golden builds. Each
// reconciler owns one beside its queue: Agent and SandboxTemplate keys share
// the namespace/name form, so one timer map for both would collide.
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

// retry advances the stored retry for crashed, which the caller must already
// have recorded as revision, and returns the Ready failure to publish for key.
// A nil failure means scheduling failed and the caller reports err itself. A
// failure with err means the delete failed; publish it and return err so the
// queue backs off. A repeat pass over the same crash re-reads the stored
// schedule and spends no attempt.
func (g *goldenRetries) retry(ctx context.Context, revision, key string, crashed *ateapipb.ActorTemplate) (*ReconciliationFailure, error) {
	ref := crashed.GetMetadata()
	crash := crashed.GetStatus().GetGoldenSnapshotStatus().GetErrorMessage()
	now := time.Now()
	retry, err := g.store.ScheduleGoldenRetry(ctx, revision, ref.GetUid(), now, g.policy.jitteredDelays(g.jitter))
	if err != nil {
		return nil, err
	}
	if delay := retry.RetryAt.Sub(now); delay > 0 {
		g.arm(key, delay)
		return goldenRetrying(fmt.Sprintf("golden build crashed, retry %d at %s: %s", retry.Attempts, retry.RetryAt.UTC().Format(time.RFC3339), crash)), nil
	}
	if err := g.templates.DeleteActorTemplateUID(ctx, ref.GetAtespace(), ref.GetName(), ref.GetUid()); err != nil {
		return goldenRetrying(fmt.Sprintf("golden build crashed, retry %d waits to delete the crashed template (%s): %s", retry.Attempts, status.Code(err), crash)),
			fmt.Errorf("delete crashed ActorTemplate %s/%s: %w", ref.GetAtespace(), ref.GetName(), err)
	}
	g.queue.Add(key)
	return goldenRetrying(fmt.Sprintf("golden build crashed, retry %d started: %s", retry.Attempts, crash)), nil
}

// arm keeps one timer per key and none after stop.
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

// goldenRebuildMessage names the retry while a crashed golden build rebuilds.
func goldenRebuildMessage(retries int, waiting string) string {
	if retries == 0 {
		return waiting
	}
	return fmt.Sprintf("rebuilding after golden crash, retry %d; %s", retries, waiting)
}

func goldenRetrying(message string) *ReconciliationFailure {
	return &ReconciliationFailure{Condition: kagentv1alpha3.AgentConditionReady, Reason: goldenRetryingReason, Message: message, Retryable: true}
}

package anthropicworker

import (
	"context"
	"crypto/cipher"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/api/client"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	tickInterval = 500 * time.Millisecond
	retryDelay   = 3 * time.Second
	dbTimeout    = 3 * time.Second
	apiTimeout   = 10 * time.Second

	commandRecoveryInterval = 30 * time.Second
)

var errHeartbeatExpired = errors.New("sandbox worker heartbeat lease expired")

type Environment struct {
	ID, Key  string
	Template *apiv1.ResourceReference
}

type Runner struct {
	store        store
	provider     anthropic.Client
	sandboxes    *client.SandboxClient
	environments map[string]Environment
	cipher       cipher.AEAD
	idle         time.Duration
	concurrency  int
}

func New(db *pgxpool.Pool, provider anthropic.Client, sandboxes *client.SandboxClient, environments []Environment,
	key []byte, idle time.Duration, concurrency int,
) (*Runner, error) {
	aead, err := credentialCipher(key)
	if err != nil {
		return nil, err
	}
	if idle <= 0 || concurrency <= 0 || len(environments) == 0 {
		return nil, errors.New("positive idle timeout, worker concurrency and at least one environment are required")
	}
	r := &Runner{store: store{db}, provider: provider, sandboxes: sandboxes, cipher: aead,
		idle: idle, concurrency: concurrency, environments: make(map[string]Environment)}
	for _, env := range environments {
		if env.ID == "" || env.Key == "" || env.Template.GetName() == "" || env.Template.GetNamespace() == "" {
			return nil, errors.New("environment ID, key and sandbox template name and namespace are required")
		}
		if _, exists := r.environments[env.ID]; exists {
			return nil, fmt.Errorf("duplicate Anthropic environment ID %q", env.ID)
		}
		r.environments[env.ID] = env
	}
	return r, nil
}

func (r *Runner) Run(ctx context.Context) {
	var workers sync.WaitGroup
	environments := make([]string, 0, len(r.environments))
	for id, env := range r.environments {
		environments = append(environments, id)
		workers.Go(func() { r.poll(ctx, env) })
	}
	for range r.concurrency {
		workers.Go(func() { r.work(ctx, environments) })
	}
	workers.Wait()
}

func (r *Runner) work(ctx context.Context, environments []string) {
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, dbTimeout)
		w, err := r.store.claim(call, environments)
		cancel()
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				r.warn(ctx, "claim assignment", err)
			}
			wait(ctx, tickInterval)
			continue
		}
		err = r.serve(ctx, w, r.environments[w.environment])
		if errors.Is(err, errSessionEnded) {
			r.complete(context.WithoutCancel(ctx), w)
			continue
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			r.warn(ctx, "relay assignment", err)
		}
		delay := time.Duration(0)
		if ctx.Err() == nil {
			delay = retryDelay
		}
		call, cancel = context.WithTimeout(context.WithoutCancel(ctx), dbTimeout)
		if errors.Is(err, errHeartbeatRejected) {
			err = r.store.park(call, w)
		} else {
			err = r.store.release(call, w, delay)
		}
		if err != nil && !errors.Is(err, errLeaseLost) {
			r.warn(call, "release assignment", err)
		}
		cancel()
	}
}

func (r *Runner) complete(ctx context.Context, w assignment) {
	call, cancel := context.WithTimeout(ctx, dbTimeout)
	id, err := r.store.sandbox(call, w)
	cancel()
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			r.warn(ctx, "find sandbox for completed assignment", err)
		}
		return
	}
	if id != "" {
		call, cancel = context.WithTimeout(ctx, apiTimeout)
		_, err := r.sandboxes.DeleteSandbox(call, &apiv1.DeleteSandboxRequest{SandboxId: id})
		cancel()
		if err != nil && status.Code(err) != codes.NotFound {
			// Sandbox TTL is the cleanup backstop when deletion cannot be confirmed.
			r.warn(ctx, "delete completed assignment sandbox", fmt.Errorf("sandbox API status %s", status.Code(err)))
		}
	}
	call, cancel = context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	if err := r.store.finish(call, w); err != nil && !errors.Is(err, errLeaseLost) {
		r.warn(ctx, "finish assignment", err)
	}
}

func (r *Runner) serve(ctx context.Context, w assignment, env Environment) error {
	secret, err := r.cipher.Open(nil, nil, w.credential, []byte(w.id))
	if err != nil {
		return errors.New("decrypt assignment credential")
	}
	token := string(secret)
	if token == "" {
		token = env.Key
	}
	auth := bearer(token)
	if !w.acknowledged {
		if err := r.acknowledge(ctx, w, env); err != nil {
			return err
		}
	}
	group, live := errgroup.WithContext(ctx)
	relay, stopRelay := context.WithCancel(live)
	defer stopRelay()
	heartbeatReady, eventsReady := make(chan struct{}), make(chan struct{})
	eventsDone := make(chan struct{})
	commandWake := make(chan struct{}, 1)
	var activity atomic.Int64
	markActivity(&activity)
	group.Go(func() error { return r.renew(live, w) })
	group.Go(func() error { return r.heartbeat(live, w, auth, heartbeatReady, stopRelay) })
	group.Go(func() error {
		defer close(eventsDone)
		err := await(relay, heartbeatReady)
		if err == nil {
			err = r.events(relay, w, auth, eventsReady, commandWake, &activity)
		}
		if relay.Err() != nil && live.Err() == nil {
			return nil
		}
		return err
	})
	group.Go(func() error {
		err := await(relay, eventsReady)
		if err == nil {
			err = r.runCommands(live, relay, w, env, auth, commandWake, &activity)
		}
		if relay.Err() == nil || live.Err() != nil {
			return err
		}
		// Stop reading SSE before we flush recorded calls under the PostgreSQL claim.
		<-eventsDone
		return r.failExpiredCommands(live, w, auth)
	})
	return group.Wait()
}

func (r *Runner) renew(ctx context.Context, w assignment) error {
	for wait(ctx, leaseDuration/5) {
		call, cancel := context.WithTimeout(ctx, dbTimeout)
		err := r.store.renew(call, w)
		cancel()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runner) warn(ctx context.Context, operation string, err error) {
	logging.FromContext(ctx).WarnContext(ctx, "anthropic sandbox relay operation failed", "operation", operation, "error", err)
}

func markActivity(activity *atomic.Int64) { activity.Store(time.Now().UnixNano()) }

func await(ctx context.Context, ready <-chan struct{}) error {
	select {
	case <-ready:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *Runner) runCommands(live, ctx context.Context, w assignment, env Environment, auth []option.RequestOption, wake <-chan struct{}, activity *atomic.Int64) error {
	var suspended bool
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, dbTimeout)
		c, err := r.store.nextCommand(call, w)
		cancel()
		if errors.Is(err, pgx.ErrNoRows) {
			delay := commandRecoveryInterval
			if !suspended && w.sandboxID != "" {
				untilIdle := time.Until(time.Unix(0, activity.Load()).Add(r.idle))
				if untilIdle <= 0 {
					suspended = r.suspend(ctx, w)
					markActivity(activity)
					untilIdle = r.idle
				}
				if !suspended {
					delay = min(delay, untilIdle)
				}
			}
			select {
			case <-wake:
			case <-time.After(delay):
			case <-ctx.Done():
			}
			continue
		}
		if err != nil {
			return err
		}
		markActivity(activity)
		switch c.phase {
		case "queued":
			suspended = false
			err = r.startCommand(ctx, &w, c, env.Template)
		case "starting":
			err = r.result(ctx, w, c, "Could not recover the command's execution status.", true)
		case "running":
			err = r.collectCommand(ctx, w, c)
			wait(ctx, tickInterval)
		case "result":
			err = r.sendResult(live, w, c, auth)
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runner) failExpiredCommands(ctx context.Context, w assignment, auth []option.RequestOption) error {
	ctx, cancel := context.WithTimeout(ctx, 2*apiTimeout)
	defer cancel()
	for {
		call, done := context.WithTimeout(ctx, dbTimeout)
		c, err := r.store.nextCommand(call, w)
		done()
		if errors.Is(err, pgx.ErrNoRows) {
			return errHeartbeatExpired
		}
		if err != nil {
			return err
		}
		if c.phase != "result" {
			if c.phase == "running" && c.processID != "" {
				r.killExpiredProcess(ctx, w, c.processID)
			}
			if err := r.result(ctx, w, c, "Command did not finish before the sandbox worker's heartbeat lease expired.", true); err != nil {
				return err
			}
			continue
		}
		if err := r.sendResult(ctx, w, c, auth); errors.Is(err, errResultRetryable) && wait(ctx, retryDelay) {
			continue
		} else if err != nil {
			return err
		}
	}
}
